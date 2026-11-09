package main

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/unit"

	"gioui.org/font"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"loov.dev/lensm/internal/codeview"
	"loov.dev/lensm/internal/gui"
)

// Panels of the sidebar, picked in the icon strip.
const (
	panelFunctions = iota
	panelComments
	panelHistory
	panelMarks
	panelCallers
	panelSummary
	panelGraph
	panelCount
)

// quit closes the window, which saves the settings and comments on the
// way out.
func (ui *FileUI) quit() {
	if ui.window != nil {
		ui.window.Perform(system.ActionClose)
	}
}

// showSidebar shows or hides the panel left of the code.
func (ui *FileUI) showSidebar(show bool) {
	if ui.Settings.HideSidebar == !show {
		return
	}
	ui.saveSetting(func(s *AppSettings) { s.HideSidebar = !show })
}

// focusFilter puts the caret at the end of the function filter, showing
// the function list first.
func (ui *FileUI) focusFilter(gtx layout.Context) {
	ui.Panel = panelFunctions
	ui.showSidebar(true)
	gtx.Execute(key.FocusCmd{Tag: &ui.Funcs.Filter})
	ui.Funcs.Filter.SetCaret(ui.Funcs.Filter.Len(), 0)
}

func (ui *FileUI) reload() {
	if ui.Config.Path != "" {
		ui.LoadError = nil
		ui.requestLoad(ui.Config.Path)
	}
}

func (ui *FileUI) setWatch(on bool) {
	ui.Config.Watch = on
	if ui.loader != nil {
		ui.loader.SetWatch(on)
	}
}

// The toolbar's height, and with a custom title bar where the window
// buttons start and the room they take, in dp.
const (
	toolbarHeight      = 44
	windowButtonsLeft  = 14
	windowButtonsWidth = 78
)

func (ui *FileUI) layoutToolbar(gtx layout.Context) layout.Dimensions {
	th := ui.Theme
	c := th.Colors
	size := image.Pt(gtx.Constraints.Max.X, gtx.Metric.Dp(toolbarHeight))
	gtx.Constraints = layout.Exact(size)
	paint.FillShape(gtx.Ops, c.SecondaryBackground, clip.Rect{Max: size}.Op())
	paint.FillShape(gtx.Ops, c.Splitter, clip.Rect{Min: image.Pt(0, size.Y-gtx.Metric.Dp(1)), Max: size}.Op())

	left := unit.Dp(8)
	if customTitleBar {
		// Empty toolbar space moves the window. The widgets on top
		// keep their clicks, since a hit stops at the topmost area.
		area := clip.Rect{Max: size}.Push(gtx.Ops)
		system.ActionInputOp(system.ActionMove).Add(gtx.Ops)
		event.Op(gtx.Ops, &ui.titleDrag)
		area.Pop()
		for {
			ev, ok := gtx.Event(pointer.Filter{Target: &ui.titleDrag, Kinds: pointer.Enter | pointer.Leave | pointer.Cancel})
			if !ok {
				break
			}
			if e, ok := ev.(pointer.Event); ok {
				overTitleDrag.Store(e.Kind == pointer.Enter)
			}
		}
		if !ui.fullscreen {
			left = windowButtonsWidth
		}
	}

	gui.Start(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: left, Right: 12}.Layout(gtx, ui.layoutToolbarRow)
	})
	return layout.Dimensions{Size: size}
}

