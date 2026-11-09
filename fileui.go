package main

import (
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
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
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/component"
	"gioui.org/x/explorer"

	"loov.dev/lensm/internal/codeview"
	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/pprof"

	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/mcp"
	"loov.dev/lensm/internal/syntax"
)

type FileUIConfig struct {
	Path         string
	Watch        bool
	Context      int
	CommentsPath string
	// ProfilePath is a pprof profile of the binary to show.
	ProfilePath string
	// BaseProfilePath is a profile to compare the profile with, in the
	// graphs.
	BaseProfilePath string
	// ProfileSample names the profile's value to show, such as
	// "alloc_space bytes"; empty is the profile's default.
	ProfileSample string
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
	// graphSplit divides the code from the profile graph on its right.
	graphSplit component.Resize

	// Window chrome.
	AppMenu        gui.Menu
	GoToButton     widget.Clickable
	BackButton     widget.Clickable
	ForwardButton  widget.Clickable
	BackTip        gui.Tooltip
	ForwardTip     gui.Tooltip
	SettingsTip    gui.Tooltip
	QuickOpen      gui.QuickOpen[disasm.Func]
	EmptyOpen      widget.Clickable
	SettingsButton widget.Clickable
	// Panel is the sidebar panel picked in the icon strip.
	Panel        int
	PanelButtons [panelCount]widget.Clickable
	PanelTips    [panelCount]gui.Tooltip
	WatchButton  widget.Clickable
	WatchTip     gui.Tooltip
	CommentsList gui.ClickList
	MarkToggles  [len(markTitles)]widget.Bool
	NoteToggles  [len(noteTitles)]widget.Bool
	MarksList    gui.ClickList
	// ClearComments deletes every comment on its second click in a row.
	ClearComments      widget.Clickable
	clearCommentsArmed bool
	HistoryList        gui.ClickList
	CallersList        gui.ClickList
	SummaryList        gui.ClickList
	SummarySelect      gui.Select

	// Column toggles and settings.
	ShowGoAsm     widget.Bool
	ShowNativeAsm widget.Bool
	// ShowDiagnostics shows the marks and notes, all of them at once.
	ShowDiagnostics widget.Bool
	ShowSource      widget.Bool
	ShowAsmHelp     widget.Bool
	HoverLinksOnly  widget.Bool
	SyncScroll      widget.Bool
	// SyncScrollSetting is the same switch in the settings, a widget of
	// its own since both can be on screen.
	SyncScrollSetting widget.Bool
	SettingsNav       gui.NavList
	ThemeSelect       gui.Select
	SyntaxSelect      gui.Select
	SettingsOK        widget.Clickable
	SettingsCancel    widget.Clickable
	Comment           codeview.CommentEditor
	Find              findBar
	Shortcuts         shortcutSheet
	Compare           compareView
	Graph             callGraph
	// ShowGraph puts the whole profile's graph in place of the code.
	ShowGraph    bool
	ProfileGraph callGraph
	// GraphButton on the right strip shows and hides the profile graph.
	GraphButton widget.Clickable
	GraphTip    gui.Tooltip
	// GraphFull gives the profile graph the room of the sidebar and the
	// code as well.
	GraphFull    bool
	GraphFullBtn widget.Clickable
	GraphFullTip gui.Tooltip
	// ProfileMenu picks the profile value to show.
	ProfileMenu gui.Menu
	// GraphTools are the controls above the profile graph.
	GraphTools graphTools
	// BaseProfile is the profile the graphs show the change from.
	BaseProfile *pprof.Profile
	// profileModTime is when the loaded profile file last changed.
	profileModTime time.Time
	TextSizeEditor widget.Editor
	// textSizeEdited marks TextSizeEditor as changed since it was applied.
	textSizeEdited bool
	// settingsPath is where Settings are saved, empty for none.
	settingsPath string
	StartMCP     widget.Clickable
	StopMCP      widget.Clickable

	// Index is the scan of the file for callers and stats, nil until
	// it finishes.
	Index *disasm.Index
	// Profile is the loaded profile, nil without one, and ProfileFlat
	// its samples per function, in File.Funcs order.
	Profile     *pprof.Profile
	ProfileFlat []int64

	Comments *comments.Store
	MCP      *mcp.AppServer

	window *app.Window
	// menuQueue holds the menu bar items chosen since the last frame;
	// menuStates the checkmarks and enabled items last sent to it.
	menuQueue  []int
	menuStates map[int]uint8
	// fullscreen hides the window buttons, so the toolbar needs no
	// room for them; titleDrag tracks the pointer over its empty space.
	fullscreen    bool
	titleDrag     struct{}
	picker        *explorer.Explorer
	loader        *loader
	invalidate    chan struct{}
	pickerResults chan pickerResult
	codeResults   chan codeResult
	indexResults  chan indexResult
	// summary is the order of the summary panel, by summaryKey, for
	// the index summaryFor.
	summary           []int
	summaryKey        int
	summaryFor        *disasm.Index
	summaryProfile    []int64
	summarySortedBy   int
	exited            chan struct{}
	flushTimer        *time.Timer
	sessionDirty      bool
	copyStatus        string
	copyStatusAt      time.Time
	saveError         string
	settingsOpen      bool
	settingsSnapshot  AppSettings
	pickerOpen        bool
	loadGeneration    uint64
	loadedPath        string
	Navigation        NavigationHistory
	navigatingHistory bool
}

