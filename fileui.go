package main

import (
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/component"
	"gioui.org/x/explorer"

	"loov.dev/lensm/internal/codeview"
	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"

	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/mcp"
	"loov.dev/lensm/internal/syntax"
)

type FileUIConfig struct {
	Path         string
	Watch        bool
	Context      int
	CommentsPath string
}

type FileUI struct {
	Windows *gui.Windows
	Theme   *gui.Theme

	Config   FileUIConfig
	Settings AppSettings

	LoadError error

	// Currently loaded executable.
	// File is shared with the MCP server, which holds its own reference.
	File  *disasm.Shared
	Funcs *gui.FilterList[disasm.Func]

	// Active code view.
	CodeTabs  []*CodeTab
	ActiveTab int
	Tabs      widget.List

	// split divides the function list from the code view.
	split component.Resize

	// Other FileUI elements.
	BrowseButton   widget.Clickable
	SettingsButton widget.Clickable
	Dark           widget.Bool
	SyntaxStyle    widget.Enum
	ShowNativeAsm  widget.Bool
	ShowAsmHelp    widget.Bool
	HoverLinksOnly widget.Bool
	Comment        codeview.CommentEditor
	TextSizeEditor widget.Editor
	// textSizeEdited marks TextSizeEditor as changed since it was applied.
	textSizeEdited bool
	// settingsPath is where Settings are saved, empty for none.
	settingsPath string
	StartMCP     widget.Clickable
	StopMCP      widget.Clickable

	Comments *comments.Store
	MCP      *mcp.AppServer

	picker             *explorer.Explorer
	loader             *loader
	invalidate         chan struct{}
	settingsEvents     chan event.Event
	settingsAcks       chan struct{}
	pickerResults      chan pickerResult
	codeResults        chan codeResult
	exited             chan struct{}
	flushTimer         *time.Timer
	sessionDirty       bool
	copyStatus         string
	copyStatusAt       time.Time
	saveError          string
	settingsWindowOpen bool
	pickerOpen         bool
	loadGeneration     uint64
	loadedPath         string
	Navigation         NavigationHistory
	navigatingHistory  bool
}

type pickerResult struct {
	path string
	ok   bool
	err  error
}

func NewFileUI(windows *gui.Windows, theme *material.Theme) *FileUI {
	ui := &FileUI{}
	ui.Windows = windows
	settings := DefaultAppSettings()
	path, err := AppSettingsPath()
	if err == nil {
		ui.settingsPath = path
		settings, err = LoadAppSettings(path)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to load settings: %v\n", err)
	}
	ui.Settings = settings
	ui.Theme = gui.NewTheme(theme, settings.Dark)
	ui.SyntaxStyle.Value = settings.SyntaxStyle
	ui.Dark.Value = settings.Dark
	ui.Funcs = gui.NewFilterList[disasm.Func]()
	ui.ActiveTab = -1
	ui.Navigation.Reset()
	ui.Tabs.List.Axis = layout.Horizontal
	ui.ShowNativeAsm.Value = settings.ShowNativeAsm
	ui.ShowAsmHelp.Value = settings.ShowAsmHelp
	ui.HoverLinksOnly.Value = settings.HoverLinksOnly
	ui.split.Axis = layout.Horizontal
	ui.split.Ratio = settings.SidebarRatio
	ui.Comment.SingleLine = true
	ui.TextSizeEditor.SingleLine = true
	ui.TextSizeEditor.Submit = true
	ui.TextSizeEditor.SetText(strconv.Itoa(settings.TextSize))
	ui.Comments = comments.NewMemory("")
	return ui
}

