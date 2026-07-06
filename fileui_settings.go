package main

import (
	"image"
	"strconv"
	"strings"

	"gioui.org/app"
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
	colors := gui.ApplyTheme(ui.Theme, ui.Dark.Value)
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
						check := material.CheckBox(ui.Theme, &ui.ShowAsmHelp, "Show instruction help")
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

func (ui *FileUI) layoutSettingsSection(gtx layout.Context, colors gui.UIColors, title string, children []layout.FlexChild) layout.Dimensions {
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

func (ui *FileUI) layoutLabeledEditor(gtx layout.Context, colors gui.UIColors, labelText string, editor *widget.Editor) layout.Dimensions {
	return layout.Inset{Top: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme, labelText)
				label.Color = colors.Text
				return layout.Inset{Right: 8}.Layout(gtx, label.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Pt(gtx.Metric.Dp(80), gtx.Metric.Dp(34)))
				return gui.FocusBorder(ui.Theme, gtx.Focused(editor)).Layout(gtx,
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
	for ui.ShowAsmHelp.Update(gtx) {
		changedVisual = true
	}
	for ui.SyntaxStyle.Update(gtx) {
		ui.SyntaxStyle.Value = syntax.NormalizeStyle(ui.SyntaxStyle.Value)
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
	settings.ShowAsmHelp = ui.ShowAsmHelp.Value
	settings.SyntaxStyle = syntax.NormalizeStyle(ui.SyntaxStyle.Value)
	if value, err := strconv.Atoi(strings.TrimSpace(ui.TextSizeEditor.Text())); err == nil && value > 0 {
		settings.TextSize = value
	}
	ui.saveSettings(settings)
}

func (ui *FileUI) saveSettings(settings AppSettings) {
	ui.Settings = settings
	ui.sessionDirty = true
	ui.scheduleFlush()
}