// What a file pick is for.
type pickKind uint8

const (
	pickBinary pickKind = iota
	pickProfile
	// pickCompare loads a binary to compare the open one with.
	pickCompare
	// pickBaseProfile loads a profile to compare the profile with.
	pickBaseProfile
)

type pickerResult struct {
	kind pickKind
	path string
	// file is the loaded binary of a pickCompare.
	file disasm.File
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
	ui.Funcs = gui.NewFilterList[disasm.Func]()
	ui.ActiveTab = -1
	ui.Navigation.Reset()
	ui.Tabs.List.Axis = layout.Horizontal
	ui.ShowGoAsm.Value = settings.ShowGoAsm
	ui.ShowNativeAsm.Value = settings.ShowNativeAsm
	ui.ShowDiagnostics.Value = !settings.HideDiagnostics
	ui.ShowSource.Value = settings.ShowSource
	ui.ShowAsmHelp.Value = settings.ShowAsmHelp
	ui.HoverLinksOnly.Value = settings.HoverLinksOnly
	ui.SyncScroll.Value = settings.SyncScroll
	ui.SyncScrollSetting.Value = settings.SyncScroll
	ui.split.Axis = layout.Horizontal
	ui.split.Ratio = settings.SidebarRatio
	ui.graphSplit.Axis = layout.Horizontal
	ui.graphSplit.Ratio = settings.GraphRatio
	ui.Comment.SingleLine = true
	ui.TextSizeEditor.SingleLine = true
	ui.TextSizeEditor.Submit = true
	ui.TextSizeEditor.SetText(strconv.Itoa(settings.TextSize))
	ui.Comments = comments.NewMemory("")
	ui.QuickOpen.Lookup = func(pattern string) (disasm.Func, bool) {
		pc, ok := parseAddress(pattern)
		if !ok {
			return nil, false
		}
		fn := ui.findPC(pc)
		return fn, fn != nil
	}
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
		// A package build keeps its binary in a temporary directory
		// until the file closes.
		if ui.File != nil {
			_ = ui.File.Close()
			ui.File = nil
		}
		ui.setCompare("", nil)
	}()

	ui.window = w
	loader := newLoader(loadFile, ui.Config.Watch)
	ui.loader = loader
	defer loader.Close()
	picker := explorer.NewExplorer(w)
	ui.picker = picker
	invalidate := make(chan struct{}, 1)
	ui.invalidate = invalidate
	pickerResults := make(chan pickerResult, 1)
	ui.pickerResults = pickerResults
	codeResults := make(chan codeResult)
	ui.codeResults = codeResults
	indexResults := make(chan indexResult)
	ui.indexResults = indexResults
	ui.exited = exited
	profileTick := time.NewTicker(time.Second)
	defer profileTick.Stop()
	flushTimer := time.NewTimer(time.Hour)
	flushTimer.Stop()
	ui.flushTimer = flushTimer
	defer func() {
		// The selected rows change without a save; keep them too.
		ui.saveSessionState()
		ui.flushPending()
	}()
	defer func() {
		ui.window = nil
		ui.picker = nil
		ui.loader = nil
		ui.invalidate = nil
		ui.pickerResults = nil
		ui.codeResults = nil
		ui.indexResults = nil
		ui.exited = nil
		ui.flushTimer = nil
	}()

	if ui.Config.Path != "" {
		ui.requestLoad(ui.Config.Path)
	}
	if nativeMenu {
		installMenu(menuBar)
	}

	var bar titleBar
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
		case <-windowClosed:
			return nil
		case item := <-menuActions:
			ui.menuQueue = append(ui.menuQueue, item)
			w.Invalidate()
		case res := <-indexResults:
			ui.setIndex(res)
			w.Invalidate()
		case res := <-codeResults:
			ui.setTabCode(res)
			w.Invalidate()
		case <-profileTick.C:
			if ui.profileChanged() {
				ui.loadProfile()
				w.Invalidate()
			}
		case <-flushTimer.C:
			ui.flushPending()
			w.Invalidate()
		case res := <-pickerResults:
			ui.pickerOpen = false
			if res.err != nil {
				ui.LoadError = res.err
			} else if res.ok && res.kind == pickProfile {
				ui.Config.ProfilePath, ui.Config.ProfileSample = res.path, ""
				ui.loadProfile()
			} else if res.ok && res.kind == pickBaseProfile {
				ui.Config.BaseProfilePath = res.path
				ui.loadBaseProfile()
			} else if res.ok && res.kind == pickCompare {
				ui.setCompare(res.path, res.file)
			} else if res.ok {
				ui.Config.Path = res.path
				ui.LoadError = nil
				ui.copyStatus = ""
				ui.requestLoad(res.path)
			}
			w.Invalidate()
		case e := <-events:
			picker.ListenEvents(e)
			bar.event(e)
			switch e := e.(type) {
			case app.ConfigEvent:
				ui.fullscreen = e.Config.Mode == app.Fullscreen
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
		// Keep the rows selected in the binary being left.
		ui.saveSessionState()
	}
	if !reload {
		// Each binary has its own profile. The first binary keeps one
		// given on the command line.
		session, _ := ui.Settings.session(path)
		if ui.File != nil || ui.Config.ProfilePath == "" {
			ui.Config.ProfilePath, ui.Config.ProfileSample = session.Profile, session.ProfileSample
			ui.Config.BaseProfilePath = session.BaseProfile
		}
	}
	ui.loadedPath = path
	// A reload keeps each tab where it was, matched by function name.
	previous := map[string]*CodeTab{}
	if reload {
		for _, tab := range ui.CodeTabs {
			previous[tab.Name] = tab
		}
	}

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
	// A binary opened before gets its own tabs back, each at the row
	// that was selected.
	rows := map[string]comments.Coord{}
	if session, ok := ui.Settings.session(path); ok && !reload && len(session.Tabs) > 0 {
		openNames = openNames[:0]
		for _, tab := range session.Tabs {
			openNames = append(openNames, tab.Func)
			if tab.View != "" {
				rows[tab.Func] = comments.Coord{View: tab.View, File: tab.File, Line: tab.Line, PC: tab.PC}
			}
		}
		activeName = session.Active
		if activeName == "" {
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
	ui.QuickOpen.SetItems(file.Funcs())

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
		} else if row, ok := rows[name]; ok {
			tab.Code.Reveal(row)
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

func (ui *FileUI) findPC(pc uint64) disasm.Func {
	if ui.File == nil {
		return nil
	}
	return disasm.FindPC(ui.File, pc)
}

// parseAddress reads a hex address such as 0x4a2f10, as stack traces
// and profiles print them.
func parseAddress(s string) (uint64, bool) {
	s, ok := strings.CutPrefix(strings.TrimSpace(s), "0x")
	if !ok {
		return 0, false
	}
	pc, err := strconv.ParseUint(s, 16, 64)
	return pc, err == nil
}

func (ui *FileUI) Layout(gtx layout.Context) {
	colors := ui.Theme.Colors
	paint.FillShape(gtx.Ops, colors.Background, clip.Rect{Max: gtx.Constraints.Max}.Op())

	event.Op(gtx.Ops, ui)
	ui.handleActions(gtx)

	layout.Flex{
		Axis: layout.Vertical,
	}.Layout(gtx,
		layout.Rigid(ui.layoutToolbar),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(ui.layoutStrip),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return ui.layoutContent(gtx, colors)
				}),
				layout.Rigid(ui.layoutRightStrip),
			)
		}),
		layout.Rigid(ui.layoutStatus),
	)

	if fn, ok := ui.QuickOpen.Update(gtx); ok {
		tab := ui.openTab(fn, false)
		if pc, ok := parseAddress(ui.QuickOpen.Editor.Text()); ok && tab != nil && fn == ui.findPC(pc) {
			view := comments.ViewGoAsm
			if !ui.ShowGoAsm.Value {
				view = comments.ViewNativeAsm
			}
			tab.Code.Reveal(comments.Coord{View: view, PC: pc})
		}
		// The arrows step through the opened code at once.
		if code := ui.activeCode(); code != nil {
			gtx.Execute(key.FocusCmd{Tag: code})
		}
	}
	ui.QuickOpen.Layout(gtx, ui.Theme, "Go to function")
	if ui.settingsOpen {
		ui.layoutSettings(gtx)
	}
	ui.layoutShortcuts(gtx)
}