func (ui *FileUI) Run(w *app.Window) error {
	var ops op.Ops

	exited := make(chan struct{})
	defer close(exited)
	defer func() {
		if ui.MCP != nil {
			_ = ui.MCP.Close()
		}
	}()

	loader := newLoader(loadFile, ui.Config.Watch)
	ui.loader = loader
	defer loader.Close()
	picker := explorer.NewExplorer(w)
	ui.picker = picker
	invalidate := make(chan struct{}, 1)
	ui.invalidate = invalidate
	settingsEvents := make(chan event.Event)
	settingsAcks := make(chan struct{})
	ui.settingsEvents = settingsEvents
	ui.settingsAcks = settingsAcks
	pickerResults := make(chan pickerResult, 1)
	ui.pickerResults = pickerResults
	codeResults := make(chan codeResult)
	ui.codeResults = codeResults
	ui.exited = exited
	flushTimer := time.NewTimer(time.Hour)
	flushTimer.Stop()
	ui.flushTimer = flushTimer
	var settingsOps op.Ops
	defer ui.flushPending()
	defer func() {
		ui.picker = nil
		ui.loader = nil
		ui.invalidate = nil
		ui.settingsEvents = nil
		ui.settingsAcks = nil
		ui.pickerResults = nil
		ui.codeResults = nil
		ui.exited = nil
		ui.flushTimer = nil
	}()

	if ui.Config.Path != "" {
		ui.requestLoad(ui.Config.Path)
	}

	events := make(chan event.Event)
	acks := make(chan struct{})

	go func() {
		for {
			ev := w.Event()
			events <- ev
			<-acks
			if _, ok := ev.(app.DestroyEvent); ok {
				return
			}
		}
	}()

	for {
		select {
		case result := <-loader.Results():
			if result.generation != ui.loadGeneration {
				if result.file != nil {
					_ = result.file.Close()
				}
				continue
			}
			if result.err != nil {
				// The previous file, if any, stays open for the GUI and
				// MCP alike until a reload succeeds.
				ui.LoadError = result.err
				w.Invalidate()
				continue
			}
			ui.SetFile(result.file)
			w.Invalidate()
		case <-invalidate:
			w.Invalidate()
		case res := <-codeResults:
			ui.setTabCode(res)
			w.Invalidate()
		case <-flushTimer.C:
			ui.flushPending()
			w.Invalidate()
		case res := <-pickerResults:
			ui.pickerOpen = false
			if res.err != nil {
				ui.LoadError = res.err
			} else if res.ok {
				ui.Config.Path = res.path
				ui.LoadError = nil
				ui.copyStatus = ""
				ui.requestLoad(res.path)
			}
			w.Invalidate()
		case ev := <-settingsEvents:
			switch e := ev.(type) {
			case app.FrameEvent:
				gtx := app.NewContext(&settingsOps, e)
				ui.layoutSettingsWindow(gtx)
				e.Frame(gtx.Ops)
			case app.DestroyEvent:
				ui.settingsWindowOpen = false
			}
			settingsAcks <- struct{}{}
		case e := <-events:
			picker.ListenEvents(e)
			switch e := e.(type) {
			case app.FrameEvent:
				gtx := app.NewContext(&ops, e)
				ui.Layout(gtx)
				e.Frame(gtx.Ops)

			case app.DestroyEvent:
				acks <- struct{}{}
				return e.Err
			}
			acks <- struct{}{}
		}
	}
}

