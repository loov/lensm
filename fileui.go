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

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/wasmobj"
)

type FileUIConfig struct {
	Path    string
	Watch   bool
	Context int
}

type FileUI struct {
	Windows *Windows
	Theme   *material.Theme

	Config FileUIConfig

	LoadError error

	// Currently loaded executable.
	File  disasm.File
	Funcs *FilterList[disasm.Func]

	// Active code view.
	Code CodeUI

	// Other FileUI elements.
	OpenInNew widget.Clickable
}

func NewExeUI(windows *Windows, theme *material.Theme) *FileUI {
	ui := &FileUI{}
	ui.Windows = windows
	ui.Theme = theme
	ui.Funcs = NewFilterList[disasm.Func](theme)
	return ui
}

func (ui *FileUI) Run(w *app.Window) error {
	var ops op.Ops

	exited := make(chan struct{})
	defer close(exited)

	fileLoaded := make(chan disasm.File, 1)
	fileLoadError := make(chan error, 1)

	loadFinished := func(exe disasm.File, err error) {
		if err == nil {
			select {
			case <-fileLoaded:
			default:
			}
			fileLoaded <- exe
		} else {
			select {
			case <-fileLoadError:
			default:
			}
			fileLoadError <- err
		}
	}

	go func() {
		var lastModTime time.Time
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			func() {
				stat, err := os.Stat(ui.Config.Path)
				if err != nil {
					loadFinished(nil, err)
					return
				}
				if stat.ModTime().Equal(lastModTime) {
					return
				}
				lastModTime = stat.ModTime()

				file, err := wasmobj.Load(ui.Config.Path)
				loadFinished(file, err)
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
		case err := <-fileLoadError:
			ui.LoadError = err
			w.Invalidate()
		case file := <-fileLoaded:
			ui.SetFile(file)
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

func (ui *FileUI) SetFile(file disasm.File) {
	if ui.File != nil {
		_ = ui.File.Close()
	}
	ui.File = file
	ui.Funcs.SetItems(file.Funcs())
	if ui.Funcs.Selected != "" {
		for _, fn := range file.Funcs() {
			if fn.Name() == ui.Funcs.Selected {
				ui.Code.Code = fn.Load(ui.loadOptions())
			}
		}
	}
}

func (ui *FileUI) loadOptions() disasm.Options {
	return disasm.Options{Context: ui.Config.Context}
}

func (ui *FileUI) Layout(gtx layout.Context) {
	for ui.OpenInNew.Clicked() {
		ui.openInNew(gtx)
	}

	if ui.Funcs.Selected == "" {
		ui.Funcs.SelectIndex(0)
	}

	if !ui.Code.Loaded() || ui.Code.Name != ui.Funcs.Selected {
		selected := ui.Funcs.SelectedItem
		if selected != nil {
			ui.Code.Code = selected.Load(ui.loadOptions())
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
			return ui.Funcs.Layout(ui.Theme, gtx)
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

func (ui *FileUI) tryOpen(gtx layout.Context, call string) {
	var fn disasm.Func
	for _, target := range ui.File.Funcs() {
		if target.Name() == call {
			fn = target
			break
		}
	}
	if fn == nil {
		return
	}

	load := fn.Load(ui.loadOptions())
	ui.Funcs.Selected = load.Name
	ui.Funcs.SelectedItem = fn
	ui.Funcs.List.Selected = -1
	for i, fil := range ui.Funcs.Filtered {
		if fil == fn {
			ui.Funcs.List.Selected = i
			break
		}
	}

	ui.Code.Code = load

	if ui.Funcs.Selected == "" {
		ui.Funcs.SelectIndex(0)
	}

	ui.Code.ResetScroll()
}

func (ui *FileUI) openInNew(gtx layout.Context) {
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
