package main

import (
	"slices"
	"strconv"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"

	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/syntax"
)

// settingsPages are the categories of the settings dialog.
var settingsPages = []string{"Appearance", "Code view", "MCP server"}

// openSettings shows the settings dialog. Changes apply as they are
// made; Cancel puts back what was there when it opened.
func (ui *FileUI) openSettings() {
	if ui.settingsOpen {
		return
	}
	ui.settingsOpen = true
	ui.settingsSnapshot = ui.Settings
	ui.TextSizeEditor.SetText(strconv.Itoa(ui.Settings.TextSize))
}

func (ui *FileUI) closeSettings(keep bool) {
	if !keep {
		ui.applySettings(ui.settingsSnapshot)
	}
	ui.settingsOpen = false
}

// applySettings shows the visual settings in s and saves them.
func (ui *FileUI) applySettings(s AppSettings) {
	s.SyntaxStyle = syntax.NormalizeStyle(s.SyntaxStyle)
	ui.Theme.SetDark(s.Dark)
	ui.Theme.TextSize = unit.Sp(s.TextSize)
	ui.ShowGoAsm.Value = s.ShowGoAsm
	ui.ShowNativeAsm.Value = s.ShowNativeAsm
	ui.ShowSource.Value = s.ShowSource
	ui.ShowAsmHelp.Value = s.ShowAsmHelp
	ui.HoverLinksOnly.Value = s.HoverLinksOnly
	ui.saveSettings(s)
	ui.invalidateMain()
}

func (ui *FileUI) layoutSettings(gtx layout.Context) layout.Dimensions {
	ui.handleSettingsActions(gtx)
	th := ui.Theme
	return gui.Dialog{
		Theme: th, Width: 720, Height: 460,
		Nav: func(gtx layout.Context) layout.Dimensions {
			return ui.SettingsNav.Layout(gtx, th, settingsPages)
		},
		Page: func(gtx layout.Context) layout.Dimensions {
			var rows []layout.Widget
			switch ui.SettingsNav.Selected {
			case 0:
				rows = ui.appearancePage()
			case 1:
				rows = ui.codeViewPage()
			default:
				rows = ui.mcpPage()
			}
			children := []layout.FlexChild{layout.Rigid(th.Title(settingsPages[ui.SettingsNav.Selected]))}
			for _, row := range rows {
				children = append(children, layout.Rigid(row))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		},
		Footer: func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(gui.Button{Theme: th, Clickable: &ui.SettingsCancel, Text: "Cancel"}.Layout),
				layout.Rigid(layout.Spacer{Width: 8}.Layout),
				layout.Rigid(gui.Button{Theme: th, Clickable: &ui.SettingsOK, Text: "OK", Primary: true}.Layout),
			)
		},
	}.Layout(gtx, &ui.settingsOpen)
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
	return []layout.Widget{
		gui.FormRow(th, "Theme", func(gtx layout.Context) layout.Dimensions {
			return ui.ThemeSelect.Layout(gtx, th, []string{"Light", "Dark"}, theme)
		}),
		gui.FormRow(th, "Syntax colors", func(gtx layout.Context) layout.Dimensions {
			return ui.SyntaxSelect.Layout(gtx, th, syntaxLabels, slices.Index(syntax.Styles, ui.Settings.SyntaxStyle))
		}),
		gui.FormRow(th, "Text size", func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Metric.Dp(56)
					gtx.Constraints.Max.X = gtx.Constraints.Min.X
					return gui.Field(gtx, th, gtx.Focused(&ui.TextSizeEditor), gui.Editor(th, &ui.TextSizeEditor, ""))
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: 8}.Layout(gtx, th.Muted("pt", 0.95).Layout)
				}),
			)
		}),
	}
}

func (ui *FileUI) codeViewPage() []layout.Widget {
	th := ui.Theme
	return []layout.Widget{
		gui.Checkbox{Theme: th, Value: &ui.ShowAsmHelp, Label: "Show instruction help on hover"}.Layout,
		gui.Checkbox{Theme: th, Value: &ui.HoverLinksOnly, Label: "Link only the hovered line"}.Layout,
		gui.FormHint(th, "Draws the band between source and assembly for the line under the pointer only."),
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
						return gui.Button{Theme: th, Clickable: &ui.StopMCP, Text: "Stop server"}.Layout(gtx)
					}
					return gui.Button{Theme: th, Clickable: &ui.StartMCP, Text: "Start server"}.Layout(gtx)
				}),
			)
		},
	}
}

func (ui *FileUI) handleSettingsActions(gtx layout.Context) {
	s := ui.Settings
	changed := false
	if i, ok := ui.ThemeSelect.Update(gtx); ok {
		s.Dark, changed = i == 1, true
	}
	if i, ok := ui.SyntaxSelect.Update(gtx); ok {
		s.SyntaxStyle, changed = syntax.Styles[i], true
	}
	for ui.ShowAsmHelp.Update(gtx) {
		s.ShowAsmHelp, changed = ui.ShowAsmHelp.Value, true
	}
	for ui.HoverLinksOnly.Update(gtx) {
		s.HoverLinksOnly, changed = ui.HoverLinksOnly.Value, true
	}
	if value, ok := ui.updateTextSize(gtx); ok {
		s.TextSize, changed = value, true
	}
	if changed {
		ui.applySettings(s)
		gtx.Execute(op.InvalidateCmd{})
	}

	for ui.StartMCP.Clicked(gtx) {
		ui.startMCP()
	}
	for ui.StopMCP.Clicked(gtx) {
		ui.stopMCP()
	}
	for ui.SettingsOK.Clicked(gtx) {
		ui.closeSettings(true)
	}
	for ui.SettingsCancel.Clicked(gtx) {
		ui.closeSettings(false)
	}
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		if ev, ok := ev.(key.Event); ok && ev.State == key.Press && !ui.ThemeSelect.Menu.Open && !ui.SyntaxSelect.Menu.Open {
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
	editor := &ui.TextSizeEditor
	submitted := false
	for {
		ev, ok := editor.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent:
			ui.textSizeEdited = true
		case widget.SubmitEvent:
			submitted = true
		}
	}
	if !submitted && !(ui.textSizeEdited && !gtx.Focused(editor)) {
		return 0, false
	}
	ui.textSizeEdited = false
	value, err := strconv.Atoi(strings.TrimSpace(editor.Text()))
	if err != nil {
		editor.SetText(strconv.Itoa(int(ui.Theme.TextSize)))
		return 0, false
	}
	value = min(max(value, minTextSize), maxTextSize)
	editor.SetText(strconv.Itoa(value))
	return value, true
}

func (ui *FileUI) saveSettings(settings AppSettings) {
	ui.Settings = settings
	ui.sessionDirty = true
	ui.scheduleFlush()
}