func (ui *FileUI) SetFile(file disasm.File) {
	ui.navigatingHistory = true
	defer func() {
		ui.navigatingHistory = false
		if tab := ui.activeTab(); tab != nil {
			ui.Navigation.Visit(tab.Name)
		}
	}()
	// Keep history across watch-mode reloads of the same binary; open
	// tabs are preserved there too. Entries whose functions vanished are
	// skipped during navigation.
	path := comments.CleanPath(ui.Config.Path)
	reload := ui.File != nil && path == ui.loadedPath
	if !reload {
		ui.Navigation.Reset()
	}
	ui.loadedPath = path
	// A reload keeps each tab where it was, matched by function name.
	previous := map[string]*CodeTab{}
	if reload {
		for _, tab := range ui.CodeTabs {
			previous[tab.Name] = tab
		}
	}

	initialLoad := ui.File == nil
	if ui.File != nil {
		_ = ui.File.Close()
	}

	openNames := make([]string, 0, len(ui.CodeTabs))
	for _, tab := range ui.CodeTabs {
		openNames = append(openNames, tab.Name)
	}
	activeName := ui.Funcs.Selected
	if tab := ui.activeTab(); tab != nil {
		activeName = tab.Name
	}
	if initialLoad && comments.CleanPath(ui.Config.Path) == ui.Settings.LastPath && len(ui.Settings.OpenTabs) > 0 {
		openNames = append(openNames[:0], ui.Settings.OpenTabs...)
		activeName = ui.Settings.ActiveTab
		if activeName == "" && len(openNames) > 0 {
			activeName = openNames[0]
		}
	}

	ui.File = disasm.Share(file)
	ui.LoadError = nil
	ui.loadCommentsForPath(ui.Config.Path)
	ui.CodeTabs = nil
	ui.ActiveTab = -1
	ui.Comment.Reset()
	ui.Funcs.SetItems(file.Funcs())

	if activeName != "" {
		ui.Funcs.Select(activeName)
	}
	for _, name := range openNames {
		fn := ui.findFunc(name)
		if fn == nil {
			continue
		}
		tab := ui.appendCodeTab(fn)
		if old, ok := previous[name]; ok {
			tab.Code.KeepView(&old.Code)
			tab.Preview = old.Preview
		}
		if name == activeName {
			ui.ActiveTab = len(ui.CodeTabs) - 1
			ui.Funcs.Select(tab.Name)
		}
	}

	if len(ui.CodeTabs) == 0 {
		if ui.Funcs.SelectedItem == nil && len(ui.Funcs.Filtered) > 0 {
			ui.Funcs.SelectIndex(0)
		}
		ui.openTab(ui.Funcs.SelectedItem, false)
		ui.afterFileLoaded()
		return
	}
	if ui.ActiveTab < 0 || ui.ActiveTab >= len(ui.CodeTabs) {
		ui.ActiveTab = 0
		ui.Funcs.Select(ui.CodeTabs[ui.ActiveTab].Name)
	}
	ui.afterFileLoaded()
}

func (ui *FileUI) loadOptions() disasm.Options {
	return disasm.Options{Context: ui.Config.Context}
}

func (ui *FileUI) findFunc(name string) disasm.Func {
	if ui.File == nil {
		return nil
	}
	return disasm.FindFunc(ui.File, name)
}

func (ui *FileUI) Layout(gtx layout.Context) {
	colors := ui.Theme.Colors
	paint.FillShape(gtx.Ops, colors.Background, clip.Rect{Max: gtx.Constraints.Max}.Op())

	event.Op(gtx.Ops, ui)
	ui.handleActions(gtx)

	layout.Flex{
		Axis: layout.Vertical,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.layoutToolbar(gtx, colors)
		}),
		layout.Rigid(gui.HorizontalLine{Height: 1, Color: colors.Splitter}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return ui.layoutContent(gtx, colors)
		}),
	)
}

func (ui *FileUI) handleActions(gtx layout.Context) {
	// widget.Editor claims Option+arrow word-jumps on macOS, and this
	// unfocused poll runs before any editor's Update, so it would steal
	// them from a focused editor. Only listen for Alt+arrows while no
	// text editor has focus.
	editorFocused := gtx.Focused(&ui.Comment) || gtx.Focused(&ui.Funcs.Filter)
	filters := []event.Filter{
		key.Filter{Required: key.ModShortcut, Name: key.Name("[")},
		key.Filter{Required: key.ModShortcut, Name: key.Name("]")},
		key.Filter{Required: key.ModShortcut, Name: key.Name("W")},
		key.Filter{Required: key.ModShortcut, Name: key.Name("F")},
		key.Filter{Required: key.ModShortcut, Name: key.Name("O")},
		key.Filter{Required: key.ModShortcut, Name: key.Name(",")},
		key.Filter{Required: key.ModCtrl, Optional: key.ModShift, Name: key.NameTab},
	}
	if !editorFocused {
		filters = append(filters,
			key.Filter{Required: key.ModAlt, Name: key.NameLeftArrow},
			key.Filter{Required: key.ModAlt, Name: key.NameRightArrow},
		)
	}
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		keyEvent, ok := ev.(key.Event)
		if !ok || keyEvent.State != key.Press {
			continue
		}
		switch keyEvent.Name {
		case key.NameLeftArrow, key.Name("["):
			ui.navigateBack()
		case key.NameRightArrow, key.Name("]"):
			ui.navigateForward()
		case key.Name("W"):
			ui.closeTab(ui.ActiveTab)
		case key.Name("F"):
			gtx.Execute(key.FocusCmd{Tag: &ui.Funcs.Filter})
			ui.Funcs.Filter.SetCaret(ui.Funcs.Filter.Len(), 0)
		case key.Name("O"):
			ui.chooseFile()
		case key.Name(","):
			ui.openSettingsWindow()
		case key.NameTab:
			if n := len(ui.CodeTabs); n > 0 {
				step := 1
				if keyEvent.Modifiers.Contain(key.ModShift) {
					step = n - 1
				}
				ui.activateTab((ui.ActiveTab + step) % n)
			}
		}
	}
	// Enter in the function list keeps the previewed function open.
	if ui.Funcs.List.Activated() {
		if tab := ui.activeTab(); tab != nil && tab.Preview {
			ui.selectTab(ui.ActiveTab)
		}
	}
	for ui.BrowseButton.Clicked(gtx) {
		ui.chooseFile()
	}
	for ui.SettingsButton.Clicked(gtx) {
		ui.openSettingsWindow()
	}
}

