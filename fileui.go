package main

import (
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
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/goobj"
	"loov.dev/lensm/internal/mcp"
	"loov.dev/lensm/internal/syntax"
	"loov.dev/lensm/internal/wasmobj"
)

var workInProgressWASM bool

type FileUIConfig struct {
	Path         string
	Watch        bool
	Context      int
	CommentsPath string
}

type FileUI struct {
	Windows *Windows
	Theme   *material.Theme

	Config   FileUIConfig
	Settings AppSettings

	LoadError error

	// Currently loaded executable.
	File  disasm.File
	Funcs *FilterList[disasm.Func]

	// Active code view.
	CodeTabs  []*CodeTab
	ActiveTab int
	Tabs      widget.List

	// Other FileUI elements.
	BrowseButton   widget.Clickable
	SettingsButton widget.Clickable
	Dark           widget.Bool
	SyntaxStyle    widget.Enum
	ShowNativeAsm  widget.Bool
	ShowAsmHelp    widget.Bool
	Comment        widget.Editor
	TextSizeEditor widget.Editor
	StartMCP       widget.Clickable
	StopMCP        widget.Clickable

	Comments *comments.Store
	MCP      *mcp.AppServer

	loadRequests       chan fileLoadRequest
	invalidate         chan struct{}
	settingsEvents     chan event.Event
	settingsAcks       chan struct{}
	pickerResults      chan pickerResult
	exited             chan struct{}
	flushTimer         *time.Timer
	sessionDirty       bool
	commentKey         string
	copyStatus         string
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

type fileLoadRequest struct {
	generation uint64
	path       string
}

type fileLoadResult struct {
	generation uint64
	file       disasm.File
	err        error
}

func NewFileUI(windows *Windows, theme *material.Theme) *FileUI {
	ui := &FileUI{}
	ui.Windows = windows
	ui.Theme = theme
	settings, err := LoadAppSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to load settings: %v\n", err)
	}
	ui.Settings = settings
	ui.SyntaxStyle.Value = settings.SyntaxStyle
	ui.Dark.Value = settings.Dark
	ui.Funcs = NewFilterList[disasm.Func](theme)
	ui.ActiveTab = -1
	ui.Navigation.Reset()
	ui.Tabs.List.Axis = layout.Horizontal
	ui.ShowNativeAsm.Value = settings.ShowNativeAsm
	ui.ShowAsmHelp.Value = settings.ShowAsmHelp
	ui.Comment.SingleLine = true
	ui.TextSizeEditor.SingleLine = true
	ui.TextSizeEditor.Submit = true
	ui.TextSizeEditor.SetText(strconv.Itoa(settings.TextSize))
	ui.Comments, _ = comments.Open("", "")
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

	loadResults := make(chan fileLoadResult, 1)
	loadRequests := make(chan fileLoadRequest, 1)
	ui.loadRequests = loadRequests
	invalidate := make(chan struct{}, 1)
	ui.invalidate = invalidate
	settingsEvents := make(chan event.Event)
	settingsAcks := make(chan struct{})
	ui.settingsEvents = settingsEvents
	ui.settingsAcks = settingsAcks
	pickerResults := make(chan pickerResult, 1)
	ui.pickerResults = pickerResults
	ui.exited = exited
	flushTimer := time.NewTimer(time.Hour)
	flushTimer.Stop()
	ui.flushTimer = flushTimer
	var settingsOps op.Ops
	defer ui.flushPending()
	defer func() {
		ui.loadRequests = nil
		ui.invalidate = nil
		ui.settingsEvents = nil
		ui.settingsAcks = nil
		ui.pickerResults = nil
		ui.exited = nil
		ui.flushTimer = nil
	}()

	loadFinished := func(result fileLoadResult) {
		select {
		case old := <-loadResults:
			if old.file != nil {
				_ = old.file.Close()
			}
		default:
		}
		loadResults <- result
	}

	go func() {
		var lastModTime time.Time
		var pendingModTime time.Time
		var pendingSince time.Time
		var path string
		var generation uint64
		tick := time.NewTicker(150 * time.Millisecond)
		defer tick.Stop()

		load := func(force bool, now time.Time) {
			if path == "" {
				return
			}

			stat, err := os.Stat(path)
			if err != nil {
				loadFinished(fileLoadResult{generation: generation, err: err})
				return
			}
			if !force && stat.ModTime().Equal(lastModTime) {
				return
			}
			if !force {
				if !stat.ModTime().Equal(pendingModTime) {
					pendingModTime = stat.ModTime()
					pendingSince = now
					return
				}
				if now.Sub(pendingSince) < 300*time.Millisecond {
					return
				}
			}
			lastModTime = stat.ModTime()
			pendingModTime = time.Time{}

			file, err := loadDisasmFile(path)
			loadFinished(fileLoadResult{generation: generation, file: file, err: err})
		}

		for {
			select {
			case req := <-loadRequests:
				path = strings.TrimSpace(req.path)
				generation = req.generation
				lastModTime = time.Time{}
				pendingModTime = time.Time{}
				load(true, time.Now())
			case now := <-tick.C:
				if ui.Config.Watch {
					load(false, now)
				}
			case <-exited:
				return
			}
		}
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
		case result := <-loadResults:
			if result.generation != ui.loadGeneration {
				if result.file != nil {
					_ = result.file.Close()
				}
				continue
			}
			if result.err != nil {
				ui.LoadError = result.err
				if ui.MCP != nil {
					ui.MCP.SetPath("", ui.Comments)
				}
				w.Invalidate()
				continue
			}
			ui.SetFile(result.file)
			w.Invalidate()
		case <-invalidate:
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
	if ui.File == nil || path != ui.loadedPath {
		ui.Navigation.Reset()
	}
	ui.loadedPath = path

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

	ui.File = file
	ui.LoadError = nil
	ui.loadCommentsForPath(ui.Config.Path)
	ui.CodeTabs = nil
	ui.ActiveTab = -1
	ui.commentKey = ""
	ui.Funcs.SetItems(file.Funcs())

	if activeName != "" {
		ui.selectFuncByName(activeName)
	}
	for _, name := range openNames {
		fn := ui.findFunc(name)
		if fn == nil {
			continue
		}
		tab := ui.appendCodeTab(fn)
		if name == activeName {
			ui.ActiveTab = len(ui.CodeTabs) - 1
			ui.selectFuncByName(tab.Name)
		}
	}

	if len(ui.CodeTabs) == 0 {
		if ui.Funcs.SelectedItem == nil && len(ui.Funcs.Filtered) > 0 {
			ui.Funcs.SelectIndex(0)
		}
		ui.openFuncTab(ui.Funcs.SelectedItem)
		ui.afterFileLoaded()
		return
	}
	if !InRange(ui.ActiveTab, len(ui.CodeTabs)) {
		ui.ActiveTab = 0
		ui.selectFuncByName(ui.CodeTabs[ui.ActiveTab].Name)
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
	for _, fn := range ui.File.Funcs() {
		if fn.Name() == name {
			return fn
		}
	}
	return nil
}

func (ui *FileUI) selectFuncByName(name string) {
	ui.Funcs.Selected = name
	ui.Funcs.List.Selected = -1
	var zero disasm.Func
	ui.Funcs.SelectedItem = zero

	for i, fn := range ui.Funcs.Filtered {
		if fn.Name() == name {
			ui.Funcs.List.Selected = i
			ui.Funcs.SelectedItem = fn
			return
		}
	}
	for _, fn := range ui.Funcs.All {
		if fn.Name() == name {
			ui.Funcs.SelectedItem = fn
			return
		}
	}
}

func (ui *FileUI) Layout(gtx layout.Context) {
	colors := ApplyTheme(ui.Theme, ui.Dark.Value)
	paint.FillShape(gtx.Ops, colors.Background, clip.Rect{Max: gtx.Constraints.Max}.Op())

	event.Op(gtx.Ops, ui)
	ui.handleActions(gtx)

	layout.Flex{
		Axis: layout.Vertical,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.layoutToolbar(gtx, colors)
		}),
		layout.Rigid(HorizontalLine{Height: 1, Color: colors.Splitter}.Layout),
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
		}
	}
	for ui.BrowseButton.Clicked(gtx) {
		ui.chooseFile()
	}
	for ui.SettingsButton.Clicked(gtx) {
		ui.openSettingsWindow()
	}
}

