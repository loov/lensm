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
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/disasm"
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
	CopyAsm        widget.Clickable
	CopyCode       widget.Clickable
	OpenInNew      widget.Clickable
	ShowNativeAsm  widget.Bool
	Comment        widget.Editor
	TextSizeEditor widget.Editor
	ContextEditor  widget.Editor
	StartMCP       widget.Clickable
	StopMCP        widget.Clickable

	Comments *CommentStore
	MCP      *AppMCPServer

	loadRequests       chan fileLoadRequest
	invalidate         chan struct{}
	settingsEvents     chan event.Event
	settingsAcks       chan struct{}
	exited             chan struct{}
	commentKey         string
	copyStatus         string
	settingsWindowOpen bool
	loadGeneration     uint64
}

type CodeTab struct {
	Name  string
	Func  disasm.Func
	Code  CodeUI
	Tab   widget.Clickable
	Close widget.Clickable
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

func NewExeUI(windows *Windows, theme *material.Theme) *FileUI {
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
	ui.Tabs.List.Axis = layout.Horizontal
	ui.ShowNativeAsm.Value = settings.ShowNativeAsm
	ui.Comment.SingleLine = true
	ui.TextSizeEditor.SingleLine = true
	ui.TextSizeEditor.Submit = true
	ui.TextSizeEditor.SetText(strconv.Itoa(settings.TextSize))
	ui.ContextEditor.SingleLine = true
	ui.ContextEditor.Submit = true
	ui.ContextEditor.SetText(strconv.Itoa(settings.Context))
	ui.Comments, _ = NewCommentStore("", "")
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
	ui.exited = exited
	var settingsOps op.Ops
	defer func() {
		ui.loadRequests = nil
		ui.invalidate = nil
		ui.settingsEvents = nil
		ui.settingsAcks = nil
		ui.exited = nil
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
	if initialLoad && cleanPath(ui.Config.Path) == ui.Settings.LastPath && len(ui.Settings.OpenTabs) > 0 {
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

func (ui *FileUI) activeTab() *CodeTab {
	if !InRange(ui.ActiveTab, len(ui.CodeTabs)) {
		return nil
	}
	return ui.CodeTabs[ui.ActiveTab]
}

func (ui *FileUI) activeCode() *CodeUI {
	tab := ui.activeTab()
	if tab == nil {
		return nil
	}
	return &tab.Code
}

func (ui *FileUI) activeCodeLoaded() bool {
	code := ui.activeCode()
	return code != nil && code.Loaded()
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

func (ui *FileUI) appendCodeTab(fn disasm.Func) *CodeTab {
	if fn == nil {
		return nil
	}
	tab := &CodeTab{
		Name: fn.Name(),
		Func: fn,
	}
	tab.Code.Code, ui.LoadError = fn.Load(ui.loadOptions())
	tab.Code.SelectedAsm = -1
	tab.Code.SelectedView = CommentViewGoAsm
	tab.Code.ResetScroll()
	ui.CodeTabs = append(ui.CodeTabs, tab)
	return tab
}

func (ui *FileUI) openFuncTab(fn disasm.Func) *CodeTab {
	if fn == nil {
		return nil
	}
	name := fn.Name()
	for i, tab := range ui.CodeTabs {
		if tab.Name == name {
			tab.Func = fn
			ui.ActiveTab = i
			ui.selectFuncByName(name)
			ui.commentKey = ""
			ui.saveSessionState()
			return tab
		}
	}

	tab := ui.appendCodeTab(fn)
	if tab == nil {
		return nil
	}
	ui.ActiveTab = len(ui.CodeTabs) - 1
	ui.selectFuncByName(name)
	ui.commentKey = ""
	ui.saveSessionState()
	return tab
}

func (ui *FileUI) openFuncTabNext(fn disasm.Func) *CodeTab {
	if fn == nil {
		return nil
	}
	name := fn.Name()
	for i, tab := range ui.CodeTabs {
		if tab.Name == name {
			tab.Func = fn
			ui.ActiveTab = i
			ui.selectFuncByName(name)
			ui.commentKey = ""
			ui.saveSessionState()
			return tab
		}
	}

	tab := ui.appendCodeTab(fn)
	if tab == nil {
		return nil
	}
	index := len(ui.CodeTabs) - 1
	if InRange(ui.ActiveTab, index) {
		next := ui.ActiveTab + 1
		if next < index {
			copy(ui.CodeTabs[next+1:], ui.CodeTabs[next:index])
			ui.CodeTabs[next] = tab
			index = next
		}
	}
	ui.ActiveTab = index
	ui.selectFuncByName(name)
	ui.commentKey = ""
	ui.saveSessionState()
	return tab
}

func (ui *FileUI) selectTab(index int) {
	if !InRange(index, len(ui.CodeTabs)) {
		return
	}
	ui.ActiveTab = index
	ui.commentKey = ""
	ui.selectFuncByName(ui.CodeTabs[index].Name)
	ui.saveSessionState()
}

func (ui *FileUI) closeTab(index int) {
	if !InRange(index, len(ui.CodeTabs)) {
		return
	}
	ui.CodeTabs = append(ui.CodeTabs[:index], ui.CodeTabs[index+1:]...)
	switch {
	case len(ui.CodeTabs) == 0:
		ui.ActiveTab = -1
		ui.commentKey = ""
		ui.Funcs.Selected = ""
		ui.Funcs.SelectedItem = nil
		ui.Funcs.List.Selected = -1
	case ui.ActiveTab > index:
		ui.ActiveTab--
	case ui.ActiveTab == index && ui.ActiveTab >= len(ui.CodeTabs):
		ui.ActiveTab = len(ui.CodeTabs) - 1
	}
	if tab := ui.activeTab(); tab != nil {
		ui.selectFuncByName(tab.Name)
	} else {
		ui.commentKey = ""
	}
	ui.saveSessionState()
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
	for ui.BrowseButton.Clicked(gtx) {
		ui.chooseFile()
	}
	for ui.SettingsButton.Clicked(gtx) {
		ui.openSettingsWindow()
	}
	for ui.CopyAsm.Clicked(gtx) {
		ui.copyAssembly(gtx)
	}
	for ui.CopyCode.Clicked(gtx) {
		ui.copySourceCode(gtx)
	}
	for ui.OpenInNew.Clicked(gtx) {
		if ui.activeCodeLoaded() {
			ui.openInNew(gtx)
		}
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
				button := material.IconButton(ui.Theme, &ui.CopyAsm, CopyIcon, "Copy assembly")
				button.Size = 18
				button.Inset = layout.UniformInset(8)
				if !ui.activeCodeLoaded() {
					gtx = gtx.Disabled()
				}
				return layout.Inset{Left: 4}.Layout(gtx, button.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				button := material.Button(ui.Theme, &ui.CopyCode, "Copy code")
				button.Inset = layout.Inset{Top: 6, Right: 10, Bottom: 6, Left: 10}
				code := ui.activeCode()
				if code == nil || !code.Loaded() || len(code.Source) == 0 {
					gtx = gtx.Disabled()
				}
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
				return ui.layoutSyntaxRadio(gtx, colors, SyntaxStyleGoLand)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutSyntaxRadio(gtx, colors, SyntaxStyleDarcula)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutSyntaxRadio(gtx, colors, SyntaxStyleMono)
			}),
		)
	})
}

func (ui *FileUI) layoutSyntaxRadio(gtx layout.Context, colors UIColors, style string) layout.Dimensions {
	radio := material.RadioButton(ui.Theme, &ui.SyntaxStyle, style, SyntaxStyleLabel(style))
	radio.Color = colors.MutedText
	radio.IconColor = ui.Theme.ContrastBg
	radio.TextSize = ui.Theme.TextSize * 0.78
	radio.Size = unit.Dp(18)
	return layout.Inset{Left: 2}.Layout(gtx, radio.Layout)
}

func (ui *FileUI) openSettingsWindow() {
	if ui.settingsWindowOpen || ui.settingsEvents == nil {
		return
	}
	ui.settingsWindowOpen = true
	events, acks, exited := ui.settingsEvents, ui.settingsAcks, ui.exited
	ui.Windows.Open("lensm settings", image.Pt(520, 360), func(w *app.Window) error {
		// Only pump events here: the settings window is laid out on the
		// main event loop, because layout reads and mutates state shared
		// with the main window (Settings, MCP, Config, widget state) and
		// shapes text through the Theme's Shaper, which is not safe for
		// concurrent use.
		for {
			ev := w.Event()
			select {
			case events <- ev:
			case <-exited:
				return nil
			}
			select {
			case <-acks:
			case <-exited:
				return nil
			}
			if e, ok := ev.(app.DestroyEvent); ok {
				return e.Err
			}
		}
	})
}

func (ui *FileUI) layoutSettingsWindow(gtx layout.Context) layout.Dimensions {
	colors := ApplyTheme(ui.Theme, ui.Dark.Value)
	paint.FillShape(gtx.Ops, colors.Background, clip.Rect{Max: gtx.Constraints.Max}.Op())
	ui.handleSettingsActions(gtx)

	return layout.UniformInset(14).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				title := material.H6(ui.Theme, "Settings")
				title.Color = colors.Text
				return layout.Inset{Bottom: 12}.Layout(gtx, title.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return ui.layoutSettingsSection(gtx, colors, "Visual", []layout.FlexChild{
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(material.Switch(ui.Theme, &ui.Dark, "Dark theme").Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								label := material.Body1(ui.Theme, "Dark theme")
								label.Color = colors.Text
								return layout.Inset{Left: 6}.Layout(gtx, label.Layout)
							}),
						)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						check := material.CheckBox(ui.Theme, &ui.ShowNativeAsm, "Native asm")
						check.Color = colors.Text
						return check.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.layoutSyntaxSelector(gtx, colors)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return ui.layoutLabeledEditor(gtx, colors, "Text size", &ui.TextSizeEditor)
					}),
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return ui.layoutSettingsSection(gtx, colors, "MCP", []layout.FlexChild{
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return ui.layoutLabeledEditor(gtx, colors, "Source context", &ui.ContextEditor)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							status := "Stopped"
							if ui.MCP != nil {
								status = "Running: " + ui.MCP.URL()
							}
							label := material.Body1(ui.Theme, status)
							label.Color = colors.MutedText
							return layout.Inset{Top: 6, Bottom: 6}.Layout(gtx, label.Layout)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if ui.MCP != nil {
										gtx = gtx.Disabled()
									}
									button := material.Button(ui.Theme, &ui.StartMCP, "Start MCP")
									return button.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if ui.MCP == nil {
										gtx = gtx.Disabled()
									}
									button := material.Button(ui.Theme, &ui.StopMCP, "Stop MCP")
									return layout.Inset{Left: 8}.Layout(gtx, button.Layout)
								}),
							)
						}),
					})
				})
			}),
		)
	})
}