func (ui *FileUI) handleActions(gtx layout.Context) {
	// widget.Editor claims Option+arrow word-jumps on macOS, and this
	// unfocused poll runs before any editor's Update, so it would steal
	// them from a focused editor. Only listen for Alt+arrows while no
	// text editor has focus.
	editorFocused := gtx.Focused(&ui.Comment) || gtx.Focused(&ui.Funcs.Filter) || gtx.Focused(&ui.QuickOpen.Editor) || gtx.Focused(&ui.Find.Editor)
	filters := []event.Filter{
		key.Filter{Required: key.ModShortcut, Name: key.Name("B")},
		key.Filter{Required: key.ModShortcut, Name: key.Name("[")},
		key.Filter{Required: key.ModShortcut, Name: key.Name("]")},
		key.Filter{Required: key.ModShortcut, Name: key.Name("W")},
		key.Filter{Required: key.ModShortcut, Name: key.Name("F")},
		key.Filter{Required: key.ModShortcut, Optional: key.ModShift, Name: key.Name("G")},
		key.Filter{Required: key.ModShortcut, Name: key.Name("O")},
		key.Filter{Required: key.ModShortcut, Name: key.Name("P")},
		key.Filter{Required: key.ModShortcut, Name: key.Name("R")},
		key.Filter{Required: key.ModShortcut, Name: key.Name(",")},
		key.Filter{Required: key.ModCtrl, Optional: key.ModShift, Name: key.NameTab},
	}
	if !editorFocused {
		filters = append(filters,
			key.Filter{Required: key.ModAlt, Name: key.NameLeftArrow},
			key.Filter{Required: key.ModAlt, Name: key.NameRightArrow},
			key.Filter{Name: "?", Optional: key.ModShift},
			key.Filter{Name: "/", Required: key.ModShift},
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
		// The settings dialog keeps its own keys.
		if ui.settingsOpen {
			continue
		}
		switch keyEvent.Name {
		case key.NameLeftArrow, key.Name("["):
			ui.navigateBack()
		case key.NameRightArrow, key.Name("]"):
			ui.navigateForward()
		case "?", "/":
			ui.Shortcuts.Open = true
		case key.Name("B"):
			ui.showSidebar(ui.Settings.HideSidebar)
		case key.Name("W"):
			ui.closeTab(ui.ActiveTab)
		case key.Name("F"):
			// In the code it finds text, elsewhere it filters functions.
			if code := ui.activeCode(); code != nil && (gtx.Focused(code) || gtx.Focused(&ui.Find.Editor)) {
				ui.openFind(gtx)
			} else {
				ui.focusFilter(gtx)
			}
		case key.Name("G"):
			if keyEvent.Modifiers.Contain(key.ModShift) {
				ui.stepFind(-1)
			} else {
				ui.stepFind(1)
			}
		case key.Name("O"):
			ui.chooseFile()
		case key.Name("P"):
			ui.QuickOpen.Show(gtx)
		case key.Name("R"):
			ui.reload()
		case key.Name(","):
			ui.openSettings()
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
	for ui.EmptyOpen.Clicked(gtx) {
		ui.chooseFile()
	}
	for ui.SettingsButton.Clicked(gtx) {
		ui.openSettings()
	}
	for ui.BackButton.Clicked(gtx) {
		ui.navigateBack()
	}
	for ui.ForwardButton.Clicked(gtx) {
		ui.navigateForward()
	}
	for ui.GoToButton.Clicked(gtx) {
		ui.QuickOpen.Show(gtx)
	}
	ui.handleAppMenu(gtx)
	ui.handleMenuBar(gtx)
	ui.handleColumnToggles(gtx)
}

// handleColumnToggles saves the column and diagnostics choices and
// keeps at least one column showing.
func (ui *FileUI) handleColumnToggles(gtx layout.Context) {
	changed := false
	for _, b := range []*widget.Bool{&ui.ShowGoAsm, &ui.ShowNativeAsm, &ui.ShowSource} {
		for b.Update(gtx) {
			changed = true
			if !ui.ShowGoAsm.Value && !ui.ShowNativeAsm.Value && !ui.ShowSource.Value {
				b.Value = true
			}
		}
	}
	for ui.ShowDiagnostics.Update(gtx) {
		changed = true
	}
	if changed {
		s := ui.Settings
		s.ShowGoAsm, s.ShowNativeAsm, s.ShowSource = ui.ShowGoAsm.Value, ui.ShowNativeAsm.Value, ui.ShowSource.Value
		s.HideDiagnostics = !ui.ShowDiagnostics.Value
		ui.saveSettings(s)
	}
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

func (ui *FileUI) chooseFile() { ui.pick(pickBinary) }

// chooseProfile picks a pprof profile of the open binary.
func (ui *FileUI) chooseProfile() {
	if ui.File != nil {
		ui.pick(pickProfile)
	}
}

func (ui *FileUI) pick(kind pickKind) {
	if ui.pickerOpen || ui.picker == nil || ui.pickerResults == nil {
		return
	}
	// The native picker blocks until dismissed; running it here would
	// stall the frame in progress and freeze the window.
	ui.pickerOpen = true
	picker, results, exited := ui.picker, ui.pickerResults, ui.exited
	go func() {
		res := pickerResult{kind: kind}
		// No extension filter: Go executables usually have none.
		file, err := picker.ChooseFile()
		switch {
		case err == nil:
			// The disassembler needs a path, not a reader; on desktop
			// platforms the explorer hands back an *os.File.
			if f, ok := file.(*os.File); ok {
				res.path, res.ok = f.Name(), true
			} else {
				res.err = errors.New("file picker did not return a local file path")
			}
			_ = file.Close()
		case errors.Is(err, explorer.ErrUserDecline):
		default:
			res.err = err
		}
		// Load a binary to compare with here, off the UI goroutine.
		if res.ok && kind == pickCompare {
			res.file, res.err = loadFile(res.path)
			res.ok = res.err == nil
		}
		select {
		case results <- res:
		case <-exited:
			if res.file != nil {
				_ = res.file.Close()
			}
		}
	}()
}

func (ui *FileUI) layoutContent(gtx layout.Context, colors gui.UIColors) layout.Dimensions {
	if ui.ShowGraph && ui.GraphFull && ui.Profile != nil && ui.File != nil {
		return ui.layoutProfileGraph(gtx)
	}
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

	content := ui.withGraph(func(gtx layout.Context) layout.Dimensions {
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
			layout.Rigid(ui.layoutCrumbs),
			layout.Rigid(ui.layoutFind),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				if ui.LoadError != nil && ui.File == nil {
					return layout.Dimensions{}
				}
				if ui.File == nil && ui.Config.Path == "" {
					return ui.layoutEmptyState(gtx)
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
				if ui.Compare.file != nil {
					return ui.layoutCompare(gtx, ui.activeTab())
				}

				gtx.Constraints = layout.Exact(gtx.Constraints.Max)
				return layout.Stack{
					Alignment: layout.SE,
				}.Layout(gtx,
					layout.Expanded(func(gtx layout.Context) layout.Dimensions {
						code.Notes = disasm.AllNotes &^ ui.Settings.HiddenNotes
						marks := disasm.AllMarks &^ ui.Settings.HiddenMarks
						if ui.Settings.HideDiagnostics {
							code.Notes, marks = 0, 0
						}
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
							ShowGo:         ui.ShowGoAsm.Value,
							ShowNative:     ui.ShowNativeAsm.Value,
							ShowSource:     ui.ShowSource.Value,
							ShowHelp:       ui.ShowAsmHelp.Value,
							HoverLinksOnly: ui.HoverLinksOnly.Value,
							SyncScroll:     ui.SyncScroll.Value,
							Marks:          marks,
							TextHeight:     ui.Theme.TextSize,
							Profile:        profileFlat(ui.Profile),
							ProfileTotal:   profileTotal(ui.Profile),
							ProfileCalls:   profileCalls(ui.Profile),
						}.Layout(gtx)
					}),
				)
			}),
		)
	})
	if ui.Settings.HideSidebar {
		return content(gtx)
	}
	return ui.split.Layout(gtx, ui.layoutPanel, content, ui.splitHandle)
}

// splitHandle is the line between two resizable panes.
func (ui *FileUI) splitHandle(gtx layout.Context) layout.Dimensions {
	size := image.Pt(gtx.Metric.Dp(8), gtx.Constraints.Max.Y)
	paint.FillShape(gtx.Ops, ui.Theme.Colors.Splitter, clip.Rect{
		Min: image.Pt(size.X/2, 0),
		Max: image.Pt(size.X/2+1, size.Y),
	}.Op())
	return layout.Dimensions{Size: size}
}

// withGraph puts the profile graph, when shown, right of content.
func (ui *FileUI) withGraph(content layout.Widget) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		if !ui.ShowGraph || ui.Profile == nil || ui.File == nil {
			return content(gtx)
		}
		total := float32(gtx.Constraints.Max.X)
		least := float32(gtx.Metric.Dp(240))
		ui.graphSplit.Ratio = min(max(ui.graphSplit.Ratio, least/total), 1-least/total)
		defer func() {
			if ui.graphSplit.Ratio != ui.Settings.GraphRatio {
				settings := ui.Settings
				settings.GraphRatio = ui.graphSplit.Ratio
				ui.saveSettings(settings)
			}
		}()
		return ui.graphSplit.Layout(gtx, content, ui.layoutProfileGraph, ui.splitHandle)
	}
}

