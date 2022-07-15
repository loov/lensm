package main

import (
	"fmt"
	"image"
	"os"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type ExeUIConfig struct {
	Exe     string
	Context int
}

type ExeUI struct {
	Windows *Windows
	Theme   *material.Theme

	Config ExeUIConfig

	// Currently loaded executable.
	Exe     *Exe
	Symbols *SymbolSelectionList

	// Active code view.
	Cache map[*Symbol]*Code
	Code  CodeUI

	// Other ExeUI elements.
	OpenInNew widget.Clickable
}

func NewExeUI(windows *Windows, theme *material.Theme) *ExeUI {
	ui := &ExeUI{}
	ui.Windows = windows
	ui.Theme = theme
	ui.Symbols = NewSymbolList(theme)
	ui.Cache = make(map[*Symbol]*Code)
	return ui
}

func (ui *ExeUI) Run(w *app.Window) error {
	var ops op.Ops
	for {
		select {
		case e := <-w.Events():
			switch e := e.(type) {
			case system.FrameEvent:
				gtx := layout.NewContext(&ops, e)
				ui.Layout(gtx)
				e.Frame(gtx.Ops)

			case system.DestroyEvent:
				return e.Err
			}
		}
	}
}

func (ui *ExeUI) SetExe(exe *Exe) {
	ui.Exe = exe
	ui.Symbols.SetSymbols(exe.Symbols)
}

func (ui *ExeUI) Layout(gtx layout.Context) {
	for ui.OpenInNew.Clicked() {
		ui.openInNew(gtx)
	}

	if ui.Symbols.Selected == "" {
		ui.Symbols.SelectIndex(0)
	}

	if !ui.Code.Loaded() || ui.Code.Name != ui.Symbols.Selected {
		selected := ui.Symbols.SelectedSymbol
		if selected != nil {
			code, ok := ui.Cache[selected]
			if !ok {
				var err error
				code, err = Disassemble(selected.Exe.Disasm, selected, Options{Context: ui.Config.Context})
				ui.Cache[selected] = code
				if err != nil {
					fmt.Fprintln(os.Stderr, code)
				}
			}
			ui.Code.Code = code
		}
	}

	layout.Flex{
		Axis: layout.Horizontal,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(image.Point{
				X: gtx.Metric.Sp(10 * 20),
				Y: gtx.Constraints.Max.Y,
			})
			return ui.Symbols.Layout(ui.Theme, gtx)
		}),
		layout.Rigid(VerticalLine{Width: 1, Color: splitterColor}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(gtx.Constraints.Max)
			return layout.Stack{
				Alignment: layout.SE,
			}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					return CodeUIStyle{
						CodeUI: &ui.Code,

						Theme:      ui.Theme,
						TextHeight: ui.Theme.TextSize,
						LineHeight: ui.Theme.TextSize * 1.2,
					}.Layout(gtx)
				}),
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					button := material.IconButton(ui.Theme, &ui.OpenInNew, OpenInNewIcon, "Open in separate window")
					button.Size = 16
					button.Inset = layout.UniformInset(12)
					return layout.UniformInset(2).Layout(gtx, button.Layout)
				}),
			)
		}),
	)
}

func (ui *ExeUI) openInNew(gtx layout.Context) {
	state := ui.Code
	style := CodeUIStyle{
		Theme:  ui.Theme,
		CodeUI: &state,

		TextHeight: ui.Theme.TextSize,
		LineHeight: ui.Theme.TextSize * 14 / 12,
	}

	size := gtx.Constraints.Max
	size.X = int(float32(size.X) / gtx.Metric.PxPerDp)
	size.Y = int(float32(size.Y) / gtx.Metric.PxPerDp)
	ui.Windows.Open(ui.Code.Name, size, WidgetWindow(style.Layout))
}