func (ui *FileUI) layoutSettingsSection(gtx layout.Context, colors UIColors, title string, children []layout.FlexChild) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		append([]layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme, title)
				label.TextSize *= 0.9
				label.Color = colors.MutedText
				return layout.Inset{Bottom: 6}.Layout(gtx, label.Layout)
			}),
		}, children...)...,
	)
}

func (ui *FileUI) layoutLabeledEditor(gtx layout.Context, colors UIColors, labelText string, editor *widget.Editor) layout.Dimensions {
	return layout.Inset{Top: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme, labelText)
				label.Color = colors.Text
				return layout.Inset{Right: 8}.Layout(gtx, label.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(gtx.Metric.Dp(80), gtx.Metric.Dp(34)))
				return FocusBorder(ui.Theme, gtx.Focused(editor)).Layout(gtx,
					material.Editor(ui.Theme, editor, "").Layout)
			}),
		)
	})
}

func (ui *FileUI) handleSettingsActions(gtx layout.Context) {
	changedVisual := false
	for ui.Dark.Update(gtx) {
		changedVisual = true
	}
	for ui.ShowNativeAsm.Update(gtx) {
		changedVisual = true
	}
	for ui.SyntaxStyle.Update(gtx) {
		ui.SyntaxStyle.Value = NormalizeSyntaxStyle(ui.SyntaxStyle.Value)
		changedVisual = true
	}
	if ui.updatePositiveIntEditor(gtx, &ui.TextSizeEditor, func(value int) {
		ui.Theme.TextSize = unit.Sp(value)
	}) {
		changedVisual = true
	}
	if changedVisual {
		ui.saveVisualSettings()
		gtx.Execute(op.InvalidateCmd{})
		ui.invalidateMain()
	}

	contextChanged := ui.updatePositiveIntEditor(gtx, &ui.ContextEditor, func(value int) {
		ui.Config.Context = value
	})
	if contextChanged {
		ui.saveMCPSettings()
		if ui.MCP != nil {
			ui.stopMCP()
			ui.startMCP()
		}
	}
	for ui.StartMCP.Clicked(gtx) {
		ui.startMCP()
		gtx.Execute(op.InvalidateCmd{})
	}
	for ui.StopMCP.Clicked(gtx) {
		ui.stopMCP()
		gtx.Execute(op.InvalidateCmd{})
	}
}