// layoutEmptyState invites the user to open a binary when none is loaded.
func (ui *FileUI) layoutEmptyState(gtx layout.Context) layout.Dimensions {
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = min(gtx.Constraints.Max.X, gtx.Metric.Dp(420))
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				title := ui.Theme.Label("No binary open", 1.3)
				title.Font.Weight = font.SemiBold
				return layout.Inset{Bottom: 8}.Layout(gtx, title.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				txt := ui.Theme.Muted("Open a Go, C, C++, Rust or TinyGo executable, an object file or a wasm module. "+
					"You can also pass it on the command line: lensm <path>", 0.95)
				txt.Alignment = text.Middle
				return layout.Inset{Bottom: 16}.Layout(gtx, txt.Layout)
			}),
			layout.Rigid(gui.Button{Theme: ui.Theme, Clickable: &ui.EmptyOpen, Text: "Open binary\u2026", Primary: true}.Layout),
		)
	})
}

func (ui *FileUI) requestLoad(path string) {
	if ui.loader == nil {
		return
	}
	ui.loadGeneration++
	ui.loader.Request(ui.loadGeneration, path)
}

func (ui *FileUI) afterFileLoaded() {
	ui.startIndex()
	ui.loadProfile()
	ui.saveSessionState()
	if ui.MCP != nil {
		ui.MCP.SetFile(ui.Config.Path, ui.File.Acquire(), ui.Comments)
	}
}