func (ui *FileUI) layoutToolbarRow(gtx layout.Context) layout.Dimensions {
	// A wasm module has WAT and the statements it adds up to instead.
	asmLabel, nativeLabel := "Go asm", "Native"
	if code := ui.activeCode(); code != nil && code.Code != nil && code.Code.Statements {
		asmLabel, nativeLabel = "WAT", "Statements"
	}
	th := ui.Theme
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	// The two sides share what the search leaves, so it stays centred.
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					// The menu bar has these items on macOS.
					if nativeMenu {
						return layout.Dimensions{}
					}
					return ui.AppMenu.Layout(gtx, th, ui.appMenuItems(), ui.layoutBrand)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					// Fill the space, so the arrows sit by the search.
					dims := layout.Inset{Left: 10, Right: 12}.Layout(gtx, ui.layoutPath)
					dims.Size.X = gtx.Constraints.Max.X
					return dims
				}),
				layout.Rigid(gui.IconButton{Theme: th, Clickable: &ui.BackButton, Tip: &ui.BackTip, Icon: BackIcon,
					Description: "Back", Disabled: !ui.Navigation.CanBack()}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: 2, Right: 8}.Layout(gtx, gui.IconButton{Theme: th, Clickable: &ui.ForwardButton, Tip: &ui.ForwardTip, Icon: ForwardIcon,
						Description: "Forward", Disabled: !ui.Navigation.CanForward()}.Layout)
				}),
			)
		}),
		layout.Rigid(gui.SearchButton{
			Theme:       th,
			Clickable:   &ui.GoToButton,
			Placeholder: "Go to function…",
			Shortcut:    shortcut("P"),
		}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.E.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(ui.layoutCopyStatus),
					layout.Rigid(gui.Segmented{Theme: th, Segments: []gui.Segment{
						{Label: asmLabel, Value: &ui.ShowGoAsm},
						{Label: nativeLabel, Value: &ui.ShowNativeAsm},
						{Label: "Source", Value: &ui.ShowSource},
						{Label: "Diagnostics", Value: &ui.ShowDiagnostics},
					}}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						button := gui.IconButton{Theme: th, Clickable: &ui.SettingsButton, Tip: &ui.SettingsTip, Icon: SettingsIcon, Description: "Settings"}
						return layout.Inset{Left: 8}.Layout(gtx, button.Layout)
					}),
				)
			})
		}),
	)
}

// layoutBrand is the anchor of the app menu: the name and a chevron.
func (ui *FileUI) layoutBrand(gtx layout.Context, open bool) layout.Dimensions {
	th := ui.Theme
	c := th.Colors
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			if open || ui.AppMenu.Hovered() {
				rect := image.Rectangle{Max: gtx.Constraints.Min}
				paint.FillShape(gtx.Ops, c.Fill, clip.UniformRRect(rect, gtx.Metric.Dp(6)).Op(gtx.Ops))
			}
			return layout.Dimensions{Size: gtx.Constraints.Min}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 4, Bottom: 4, Left: 8, Right: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						label := th.Label("lensm", 1.08)
						label.Font.Weight = font.Bold
						return label.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						s := gtx.Metric.Dp(14)
						gtx.Constraints = layout.Exact(image.Pt(s, s))
						return layout.Inset{Left: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return gui.ExpandMoreIcon.Layout(gtx, c.MutedText)
						})
					}),
				)
			})
		},
	)
}

// layoutPath shows the open binary with its directory dimmed.
func (ui *FileUI) layoutPath(gtx layout.Context) layout.Dimensions {
	if ui.Config.Path == "" {
		return layout.Dimensions{}
	}
	dir, name := filepath.Split(homeRelative(ui.Config.Path))
	return dimmedPrefix(gtx, ui.Theme, dir, name, 0.92)
}

// dimmedPrefix draws prefix muted and name in the text colour on one
// line. The prefix is cut short first when there is no room.
func dimmedPrefix(gtx layout.Context, th *gui.Theme, prefix, name string, scale float32) layout.Dimensions {
	gtx.Constraints.Min = image.Point{}
	nameLabel := th.Label(name, scale)
	nameLabel.Font.Weight = font.SemiBold
	nameLabel.MaxLines = 1
	macro := op.Record(gtx.Ops)
	nameDims := nameLabel.Layout(gtx)
	nameOps := macro.Stop()

	prefixLabel := th.Muted(prefix, scale)
	prefixLabel.MaxLines = 1
	pgtx := gtx
	pgtx.Constraints.Max.X = max(gtx.Constraints.Max.X-nameDims.Size.X, 0)
	prefixDims := prefixLabel.Layout(pgtx)

	defer op.Offset(image.Pt(prefixDims.Size.X, prefixDims.Baseline-nameDims.Baseline)).Push(gtx.Ops).Pop()
	nameOps.Add(gtx.Ops)
	return layout.Dimensions{
		Size:     image.Pt(prefixDims.Size.X+nameDims.Size.X, max(prefixDims.Size.Y, nameDims.Size.Y)),
		Baseline: prefixDims.Baseline,
	}
}

func homeRelative(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rel, ok := strings.CutPrefix(path, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rel
	}
	return path
}