func (ui *FileUI) updatePositiveIntEditor(gtx layout.Context, editor *widget.Editor, apply func(int)) bool {
	changed := false
	for {
		ev, ok := editor.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent, widget.SubmitEvent:
			changed = true
		}
	}
	if !changed {
		return false
	}
	value, err := strconv.Atoi(strings.TrimSpace(editor.Text()))
	if err != nil || value <= 0 {
		return false
	}
	apply(value)
	return true
}

func (ui *FileUI) saveVisualSettings() {
	settings := ui.Settings
	settings.Dark = ui.Dark.Value
	settings.ShowNativeAsm = ui.ShowNativeAsm.Value
	settings.SyntaxStyle = NormalizeSyntaxStyle(ui.SyntaxStyle.Value)
	if value, err := strconv.Atoi(strings.TrimSpace(ui.TextSizeEditor.Text())); err == nil && value > 0 {
		settings.TextSize = value
	}
	ui.saveSettings(settings)
}

func (ui *FileUI) saveMCPSettings() {
	settings := ui.Settings
	if value, err := strconv.Atoi(strings.TrimSpace(ui.ContextEditor.Text())); err == nil && value > 0 {
		settings.Context = value
	}
	ui.saveSettings(settings)
}

func (ui *FileUI) saveSettings(settings AppSettings) {
	if err := SaveAppSettings(settings); err != nil {
		fmt.Fprintf(os.Stderr, "unable to save settings: %v\n", err)
		return
	}
	ui.Settings = settings
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

func (ui *FileUI) startMCP() {
	if ui.MCP != nil {
		return
	}
	server, err := StartAppMCPServer(ui.Config.Context, ui.Config.CommentsPath)
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

func (ui *FileUI) chooseFile() {
	path, ok, err := chooseExecutableFile()
	if err != nil {
		ui.LoadError = err
		return
	}
	if !ok {
		return
	}
	ui.Config.Path = path
	ui.LoadError = nil
	ui.copyStatus = ""
	ui.requestLoad(path)
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

								TryOpen:          ui.tryOpen,
								CommentFor:       ui.commentFor,
								NativeCommentFor: ui.nativeCommentFor,
								SourceCommentFor: ui.sourceCommentFor,
								CommentKey:       &ui.commentKey,
								CommentKeyFor:    ui.commentKeyFor,
								SetComment:       ui.setCommentForInst,
								SetNativeComment: ui.setNativeCommentForInst,
								SetSourceComment: ui.setSourceCommentForLine,
								CommentEditor:    &ui.Comment,

								Theme:      ui.Theme,
								Colors:     colors,
								Syntax:     SyntaxPaletteFor(ui.Settings.SyntaxStyle, colors),
								ShowNative: ui.ShowNativeAsm.Value,
								TextHeight: ui.Theme.TextSize,
							}.Layout(gtx)
						}),
						layout.Stacked(func(gtx layout.Context) layout.Dimensions {
							code := ui.activeCode()
							if code == nil || !code.Loaded() {
								return layout.Dimensions{}
							}
							button := material.IconButton(ui.Theme, &ui.OpenInNew, OpenInNewIcon, "Open in separate window")
							button.Size = 16
							button.Inset = layout.UniformInset(12)
							return layout.UniformInset(2).Layout(gtx, button.Layout)
						}),
					)
				}),
			)
		}),
	)
}

