package main

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/mcp"
	"loov.dev/lensm/internal/syntax"
)

// settingsDialog is the state of the settings dialog, its widgets and
// the settings to put back on Cancel.
type settingsDialog struct {
	open     bool
	snapshot AppSettings

	Nav                                      gui.NavList
	Theme, Syntax, CallGraph, Flame, FlameUp gui.Select
	// SyncScroll is the same switch as the status bar's, a widget of
	// its own since both can be on screen.
	SyncScroll widget.Bool
	TextSize   widget.Editor
	// textSizeEdited marks TextSize as changed since it was applied.
	textSizeEdited    bool
	StartMCP, StopMCP widget.Clickable
	OK, Cancel        widget.Clickable
}

// settingsPages are the categories of the settings dialog.
var settingsPages = []string{"Appearance", "Code view", "MCP server"}

// openSettings shows the settings dialog. Changes apply as they are
// made; Cancel puts back what was there when it opened.
func (ui *FileUI) openSettings() {
	if ui.SettingsDialog.open {
		return
	}
	ui.SettingsDialog.open = true
	ui.SettingsDialog.snapshot = ui.Settings
	ui.SettingsDialog.TextSize.SetText(strconv.Itoa(ui.Settings.TextSize))
}

func (ui *FileUI) closeSettings(keep bool) {
	if !keep {
		ui.applySettings(ui.SettingsDialog.snapshot)
	}
	ui.SettingsDialog.open = false
}

// applySettings shows the visual settings in s and saves them.
func (ui *FileUI) applySettings(s AppSettings) {
	s.SyntaxStyle = syntax.NormalizeStyle(s.SyntaxStyle)
	ui.Theme.SetDark(s.Dark)
	ui.Theme.TextSize = unit.Sp(s.TextSize)
	ui.ShowGoAsm.Value = s.ShowGoAsm
	ui.ShowNativeAsm.Value = s.ShowNativeAsm
	ui.ShowSource.Value = s.ShowSource
	ui.ShowDiagnostics.Value = !s.HideDiagnostics
	ui.ShowAsmHelp.Value = s.ShowAsmHelp
	ui.HoverLinksOnly.Value = s.HoverLinksOnly
	ui.SyncScroll.Value = s.SyncScroll
	ui.SettingsDialog.SyncScroll.Value = s.SyncScroll
	ui.saveSettings(s)
	ui.invalidateMain()
}

func (ui *FileUI) layoutSettings(gtx layout.Context) layout.Dimensions {
	ui.handleSettingsActions(gtx)
	th := ui.Theme
	return gui.Dialog{
		Theme: th, Width: 720, Height: 460,
		Nav: func(gtx layout.Context) layout.Dimensions {
			return ui.SettingsDialog.Nav.Layout(gtx, th, settingsPages)
		},
		Page: func(gtx layout.Context) layout.Dimensions {
			var rows []layout.Widget
			switch ui.SettingsDialog.Nav.Selected {
			case 0:
				rows = ui.appearancePage()
			case 1:
				rows = ui.codeViewPage()
			default:
				rows = ui.mcpPage()
			}
			children := []layout.FlexChild{layout.Rigid(th.Title(settingsPages[ui.SettingsDialog.Nav.Selected]))}
			for _, row := range rows {
				children = append(children, layout.Rigid(row))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		},
		Footer: func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(gui.Button{Theme: th, Clickable: &ui.SettingsDialog.Cancel, Text: "Cancel"}.Layout),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(gui.Button{Theme: th, Clickable: &ui.SettingsDialog.OK, Text: "OK", Primary: true}.Layout),
			)
		},
	}.Layout(gtx, &ui.SettingsDialog.open)
}