func (ui *FileUI) layoutToolbar(gtx layout.Context, colors gui.UIColors) layout.Dimensions {
	paint.FillShape(gtx.Ops, colors.SecondaryBackground, clip.Rect{Max: gtx.Constraints.Max}.Op())

	inset := layout.Inset{Top: 4, Right: 6, Bottom: 4, Left: 6}
	return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				button := material.Button(ui.Theme.Theme, &ui.BrowseButton, "Choose...")
				button.Inset = layout.Inset{Top: 6, Right: 10, Bottom: 6, Left: 10}
				return layout.Inset{Right: 6}.Layout(gtx, button.Layout)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				if ui.Config.Path == "" {
					return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
				}
				label := ui.Theme.Muted(ui.Config.Path, 0.9)
				label.MaxLines = 1
				return layout.W.Layout(gtx, label.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				button := material.IconButton(ui.Theme.Theme, &ui.SettingsButton, SettingsIcon, "Settings")
				button.Size = 18
				button.Inset = layout.UniformInset(8)
				return layout.Inset{Left: 4}.Layout(gtx, button.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				// "Copied selection" is feedback for the moment, not state.
				const copyStatusFor = 2 * time.Second
				if ui.copyStatus != "" && !ui.copyStatusAt.IsZero() {
					if gtx.Now.Sub(ui.copyStatusAt) >= copyStatusFor {
						ui.copyStatus = ""
					} else {
						gtx.Execute(op.InvalidateCmd{At: ui.copyStatusAt.Add(copyStatusFor)})
					}
				}
				if ui.copyStatus == "" {
					return layout.Dimensions{}
				}
				label := ui.Theme.Muted(ui.copyStatus, 0.85)
				return layout.Inset{Left: 2}.Layout(gtx, label.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if ui.saveError == "" {
					return layout.Dimensions{}
				}
				label := ui.Theme.ErrorLabel(ui.saveError, 0.85)
				return layout.Inset{Left: 2}.Layout(gtx, label.Layout)
			}),
		)
	})
}

// scheduleFlush arranges for buffered comment and settings changes to
// reach disk once the user pauses, instead of on every keystroke or tab
// switch. Main event loop only.
func (ui *FileUI) scheduleFlush() {
	if ui.flushTimer == nil {
		ui.flushPending()
		return
	}
	ui.flushTimer.Reset(time.Second)
}

func (ui *FileUI) flushPending() {
	ui.saveError = ""
	// A FileUI without a settings path, as in tests, doesn't save.
	if ui.sessionDirty && ui.settingsPath != "" {
		if err := SaveAppSettings(ui.settingsPath, ui.Settings); err != nil {
			ui.saveError = "settings not saved: " + err.Error()
			fmt.Fprintf(os.Stderr, "unable to save settings: %v\n", err)
		} else {
			ui.sessionDirty = false
		}
	}
	if err := ui.Comments.Flush(); err != nil {
		ui.saveError = "comments not saved: " + err.Error()
		fmt.Fprintf(os.Stderr, "unable to save comments: %v\n", err)
	}
}

func (ui *FileUI) invalidateMain() {
	if ui.invalidate == nil {
		return
	}
	select {
	case ui.invalidate <- struct{}{}:
	default:
	}
}