func (ui *FileUI) layoutCopyStatus(gtx layout.Context) layout.Dimensions {
	text, isError := ui.transientStatus(gtx)
	if text == "" {
		return layout.Dimensions{}
	}
	label := ui.Theme.Muted(text, 0.88)
	if isError {
		label = ui.Theme.ErrorLabel(text, 0.88)
	}
	label.MaxLines = 1
	return layout.Inset{Right: 10}.Layout(gtx, label.Layout)
}

// transientStatus is the copy confirmation, which fades after a moment,
// or a save error, which stays until the next successful save.
func (ui *FileUI) transientStatus(gtx layout.Context) (string, bool) {
	if ui.saveError != "" {
		return ui.saveError, true
	}
	const copyStatusFor = 2 * time.Second
	if ui.copyStatus != "" && !ui.copyStatusAt.IsZero() {
		if gtx.Now.Sub(ui.copyStatusAt) >= copyStatusFor {
			ui.copyStatus = ""
		} else {
			gtx.Execute(op.InvalidateCmd{At: ui.copyStatusAt.Add(copyStatusFor)})
		}
	}
	return ui.copyStatus, false
}

// layoutRightStrip holds the buttons for the pane right of the code:
// the profile's call graph and its flame graph. The button of the one
// shown hides the pane.
func (ui *FileUI) layoutRightStrip(gtx layout.Context) layout.Dimensions {
	th := ui.Theme
	t := &ui.GraphTools
	toggle := func(flame bool) {
		if ui.Profile == nil {
			// The pane needs a profile; ask for one and show it after.
			ui.ShowGraph, t.Flame = true, flame
			ui.chooseProfile()
			return
		}
		if ui.ShowGraph && t.Flame == flame {
			ui.ShowGraph = false
			return
		}
		ui.ShowGraph, t.Flame = true, flame
	}
	for ui.GraphButton.Clicked(gtx) {
		toggle(false)
	}
	for ui.FlameButton.Clicked(gtx) {
		toggle(true)
	}
	shown := ui.ShowGraph && ui.Profile != nil
	tip := func(name string, on bool) string {
		switch {
		case ui.Profile == nil:
			return "Open a profile to see its " + name
		case on:
			return "Hide the " + name
		}
		return "Show the " + name
	}
	return gui.IconStrip{
		Theme: th,
		Right: true,
		Top: []layout.Widget{
			gui.ToolButton{Theme: th, Clickable: &ui.GraphButton, Tip: &ui.GraphTip, Icon: ProfileGraphIcon, Description: tip("call graph", shown && !t.Flame), Selected: shown && !t.Flame}.Layout,
			gui.ToolButton{Theme: th, Clickable: &ui.FlameButton, Tip: &ui.FlameTip, Icon: FlameGraphIcon, Description: tip("flame graph", shown && t.Flame), Selected: shown && t.Flame}.Layout,
		},
	}.Layout(gtx)
}

func (ui *FileUI) layoutStrip(gtx layout.Context) layout.Dimensions {
	th := ui.Theme
	for i := range ui.PanelButtons {
		for ui.PanelButtons[i].Clicked(gtx) {
			// The open panel's button hides the sidebar, for room. With
			// the graph filling the window, any shows the panel again.
			ui.showSidebar(ui.Panel != i || ui.Settings.HideSidebar || ui.GraphFull)
			ui.Panel, ui.GraphFull = i, false
		}
	}
	for ui.WatchButton.Clicked(gtx) {
		ui.setWatch(!ui.Config.Watch)
	}
	tool := func(i int, icon *widget.Icon, description string) layout.Widget {
		return gui.ToolButton{Theme: th, Clickable: &ui.PanelButtons[i], Tip: &ui.PanelTips[i], Icon: icon, Description: description, Selected: ui.Panel == i && !ui.Settings.HideSidebar && !(ui.GraphFull && ui.ShowGraph)}.Layout
	}
	watch := "Watch for changes"
	if ui.Config.Watch {
		watch = "Stop watching for changes"
	}
	return gui.IconStrip{
		Theme: th,
		Top: []layout.Widget{
			tool(panelFunctions, FunctionsIcon, "Functions"),
			tool(panelComments, CommentsIcon, "Comments"),
			tool(panelHistory, HistoryIcon, "History"),
			tool(panelCallers, CallersIcon, "Callers"),
			tool(panelMarks, MarksIcon, "Diagnostics"),
			tool(panelSummary, SummaryIcon, "Summary"),
			tool(panelGraph, GraphIcon, "Call graph"),
		},
		Bottom: []layout.Widget{
			gui.ToolButton{Theme: th, Clickable: &ui.WatchButton, Tip: &ui.WatchTip, Icon: WatchIcon, Description: watch, Selected: ui.Config.Watch}.Layout,
		},
	}.Layout(gtx)
}