func (ui *FileUI) appearancePage() []layout.Widget {
	th := ui.Theme
	theme := 0
	if ui.Settings.Dark {
		theme = 1
	}
	syntaxLabels := make([]string, len(syntax.Styles))
	for i, style := range syntax.Styles {
		syntaxLabels[i] = syntax.StyleLabel(style)
	}
	places := []string{"Below the code", "Right of the code"}
	index := func(b bool, yes, no int) int {
		if b {
			return yes
		}
		return no
	}
	return []layout.Widget{
		gui.FormRow(th, "Theme", func(gtx layout.Context) layout.Dimensions {
			return ui.SettingsDialog.Theme.Layout(gtx, th, []string{"Light", "Dark"}, theme)
		}),
		gui.FormRow(th, "Syntax colors", func(gtx layout.Context) layout.Dimensions {
			return ui.SettingsDialog.Syntax.Layout(gtx, th, syntaxLabels, slices.Index(syntax.Styles, ui.Settings.SyntaxStyle))
		}),
		gui.FormRow(th, "Text size", func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Metric.Dp(56)
					gtx.Constraints.Max.X = gtx.Constraints.Min.X
					return gui.Field(gtx, th, gtx.Focused(&ui.SettingsDialog.TextSize), gui.Editor(th, &ui.SettingsDialog.TextSize, ""))
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: 8}.Layout(gtx, th.Muted("pt", 0.95).Layout)
				}),
			)
		}),
		gui.FormHeading(th, "Profile"),
		gui.FormRow(th, "Call graph", func(gtx layout.Context) layout.Dimensions {
			return ui.SettingsDialog.CallGraph.Layout(gtx, th, places, index(ui.Settings.CallGraphBelow, 0, 1))
		}),
		gui.FormRow(th, "Flame graph", func(gtx layout.Context) layout.Dimensions {
			return ui.SettingsDialog.Flame.Layout(gtx, th, places, index(ui.Settings.FlameRight, 1, 0))
		}),
		gui.FormRow(th, "Flame graph root", func(gtx layout.Context) layout.Dimensions {
			return ui.SettingsDialog.FlameUp.Layout(gtx, th, []string{"At the top", "At the bottom"}, index(ui.Settings.FlameUp, 1, 0))
		}),
	}
}

func (ui *FileUI) codeViewPage() []layout.Widget {
	th := ui.Theme
	return []layout.Widget{
		gui.Checkbox{Theme: th, Value: &ui.ShowAsmHelp, Label: "Show instruction help on hover"}.Layout,
		gui.Checkbox{Theme: th, Value: &ui.HoverLinksOnly, Label: "Link only the hovered line"}.Layout,
		gui.FormHint(th, "Draws the band between source and assembly for the line under the pointer only."),
		gui.Checkbox{Theme: th, Value: &ui.SettingsDialog.SyncScroll, Label: "Sync scroll"}.Layout,
		gui.FormHint(th, "Scrolls the assembly and the source together."),
		gui.FormHeading(th, "Columns"),
		gui.FormHint(th, "Pick the Go asm, Native and Source columns from the toolbar."),
	}
}

func (ui *FileUI) mcpPage() []layout.Widget {
	th := ui.Theme
	return []layout.Widget{
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Bottom: 12}.Layout(gtx, th.Muted("Lets agents read and comment on the open binary over HTTP.", 0.95).Layout)
		},
		func(gtx layout.Context) layout.Dimensions {
			dot, status := th.Colors.MutedText, "Not running"
			if ui.MCP != nil {
				dot, status = th.Colors.Positive, "Running at "+ui.MCP.URL()
			}
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(gui.Dot(dot)),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: 8, Right: 12}.Layout(gtx, th.Muted(status, 0.95).Layout)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if ui.MCP != nil {
						return gui.Button{Theme: th, Clickable: &ui.SettingsDialog.StopMCP, Text: "Stop server"}.Layout(gtx)
					}
					return gui.Button{Theme: th, Clickable: &ui.SettingsDialog.StartMCP, Text: "Start server"}.Layout(gtx)
				}),
			)
		},
	}
}