func (ui *FileUI) chooseFile() {
	if ui.pickerOpen || ui.picker == nil || ui.pickerResults == nil {
		return
	}
	// The native picker blocks until dismissed; running it here would
	// stall the frame in progress and freeze the window.
	ui.pickerOpen = true
	picker, results, exited := ui.picker, ui.pickerResults, ui.exited
	go func() {
		var res pickerResult
		// No extension filter: Go executables usually have none.
		file, err := picker.ChooseFile()
		switch {
		case err == nil:
			// The disassembler needs a path, not a reader; on desktop
			// platforms the explorer hands back an *os.File.
			if f, ok := file.(*os.File); ok {
				res = pickerResult{path: f.Name(), ok: true}
			} else {
				res.err = errors.New("file picker did not return a local file path")
			}
			_ = file.Close()
		case errors.Is(err, explorer.ErrUserDecline):
		default:
			res.err = err
		}
		select {
		case results <- res:
		case <-exited:
		}
	}()
}

func (ui *FileUI) layoutContent(gtx layout.Context, colors gui.UIColors) layout.Dimensions {
	active := ui.activeTab()
	if active == nil || (!active.Code.Loaded() && !active.loading) || active.Name != ui.Funcs.Selected {
		selected := ui.Funcs.SelectedItem
		if selected != nil {
			ui.previewTab(selected)
		}
	}

	// Keep both panes usable at any window size; the ratio from a drag
	// or from settings is clamped, not trusted.
	total := float32(gtx.Constraints.Max.X)
	minSidebar := float32(gtx.Metric.Dp(120))
	minContent := float32(gtx.Metric.Dp(240))
	ui.split.Ratio = min(max(ui.split.Ratio, minSidebar/total), 1-minContent/total)

	defer func() {
		if ui.split.Ratio != ui.Settings.SidebarRatio {
			settings := ui.Settings
			settings.SidebarRatio = ui.split.Ratio
			ui.saveSettings(settings)
		}
	}()

	return ui.split.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(gtx.Constraints.Max)
			return ui.Funcs.Layout(ui.Theme, gtx)
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if ui.LoadError != nil {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								txt := ui.Theme.ErrorLabel(ui.LoadError.Error(), 1)
								return layout.UniformInset(6).Layout(gtx, txt.Layout)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if ui.File == nil {
									return layout.Dimensions{}
								}
								return ui.layoutCodeTabs(gtx, colors)
							}),
						)
					}
					return ui.layoutCodeTabs(gtx, colors)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					code := ui.activeCode()
					if code == nil || !code.Loaded() {
						return layout.Dimensions{}
					}
					txt := ui.Theme.Muted("file: "+code.Code.File, 1)
					txt.Font.Style = font.Italic

					inset := layout.Inset{Top: 2, Left: 4, Right: 4, Bottom: 4}
					return inset.Layout(gtx, txt.Layout)
				}),
				layout.Rigid(gui.HorizontalLine{Height: 1, Color: colors.Splitter}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					if ui.LoadError != nil && ui.File == nil {
						return layout.Dimensions{}
					}
					if ui.File == nil && ui.Config.Path == "" {
						txt := ui.Theme.Muted("Choose a binary to disassemble: a Go, C, C++, Rust or TinyGo executable, an object file or a wasm module. "+
							"It can also be given on the command line, lensm <path>.", 1)
						return layout.UniformInset(12).Layout(gtx, txt.Layout)
					}
					if tab := ui.activeTab(); tab != nil && tab.LoadError != nil {
						txt := ui.Theme.ErrorLabel(tab.LoadError.Error(), 1)
						return layout.UniformInset(6).Layout(gtx, txt.Layout)
					}
					if tab := ui.activeTab(); tab != nil && tab.loading {
						txt := ui.Theme.Muted("Disassembling "+tab.Name+"...", 1)
						return layout.UniformInset(6).Layout(gtx, txt.Layout)
					}
					code := ui.activeCode()
					if code == nil {
						return layout.Dimensions{}
					}

					gtx.Constraints = layout.Exact(gtx.Constraints.Max)
					return layout.Stack{
						Alignment: layout.SE,
					}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							return codeview.Style{
								UI: code,

								TryOpen:    ui.tryOpen,
								OnInteract: ui.keepActiveTab,
								CopyText: func(gtx layout.Context, text string) {
									ui.writeClipboardText(gtx, text, "Copied selection")
								},

								Comments:      ui.Comments,
								SetComment:    ui.setBufferedComment,
								CommentEditor: &ui.Comment,

								Theme:          ui.Theme,
								Syntax:         syntax.PaletteFor(ui.Settings.SyntaxStyle, codeview.SyntaxColors(colors)),
								ShowNative:     ui.ShowNativeAsm.Value,
								ShowHelp:       ui.ShowAsmHelp.Value,
								HoverLinksOnly: ui.HoverLinksOnly.Value,
								TextHeight:     ui.Theme.TextSize,
							}.Layout(gtx)
						}),
					)
				}),
			)
		},
		func(gtx layout.Context) layout.Dimensions {
			size := image.Pt(gtx.Metric.Dp(8), gtx.Constraints.Max.Y)
			paint.FillShape(gtx.Ops, colors.Splitter, clip.Rect{
				Min: image.Pt(size.X/2, 0),
				Max: image.Pt(size.X/2+1, size.Y),
			}.Op())
			return layout.Dimensions{Size: size}
		},
	)
}