func (ui *FileUI) layoutCodeTabs(gtx layout.Context, colors UIColors) layout.Dimensions {
	for i := 0; i < len(ui.CodeTabs); i++ {
		tab := ui.CodeTabs[i]
		for tab.Tab.Clicked(gtx) {
			ui.selectTab(i)
		}
		closed := false
		for tab.Close.Clicked(gtx) {
			ui.closeTab(i)
			closed = true
		}
		if closed {
			i--
		}
	}

	if len(ui.CodeTabs) == 0 {
		return layout.Dimensions{}
	}

	height := gtx.Metric.Dp(22)
	if height < 20 {
		height = 20
	}
	availableWidth := gtx.Constraints.Max.X
	gtx.Constraints = layout.Exact(image.Pt(availableWidth, height))
	paint.FillShape(gtx.Ops, colors.SecondaryBackground, clip.Rect{Max: gtx.Constraints.Max}.Op())

	tabWidth := gtx.Metric.Dp(220)
	minTabWidth := gtx.Metric.Dp(120)
	if len(ui.CodeTabs) <= 3 && availableWidth > 0 {
		tabWidth = min(tabWidth, max(minTabWidth, availableWidth/len(ui.CodeTabs)))
	}
	if tabWidth < minTabWidth {
		tabWidth = minTabWidth
	}

	list := material.List(ui.Theme, &ui.Tabs)
	list.AnchorStrategy = material.Overlay
	return list.Layout(gtx, len(ui.CodeTabs), func(gtx layout.Context, index int) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(tabWidth, height))
		return ui.layoutCodeTab(gtx, colors, ui.CodeTabs[index], index == ui.ActiveTab)
	})
}