func (ui *FileUI) handleSettingsActions(gtx layout.Context) {
	s := ui.Settings
	changed := false
	if i, ok := ui.SettingsDialog.Theme.Update(gtx); ok {
		s.Dark, changed = i == 1, true
	}
	if i, ok := ui.SettingsDialog.Syntax.Update(gtx); ok {
		s.SyntaxStyle, changed = syntax.Styles[i], true
	}
	if i, ok := ui.SettingsDialog.Flame.Update(gtx); ok {
		s.FlameRight, changed = i == 1, true
	}
	if i, ok := ui.SettingsDialog.CallGraph.Update(gtx); ok {
		s.CallGraphBelow, changed = i == 0, true
	}
	if i, ok := ui.SettingsDialog.FlameUp.Update(gtx); ok {
		s.FlameUp, changed = i == 1, true
	}
	for ui.ShowAsmHelp.Update(gtx) {
		s.ShowAsmHelp, changed = ui.ShowAsmHelp.Value, true
	}
	for ui.HoverLinksOnly.Update(gtx) {
		s.HoverLinksOnly, changed = ui.HoverLinksOnly.Value, true
	}
	for ui.SettingsDialog.SyncScroll.Update(gtx) {
		s.SyncScroll, changed = ui.SettingsDialog.SyncScroll.Value, true
	}
	if value, ok := ui.updateTextSize(gtx); ok {
		s.TextSize, changed = value, true
	}
	if changed {
		ui.applySettings(s)
		gtx.Execute(op.InvalidateCmd{})
	}

	for ui.SettingsDialog.StartMCP.Clicked(gtx) {
		ui.startMCP()
	}
	for ui.SettingsDialog.StopMCP.Clicked(gtx) {
		ui.stopMCP()
	}
	for ui.SettingsDialog.OK.Clicked(gtx) {
		ui.closeSettings(true)
	}
	for ui.SettingsDialog.Cancel.Clicked(gtx) {
		ui.closeSettings(false)
	}
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		if ev, ok := ev.(key.Event); ok && ev.State == key.Press && !ui.SettingsDialog.Theme.Menu.Open && !ui.SettingsDialog.Syntax.Menu.Open && !ui.SettingsDialog.Flame.Menu.Open && !ui.SettingsDialog.CallGraph.Menu.Open && !ui.SettingsDialog.FlameUp.Menu.Open {
			ui.closeSettings(false)
		}
	}
}

// Text sizes outside these make the window unusable.
const minTextSize, maxTextSize = 6, 48

// updateTextSize returns a new text size once the field is submitted
// with Enter or loses focus. Applying each keystroke would set size 1
// on the way to typing 16, and save it.
func (ui *FileUI) updateTextSize(gtx layout.Context) (int, bool) {
	editor := &ui.SettingsDialog.TextSize
	submitted := false
	for {
		ev, ok := editor.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent:
			ui.SettingsDialog.textSizeEdited = true
		case widget.SubmitEvent:
			submitted = true
		}
	}
	if !submitted && !(ui.SettingsDialog.textSizeEdited && !gtx.Focused(editor)) {
		return 0, false
	}
	ui.SettingsDialog.textSizeEdited = false
	value, err := strconv.Atoi(strings.TrimSpace(editor.Text()))
	if err != nil {
		editor.SetText(strconv.Itoa(int(ui.Theme.TextSize)))
		return 0, false
	}
	value = min(max(value, minTextSize), maxTextSize)
	editor.SetText(strconv.Itoa(value))
	return value, true
}

// saveSetting saves the settings as change leaves them.
func (ui *FileUI) saveSetting(change func(*AppSettings)) {
	s := ui.Settings
	change(&s)
	ui.saveSettings(s)
}

// applySetting shows and saves the settings as change leaves them.
func (ui *FileUI) applySetting(change func(*AppSettings)) {
	s := ui.Settings
	change(&s)
	ui.applySettings(s)
}

func (ui *FileUI) saveSettings(settings AppSettings) {
	ui.Settings = settings
	ui.sessionDirty = true
	ui.scheduleFlush()
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