func (ui *FileUI) layoutPanel(gtx layout.Context) layout.Dimensions {
	gtx.Constraints = layout.Exact(gtx.Constraints.Max)
	switch ui.Panel {
	case panelComments:
		return ui.layoutCommentsPanel(gtx)
	case panelHistory:
		return ui.layoutHistoryPanel(gtx)
	case panelMarks:
		return ui.layoutMarksPanel(gtx)
	case panelGraph:
		return ui.layoutGraphPanel(gtx)
	case panelSummary:
		return ui.layoutSummaryPanel(gtx)
	case panelCallers:
		return ui.layoutCallersPanel(gtx)
	}
	return ui.Funcs.Layout(ui.Theme, gtx)
}

// layoutCrumbs shows where the active function lives: its source file,
// dimmed directory first, and its name.
func (ui *FileUI) layoutCrumbs(gtx layout.Context) layout.Dimensions {
	tab := ui.activeTab()
	if tab == nil || tab.Code.Code == nil {
		return layout.Dimensions{}
	}
	th := ui.Theme
	_, name := gui.SplitPackage(tab.Name)
	file := tab.Code.Code.File
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 6, Bottom: 6, Left: 14, Right: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				if file == "" {
					label := th.Label(name, 0.9)
					label.Font.Weight = font.SemiBold
					label.MaxLines = 1
					return label.Layout(gtx)
				}
				dir, base := filepath.Split(homeRelative(file))
				return dimmedPrefix(gtx, th, dir, base+"  ›  "+name, 0.9)
			})
		}),
		layout.Rigid(gui.HorizontalLine{Height: 1, Color: th.Colors.Divider}.Layout),
	)
}

// selectedPosition is the source file and line of the selection.
func selectedPosition(code *codeview.UI) string {
	switch {
	case code.SelectedView == codeview.ViewSource && code.SelectedFile != "":
		return fmt.Sprintf("%s:%d", filepath.Base(code.SelectedFile), code.SelectedLine)
	case code.SelectedAsm >= 0 && code.SelectedAsm < len(code.Code.Insts):
		ix := code.Code.Insts[code.SelectedAsm]
		if ix.File != "" {
			return fmt.Sprintf("%s:%d", filepath.Base(ix.File), ix.Line)
		}
	}
	return ""
}

func (ui *FileUI) layoutStatus(gtx layout.Context) layout.Dimensions {
	th := ui.Theme
	var left, right []layout.Widget
	if code := ui.activeCode(); code != nil && code.Code != nil {
		jumps := 0
		for _, ix := range code.Code.Insts {
			if ix.RefOffset != 0 {
				jumps++
			}
		}
		if code.Code.Arch != "" {
			left = append(left, th.StatusText(code.Code.Arch))
		}
		left = append(left, th.StatusText(plural(len(code.Code.Insts), "instruction")+" · "+plural(jumps, "jump")))
		if pos := selectedPosition(code); pos != "" {
			right = append(right, th.StatusText(pos))
		}
	}
	if ui.File != nil {
		left = append(left, th.StatusText(ui.Funcs.CountText()))
	}
	if ui.Profile != nil {
		left = append(left, ui.layoutProfileStatus)
	}
	if ui.SyncScroll.Update(gtx) {
		ui.applySetting(func(s *AppSettings) { s.SyncScroll = ui.SyncScroll.Value })
	}
	right = append(right, gui.StatusToggle{Theme: th, Value: &ui.SyncScroll, Label: "Sync scroll"}.Layout)
	dot, mcp := th.Colors.MutedText, "MCP off"
	if ui.MCP != nil {
		dot, mcp = th.Colors.Positive, "MCP "+ui.MCP.URL()
	}
	dot.A = 0xa0
	right = append(right, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(gui.Dot(dot)),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: 6}.Layout(gtx, th.StatusText(mcp))
			}),
		)
	})
	return gui.StatusBar{Theme: th, Left: left, Right: right}.Layout(gtx)
}

// plural spells n of thing: 1 jump, 2 jumps.
func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}