func (ui *FileUI) layoutCodeTab(gtx layout.Context, colors UIColors, tab *CodeTab, active bool) layout.Dimensions {
	size := gtx.Constraints.Max
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()

	bg := colors.SecondaryBackground
	if active {
		bg = colors.Background
	} else if tab.Tab.Hovered() {
		bg = colors.Selection
	}
	paint.FillShape(gtx.Ops, bg, clip.Rect{Max: size}.Op())
	if active {
		paint.FillShape(gtx.Ops, ui.Theme.ContrastBg, clip.Rect{Max: image.Pt(size.X, 2)}.Op())
	}
	paint.FillShape(gtx.Ops, colors.Splitter, clip.Rect{
		Min: image.Pt(size.X-1, 0),
		Max: size,
	}.Op())

	layout.Inset{Top: 1, Right: 4, Bottom: 1, Left: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(gtx.Constraints.Max)
				return tab.Tab.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					size := gtx.Constraints.Max
					defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()

					label := material.Body1(ui.Theme, tab.Name)
					label.MaxLines = 1
					label.TextSize = ui.Theme.TextSize * 8 / 10
					label.Color = colors.Text
					if active {
						label.Font.Weight = font.Black
					}
					dims := layout.W.Layout(gtx, label.Layout)
					return layout.Dimensions{Size: size, Baseline: dims.Baseline}
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				closeWidth := gtx.Metric.Dp(22)
				gtx.Constraints = layout.Exact(image.Pt(closeWidth, gtx.Constraints.Max.Y))
				return tab.Close.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					size := gtx.Constraints.Max
					if tab.Close.Hovered() {
						paint.FillShape(gtx.Ops, colors.Selection, clip.Rect{Max: size}.Op())
					}
					label := material.Body1(ui.Theme, "x")
					label.MaxLines = 1
					label.TextSize = ui.Theme.TextSize * 8 / 10
					label.Color = colors.MutedText
					dims := layout.Center.Layout(gtx, label.Layout)
					return layout.Dimensions{Size: size, Baseline: dims.Baseline}
				})
			}),
		)
	})

	return layout.Dimensions{Size: size}
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
	path := cleanPath(ui.Config.Path)
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

	if err := SaveAppSettings(settings); err != nil {
		fmt.Fprintf(os.Stderr, "unable to save settings: %v\n", err)
		return
	}
	ui.Settings = settings
}


func (ui *FileUI) loadCommentsForPath(exePath string) {
	commentsPath := ui.Config.CommentsPath
	if commentsPath == "" {
		commentsPath = defaultCommentPath(exePath)
	}
	comments, err := NewCommentStore(commentsPath, exePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to load comments from %q: %v\n", commentsPath, err)
		comments, _ = NewCommentStore("", exePath)
	}
	ui.Comments = comments
}

func (ui *FileUI) commentKeyFor(inst disasm.Inst) string {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return ""
	}
	return ui.commentKeyForCode(code.Code, inst)
}