func (ui *FileUI) saveSessionState() {
	if ui.File == nil || ui.loadedPath == "" {
		return
	}
	old, _ := ui.Settings.session(ui.loadedPath)
	oldRows := map[string]TabSession{}
	for _, tab := range old.Tabs {
		oldRows[tab.Func] = tab
	}
	session := Session{Path: ui.loadedPath, ProfileSample: ui.Config.ProfileSample}
	if ui.Config.ProfilePath != "" {
		session.Profile, _ = filepath.Abs(ui.Config.ProfilePath)
	}
	if ui.Config.BaseProfilePath != "" {
		session.BaseProfile, _ = filepath.Abs(ui.Config.BaseProfilePath)
	}
	for _, tab := range ui.CodeTabs {
		if tab.Name == "" || tab.Preview {
			continue
		}
		saved, ok := selectedRow(tab)
		if !ok {
			// Not shown since it reopened, or nothing selected.
			saved = oldRows[tab.Name]
		}
		saved.Func = tab.Name
		session.Tabs = append(session.Tabs, saved)
	}
	if tab := ui.activeTab(); tab != nil && !tab.Preview {
		session.Active = tab.Name
	}
	if ui.Settings.LastPath == ui.loadedPath && old.Active == session.Active && slices.Equal(old.Tabs, session.Tabs) &&
		old.Profile == session.Profile && old.ProfileSample == session.ProfileSample && old.BaseProfile == session.BaseProfile {
		return
	}

	settings := ui.Settings.withSession(session)
	settings.LastPath = ui.loadedPath
	ui.Settings = settings
	ui.sessionDirty = true
	ui.scheduleFlush()
}

// selectedRow is the row selected in tab, if any.
func selectedRow(tab *CodeTab) (TabSession, bool) {
	code := &tab.Code
	switch {
	case code.Code == nil:
	case code.SelectedView == codeview.ViewSource && code.SelectedLine > 0:
		return TabSession{View: code.SelectedView, File: code.SelectedFile, Line: code.SelectedLine}, true
	case 0 <= code.SelectedAsm && code.SelectedAsm < len(code.Code.Insts):
		return TabSession{View: code.SelectedView, PC: code.Code.Insts[code.SelectedAsm].PC}, true
	}
	return TabSession{}, false
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