func (ui *FileUI) layoutToolbar(gtx layout.Context, colors UIColors) layout.Dimensions {
	paint.FillShape(gtx.Ops, colors.SecondaryBackground, clip.Rect{Max: gtx.Constraints.Max}.Op())

	inset := layout.Inset{Top: 4, Right: 6, Bottom: 4, Left: 6}
	return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				button := material.Button(ui.Theme, &ui.BrowseButton, "Choose...")
				button.Inset = layout.Inset{Top: 6, Right: 10, Bottom: 6, Left: 10}
				return layout.Inset{Right: 6}.Layout(gtx, button.Layout)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				if ui.Config.Path == "" {
					return layout.Dimensions{Size: image.Pt(gtx.Constraints.Max.X, 0)}
				}
				label := material.Body1(ui.Theme, ui.Config.Path)
				label.MaxLines = 1
				label.TextSize *= 0.8
				label.Color = colors.MutedText
				return layout.W.Layout(gtx, label.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				button := material.IconButton(ui.Theme, &ui.SettingsButton, SettingsIcon, "Settings")
				button.Size = 18
				button.Inset = layout.UniformInset(8)
				return layout.Inset{Left: 4}.Layout(gtx, button.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if ui.copyStatus == "" {
					return layout.Dimensions{}
				}
				label := material.Body1(ui.Theme, ui.copyStatus)
				label.TextSize *= 0.75
				label.Color = colors.MutedText
				return layout.Inset{Left: 2}.Layout(gtx, label.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if ui.saveError == "" {
					return layout.Dimensions{}
				}
				label := material.Body1(ui.Theme, ui.saveError)
				label.TextSize *= 0.75
				label.Color = colors.Error
				return layout.Inset{Left: 2}.Layout(gtx, label.Layout)
			}),
		)
	})
}