func (ui *FileUI) commentKeyForCode(code *disasm.Code, inst disasm.Inst) string {
	if code == nil {
		return ""
	}
	return fmt.Sprintf("%s:%s:%x", code.Name, CommentViewGoAsm, inst.PC)
}

func (ui *FileUI) commentFor(inst disasm.Inst) string {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return ""
	}
	return ui.Comments.ForAsm(code.Name, CommentViewGoAsm, inst.PC)
}

func (ui *FileUI) nativeCommentFor(inst disasm.Inst) string {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return ""
	}
	return ui.Comments.ForAsm(code.Name, CommentViewNativeAsm, inst.PC)
}

func (ui *FileUI) sourceCommentFor(file string, line int) string {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return ""
	}
	return ui.Comments.ForSource(code.Name, file, line)
}

func (ui *FileUI) setCommentForInst(inst disasm.Inst, text string) {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return
	}
	if err := ui.Comments.SetAsm(code.Name, CommentViewGoAsm, inst.PC, text); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func (ui *FileUI) setNativeCommentForInst(inst disasm.Inst, text string) {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return
	}
	if err := ui.Comments.SetAsm(code.Name, CommentViewNativeAsm, inst.PC, text); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func (ui *FileUI) setSourceCommentForLine(file string, line int, text string) {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return
	}
	if err := ui.Comments.SetSource(code.Name, file, line, text); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

func (ui *FileUI) copyAssembly(gtx layout.Context) {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return
	}

	var text strings.Builder
	for _, inst := range code.Insts {
		text.WriteString(inst.Text)
		if comment := ui.commentFor(inst); comment != "" {
			text.WriteString(" ; ")
			text.WriteString(comment)
		}
		text.WriteByte('\n')
	}
	gtx.Execute(clipboard.WriteCmd{
		Type: "text/plain",
		Data: io.NopCloser(strings.NewReader(text.String())),
	})
	ui.copyStatus = "Copied"
}

func (ui *FileUI) copySourceCode(gtx layout.Context) {
	code := ui.activeCode()
	if code == nil || !code.Loaded() || len(code.Source) == 0 {
		return
	}

	var text strings.Builder
	multipleFiles := len(code.Source) > 1
	for srcIndex, src := range code.Source {
		if srcIndex > 0 {
			text.WriteByte('\n')
		}
		if multipleFiles {
			text.WriteString("// ")
			text.WriteString(src.File)
			text.WriteByte('\n')
		}
		for blockIndex, block := range src.Blocks {
			if blockIndex > 0 {
				text.WriteByte('\n')
			}
			for _, line := range block.Lines {
				text.WriteString(line)
				text.WriteByte('\n')
			}
		}
	}

	gtx.Execute(clipboard.WriteCmd{
		Type: "text/plain",
		Data: io.NopCloser(strings.NewReader(text.String())),
	})
	ui.copyStatus = "Copied code"
}

func (ui *FileUI) tryOpen(gtx layout.Context, call string) {
	fn := ui.findFunc(call)
	if fn == nil {
		return
	}

	ui.openFuncTabNext(fn)
	gtx.Execute(op.InvalidateCmd{})
}

func (ui *FileUI) openInNew(gtx layout.Context) {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return
	}

	state := *code
	codeData := state.Code
	colors := ApplyTheme(ui.Theme, ui.Dark.Value)
	style := CodeUIStyle{
		Theme:  ui.Theme,
		CodeUI: &state,

		CommentFor: func(inst disasm.Inst) string {
			return ui.Comments.ForAsm(codeData.Name, CommentViewGoAsm, inst.PC)
		},
		NativeCommentFor: func(inst disasm.Inst) string {
			return ui.Comments.ForAsm(codeData.Name, CommentViewNativeAsm, inst.PC)
		},
		SourceCommentFor: func(file string, line int) string {
			return ui.Comments.ForSource(codeData.Name, file, line)
		},
		Colors:     colors,
		Syntax:     SyntaxPaletteFor(ui.Settings.SyntaxStyle, colors),
		ShowNative: ui.ShowNativeAsm.Value,
		TextHeight: ui.Theme.TextSize,
	}

	size := gtx.Constraints.Max
	size.X = int(float32(size.X) / gtx.Metric.PxPerDp)
	size.Y = int(float32(size.Y) / gtx.Metric.PxPerDp)
	ui.Windows.Open(code.Name, size, WidgetWindow(style.Layout))
}
