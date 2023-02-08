package main

import (
	"image"
	"os"
	"time"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type ExeUIConfig struct {
	Exe     string
	Watch   bool
	Context int
}

type ExeUI struct {
	Windows *Windows
	Theme   *material.Theme

	Config ExeUIConfig

	LoadError error

	// Currently loaded executable.
	Exe     *Exe
	Symbols *SymbolSelectionList

	// Active code view.
	Code CodeUI

	// Other ExeUI elements.
	OpenInNew widget.Clickable
}

func NewExeUI(windows *Windows, theme *material.Theme) *ExeUI {
	ui := &ExeUI{}
	ui.Windows = windows
	ui.Theme = theme
	ui.Symbols = NewSymbolList(theme)
	return ui
}

func (ui *ExeUI) Run(w *app.Window) error {
	var ops op.Ops

	exited := make(chan struct{})
	defer close(exited)

	exeLoaded := make(chan *Exe, 1)
	exeLoadError := make(chan error, 1)

	loadFinished := func(exe *Exe, err error) {
		if err == nil {
			select {
			case <-exeLoaded:
			default:
			}
			exeLoaded <- exe
		} else {
			select {
			case <-exeLoadError:
			default:
			}
			exeLoadError <- err
		}
	}

	go func() {
		var lastModTime time.Time
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			func() {
				stat, err := os.Stat(ui.Config.Exe)
				if err != nil {
					loadFinished(nil, err)
					return
				}
				if stat.ModTime().Equal(lastModTime) {
					return
				}
				lastModTime = stat.ModTime()

				exe, err := LoadExe(ui.Config.Exe)
				loadFinished(exe, err)
			}()

			if !ui.Config.Watch {
				break
			}

			select {
			case <-tick.C:
			case <-exited:
				return
			}
		}
	}()

	for {
		select {
		case err := <-exeLoadError:
			ui.LoadError = err
			w.Invalidate()
		case exe := <-exeLoaded:
			ui.SetExe(exe)
			w.Invalidate()
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
	if ui.Exe != nil {
		_ = ui.Exe.Close()
	}
	ui.Exe = exe
	ui.Symbols.SetSymbols(exe.Symbols)
	if ui.Symbols.Selected != "" {
		for _, sym := range exe.Symbols {
			if sym.Name == ui.Symbols.Selected {
				ui.Code.Code = ui.Exe.LoadSymbol(sym, Options{Context: ui.Config.Context})
			}
		}
	}
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
			ui.Code.Code = ui.Exe.LoadSymbol(selected, Options{Context: ui.Config.Context})
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
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if ui.LoadError != nil {
						return material.Body1(ui.Theme, ui.LoadError.Error()).Layout(gtx)
					}
					if !ui.Code.Loaded() {
						return layout.Dimensions{}
					}
					txt := material.Body1(ui.Theme, ui.Code.Code.Name)
					txt.TextSize *= 1.2

					inset := layout.Inset{Top: 4, Left: 4, Right: 4, Bottom: 2}
					return inset.Layout(gtx, txt.Layout)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if ui.LoadError != nil || !ui.Code.Loaded() {
						return layout.Dimensions{}
					}
					txt := material.Body1(ui.Theme, "file: "+ui.Code.Code.File)
					txt.Font.Style = text.Italic

					inset := layout.Inset{Top: 2, Left: 4, Right: 4, Bottom: 4}
					return inset.Layout(gtx, txt.Layout)
				}),
				layout.Rigid(HorizontalLine{Height: 1, Color: splitterColor}.Layout),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					if ui.LoadError != nil {
						return layout.Dimensions{}
					}

					gtx.Constraints = layout.Exact(gtx.Constraints.Max)
					return layout.Stack{
						Alignment: layout.SE,
					}.Layout(gtx,
						layout.Expanded(func(gtx layout.Context) layout.Dimensions {
							return CodeUIStyle{
								CodeUI: &ui.Code,

								TryOpen: ui.tryOpen,

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
		}),
	)
}

func (ui *ExeUI) tryOpen(gtx layout.Context, call string) {
	var sym *Symbol
	for _, target := range ui.Exe.Symbols {
		if target.Name == call {
			sym = target
			break
		}
	}
	if sym == nil {
		return
	}

	load := ui.Exe.LoadSymbol(sym, Options{Context: ui.Config.Context})
	ui.Symbols.Selected = load.Name
	ui.Symbols.SelectedSymbol = sym
	ui.Symbols.List.Selected = -1
	for i, fil := range ui.Symbols.Filtered {
		if fil == sym {
			ui.Symbols.List.Selected = i
			break
		}
	}

	ui.Code.Code = load

	if ui.Symbols.Selected == "" {
		ui.Symbols.SelectIndex(0)
	}

	ui.Code.ResetScroll()
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