func (ui *FileUI) requestLoad(path string) {
	if ui.loader == nil {
		return
	}
	ui.loadGeneration++
	ui.loader.Request(ui.loadGeneration, path)
}

func (ui *FileUI) afterFileLoaded() {
	ui.saveSessionState()
	if ui.MCP != nil {
		ui.MCP.SetFile(ui.Config.Path, ui.File.Acquire(), ui.Comments)
	}
}

func (ui *FileUI) saveSessionState() {
	if ui.File == nil {
		return
	}
	path := comments.CleanPath(ui.Config.Path)
	if path == "" {
		return
	}

	openTabs := make([]string, 0, len(ui.CodeTabs))
	for _, tab := range ui.CodeTabs {
		if tab.Name != "" && !tab.Preview {
			openTabs = append(openTabs, tab.Name)
		}
	}
	activeTab := ""
	if tab := ui.activeTab(); tab != nil {
		activeTab = tab.Name
	}

	settings := ui.Settings
	settings.LastPath = path
	settings.OpenTabs = cleanFuncNames(openTabs)
	settings.ActiveTab = activeTab
	if settings.ActiveTab != "" && !slices.Contains(settings.OpenTabs, settings.ActiveTab) {
		settings.ActiveTab = ""
	}
	if ui.Settings.LastPath == settings.LastPath &&
		slices.Equal(ui.Settings.OpenTabs, settings.OpenTabs) &&
		ui.Settings.ActiveTab == settings.ActiveTab {
		return
	}

	ui.Settings = settings
	ui.sessionDirty = true
	ui.scheduleFlush()
}

func (ui *FileUI) writeClipboardText(gtx layout.Context, text, status string) {
	if text == "" {
		return
	}
	gtx.Execute(clipboard.WriteCmd{
		Type: "text/plain",
		Data: io.NopCloser(strings.NewReader(text)),
	})
	ui.copyStatus = status
	ui.copyStatusAt = gtx.Now
}

func (ui *FileUI) tryOpen(gtx layout.Context, call string) {
	fn := ui.findFunc(call)
	if fn == nil {
		return
	}

	ui.openTab(fn, true)
	gtx.Execute(op.InvalidateCmd{})
}

func (ui *FileUI) startMCP() {
	if ui.MCP != nil {
		return
	}
	server, err := mcp.StartAppServer()
	if err != nil {
		ui.LoadError = fmt.Errorf("unable to start MCP server: %w", err)
		ui.invalidateMain()
		return
	}
	ui.MCP = server
	if ui.File != nil {
		ui.MCP.SetFile(ui.Config.Path, ui.File.Acquire(), ui.Comments)
	}
	fmt.Fprintf(os.Stderr, "lensm MCP server listening at %s\n", server.URL())
	ui.invalidateMain()
}

func (ui *FileUI) stopMCP() {
	if ui.MCP == nil {
		return
	}
	if err := ui.MCP.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "unable to stop MCP server: %v\n", err)
	}
	ui.MCP = nil
	ui.invalidateMain()
}
