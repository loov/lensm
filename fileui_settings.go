package main

import (
	"image"
	"strconv"
	"strings"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/syntax"
)

func (ui *FileUI) openSettingsWindow() {
	if ui.settingsWindowOpen || ui.settingsEvents == nil {
		return
	}
	ui.settingsWindowOpen = true
	events, acks, exited := ui.settingsEvents, ui.settingsAcks, ui.exited
	ui.Windows.Open("lensm settings", image.Pt(480, 430), func(w *app.Window) error {
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
	colors := ui.Theme.Colors
	paint.FillShape(gtx.Ops, colors.Background, clip.Rect{Max: gtx.Constraints.Max}.Op())
	ui.handleSettingsActions(gtx)

	return layout.Inset{Top: 18, Bottom: 18, Left: 20, Right: 20}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(ui.settingsSection("Appearance",
				ui.settingsCheck(&ui.Dark, "Dark theme"),
				ui.settingsRow("Syntax colors", func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(ui.settingsRadio(syntax.StyleGoLand)),
						layout.Rigid(ui.settingsRadio(syntax.StyleDarcula)),
						layout.Rigid(ui.settingsRadio(syntax.StyleMono)),
					)
				}),
				ui.settingsRow("Text size", func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Metric.Dp(56)
					gtx.Constraints.Max.X = gtx.Constraints.Min.X
					return gui.Field(gtx, ui.Theme, gtx.Focused(&ui.TextSizeEditor), func(gtx layout.Context) layout.Dimensions {
						editor := material.Editor(ui.Theme.Theme, &ui.TextSizeEditor, "")
						editor.TextSize = ui.Theme.TextSize * 0.95
						return editor.Layout(gtx)
					})
				}),
			)),
			layout.Rigid(ui.settingsSection("Code view",
				ui.settingsCheck(&ui.ShowNativeAsm, "Show native assembly"),
				ui.settingsCheck(&ui.ShowAsmHelp, "Show instruction help on hover"),
				ui.settingsCheck(&ui.HoverLinksOnly, "Link only the hovered line"),
			)),
			layout.Rigid(ui.settingsSection("MCP server",
				func(gtx layout.Context) layout.Dimensions {
					status := "Not running. Agents can connect to lensm over HTTP while it runs."
					if ui.MCP != nil {
						status = "Running at " + ui.MCP.URL()
					}
					return layout.Inset{Bottom: 8}.Layout(gtx, ui.Theme.Muted(status, 0.9).Layout)
				},
				func(gtx layout.Context) layout.Dimensions {
					if ui.MCP != nil {
						return gui.Button{Theme: ui.Theme, Clickable: &ui.StopMCP, Text: "Stop server"}.Layout(gtx)
					}
					return gui.Button{Theme: ui.Theme, Clickable: &ui.StartMCP, Text: "Start server", Primary: true}.Layout(gtx)
				},
			)),
		)
	})
}

// settingsSection lays out a titled group of settings rows.
func (ui *FileUI) settingsSection(title string, rows ...layout.Widget) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := ui.Theme.Label(title, 0.95)
				label.Font.Weight = font.SemiBold
				return layout.Inset{Bottom: 6}.Layout(gtx, label.Layout)
			}),
		}
		for _, row := range rows {
			children = append(children, layout.Rigid(row))
		}
		return layout.Inset{Bottom: 20}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	}
}

// settingsRow puts a label in a fixed column so the controls line up.
func (ui *FileUI) settingsRow(label string, w layout.Widget) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 3, Bottom: 3}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Metric.Dp(110)
					return ui.Theme.Label(label, 0.95).Layout(gtx)
				}),
				layout.Rigid(w),
			)
		})
	}
}

func (ui *FileUI) settingsCheck(value *widget.Bool, label string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		check := material.CheckBox(ui.Theme.Theme, value, label)
		check.Color = ui.Theme.Colors.Text
		check.IconColor = ui.Theme.Colors.Accent
		check.TextSize = ui.Theme.TextSize * 0.95
		check.Size = 20
		// The checkbox pads its icon; pull it back to the text edge.
		return layout.Inset{Left: -4}.Layout(gtx, check.Layout)
	}
}

func (ui *FileUI) settingsRadio(style string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		radio := material.RadioButton(ui.Theme.Theme, &ui.SyntaxStyle, style, syntax.StyleLabel(style))
		radio.Color = ui.Theme.Colors.Text
		radio.IconColor = ui.Theme.Colors.Accent
		radio.TextSize = ui.Theme.TextSize * 0.95
		radio.Size = 20
		return layout.Inset{Left: -4, Right: 14}.Layout(gtx, radio.Layout)
	}
}

func (ui *FileUI) handleSettingsActions(gtx layout.Context) {
	changedVisual := false
	for ui.Dark.Update(gtx) {
		ui.Theme.SetDark(ui.Dark.Value)
		changedVisual = true
	}
	for ui.ShowNativeAsm.Update(gtx) {
		changedVisual = true
	}
	for ui.ShowAsmHelp.Update(gtx) {
		changedVisual = true
	}
	for ui.HoverLinksOnly.Update(gtx) {
		changedVisual = true
	}
	for ui.SyntaxStyle.Update(gtx) {
		ui.SyntaxStyle.Value = syntax.NormalizeStyle(ui.SyntaxStyle.Value)
		changedVisual = true
	}
	if value, ok := ui.updateTextSize(gtx); ok {
		ui.Theme.TextSize = unit.Sp(value)
		ui.Settings.TextSize = value
		changedVisual = true
	}
	if changedVisual {
		ui.saveVisualSettings()
		gtx.Execute(op.InvalidateCmd{})
		ui.invalidateMain()
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

func (ui *FileUI) saveVisualSettings() {
	settings := ui.Settings
	settings.Dark = ui.Dark.Value
	settings.ShowNativeAsm = ui.ShowNativeAsm.Value
	settings.ShowAsmHelp = ui.ShowAsmHelp.Value
	settings.HoverLinksOnly = ui.HoverLinksOnly.Value
	settings.SyntaxStyle = syntax.NormalizeStyle(ui.SyntaxStyle.Value)
	ui.saveSettings(settings)
}

func (ui *FileUI) saveSettings(settings AppSettings) {
	ui.Settings = settings
	ui.sessionDirty = true
	ui.scheduleFlush()
}