func (ui *FileUI) layoutSyntaxSelector(gtx layout.Context, colors UIColors) layout.Dimensions {
	return layout.Inset{Left: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme, "Syntax")
				label.TextSize *= 0.85
				label.Color = colors.MutedText
				return layout.Inset{Right: 3}.Layout(gtx, label.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutSyntaxRadio(gtx, colors, syntax.StyleGoLand)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutSyntaxRadio(gtx, colors, syntax.StyleDarcula)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutSyntaxRadio(gtx, colors, syntax.StyleMono)
			}),
		)
	})
}

func (ui *FileUI) layoutSyntaxRadio(gtx layout.Context, colors UIColors, style string) layout.Dimensions {
	radio := material.RadioButton(ui.Theme, &ui.SyntaxStyle, style, syntax.StyleLabel(style))
	radio.Color = colors.MutedText
	radio.IconColor = ui.Theme.ContrastBg
	radio.TextSize = ui.Theme.TextSize * 0.78
	radio.Size = unit.Dp(18)
	return layout.Inset{Left: 2}.Layout(gtx, radio.Layout)
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
	if ui.sessionDirty {
		if err := SaveAppSettings(ui.Settings); err != nil {
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
	if ui.pickerOpen || ui.pickerResults == nil {
		return
	}
	// The native picker blocks until dismissed; running it here would
	// stall the frame in progress and freeze the window.
	ui.pickerOpen = true
	results, exited := ui.pickerResults, ui.exited
	go func() {
		path, ok, err := chooseExecutableFile()
		select {
		case results <- pickerResult{path: path, ok: ok, err: err}:
		case <-exited:
		}
	}()
}

func (ui *FileUI) layoutContent(gtx layout.Context, colors UIColors) layout.Dimensions {
	active := ui.activeCode()
	if active == nil || !active.Loaded() || active.Name != ui.Funcs.Selected {
		selected := ui.Funcs.SelectedItem
		if selected != nil {
			ui.openFuncTab(selected)
		}
	}

	return layout.Flex{
		Axis: layout.Horizontal,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Point{
				X: gtx.Metric.Sp(10 * 20),
				Y: gtx.Constraints.Max.Y,
			})
			return ui.Funcs.Layout(ui.Theme, colors, gtx)
		}),
		layout.Rigid(VerticalLine{Width: 1, Color: colors.Splitter}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if ui.LoadError != nil {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								txt := material.Body1(ui.Theme, ui.LoadError.Error())
								txt.Color = colors.Error
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
					txt := material.Body1(ui.Theme, "file: "+code.Code.File)
					txt.Font.Style = font.Italic
					txt.Color = colors.MutedText

					inset := layout.Inset{Top: 2, Left: 4, Right: 4, Bottom: 4}
					return inset.Layout(gtx, txt.Layout)
				}),
				layout.Rigid(HorizontalLine{Height: 1, Color: colors.Splitter}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					if ui.LoadError != nil && ui.File == nil {
						return layout.Dimensions{}
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
							return CodeUIStyle{
								CodeUI: code,

								TryOpen: ui.tryOpen,
								CopyText: func(gtx layout.Context, text string) {
									ui.writeClipboardText(gtx, text, "Copied selection")
								},

								Comments:      ui.Comments,
								SetComment:    ui.setBufferedComment,
								CommentKey:    &ui.commentKey,
								CommentEditor: &ui.Comment,

								Theme:      ui.Theme,
								Colors:     colors,
								Syntax:     syntax.PaletteFor(ui.Settings.SyntaxStyle, colors.syntaxColors()),
								ShowNative: ui.ShowNativeAsm.Value,
								ShowHelp:   ui.ShowAsmHelp.Value,
								TextHeight: ui.Theme.TextSize,
							}.Layout(gtx)
						}),
					)
				}),
			)
		}),
	)
}

func (ui *FileUI) requestLoad(path string) {
	if ui.loadRequests == nil {
		return
	}
	ui.loadGeneration++
	req := fileLoadRequest{
		generation: ui.loadGeneration,
		path:       path,
	}
	select {
	case <-ui.loadRequests:
	default:
	}
	ui.loadRequests <- req
}

func (ui *FileUI) afterFileLoaded() {
	ui.saveSessionState()
	if ui.MCP != nil {
		ui.MCP.SetPath(ui.Config.Path, ui.Comments)
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
		if tab.Name != "" {
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
}

func (ui *FileUI) tryOpen(gtx layout.Context, call string) {
	fn := ui.findFunc(call)
	if fn == nil {
		return
	}

	ui.openFuncTabNext(fn)
	gtx.Execute(op.InvalidateCmd{})
}

func loadDisasmFile(path string) (disasm.File, error) {
	if workInProgressWASM {
		return wasmobj.Load(path)
	}
	return goobj.Load(path)
}

func (ui *FileUI) startMCP() {
	if ui.MCP != nil {
		return
	}
	server, err := mcp.StartAppServer(loadDisasmFile, ui.Config.CommentsPath)
	if err != nil {
		ui.LoadError = fmt.Errorf("unable to start MCP server: %w", err)
		ui.invalidateMain()
		return
	}
	ui.MCP = server
	if ui.File != nil {
		ui.MCP.SetPath(ui.Config.Path, ui.Comments)
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
