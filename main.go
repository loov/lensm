package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"sync"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/font/opentype"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

func main() {
	filter := flag.String("filter", "", "filter the symbol by regexp")
	context := flag.Int("context", 3, "source line context")
	font := flag.String("font", "", "user font")
	maxMatches := flag.Int("max-matches", 10, "maximum number of matches to parse")
	flag.Parse()
	exename := flag.Arg(0)

	if exename == "" || *filter == "" {
		fmt.Fprintln(os.Stderr, "lensm -filter main <exename>")
		flag.Usage()
		os.Exit(1)
	}

	re, err := regexp.Compile(*filter)
	if err != nil {
		panic(err)
	}

	out, err := Parse(Options{
		Exe:        exename,
		Filter:     re,
		Context:    *context,
		MaxSymbols: *maxMatches,
	})
	if err != nil {
		panic(err)
	}

	windows := &Windows{}

	ui := NewUI(windows, *font)
	ui.Output = out
	windows.Open("lensm", image.Pt(1400, 900), ui.Run)

	go func() {
		windows.Wait()
		os.Exit(0)
	}()

	// This starts Gio main.
	app.Main()
}

type Windows struct {
	active sync.WaitGroup
}

func (windows *Windows) Open(title string, sizeDp image.Point, run func(*app.Window) error) {
	windows.active.Add(1)
	go func() {
		defer windows.active.Done()

		window := app.NewWindow(
			app.Title(title),
			app.Size(unit.Dp(sizeDp.X), unit.Dp(sizeDp.Y)),
		)
		if err := run(window); err != nil {
			log.Println(err)
		}
	}()
}

func (windows *Windows) Wait() {
	windows.active.Wait()
}

type UI struct {
	Windows *Windows
	Theme   *material.Theme

	Output   *Output
	Matches  widget.List
	Selected *Match
	MatchUI  MatchUIState

	OpenInNew widget.Clickable
}

func fontCollection(path string) []text.FontFace {
	collection := gofont.Collection()
	if path == "" {
		return collection
	}
	b, err := ioutil.ReadFile(path)
	if err != nil {
		panic(fmt.Errorf("failed to parse font: %v", err))
	}
	face, err := opentype.Parse(b)
	if err != nil {
		panic(fmt.Errorf("failed to parse font: %v", err))
	}
	fnt := text.Font{Variant: "Mono", Weight: text.Normal}
	fface := text.FontFace{Font: fnt, Face: face}
	return append(collection, fface)
}

func NewUI(windows *Windows, userfont string) *UI {
	ui := &UI{}
	ui.Windows = windows
	ui.Theme = material.NewTheme(fontCollection(userfont))
	ui.Matches.List.Axis = layout.Vertical
	return ui
}

func (ui *UI) Run(w *app.Window) error {
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

func (ui *UI) Layout(gtx layout.Context) {
	if ui.Selected == nil && len(ui.Output.Matches) > 0 {
		ui.selectMatch(&ui.Output.Matches[0])
	}

	for ui.OpenInNew.Clicked() {
		ui.openInNew(gtx)
	}

	layout.Flex{
		Axis: layout.Horizontal,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := gtx.Constraints.Max
			gtx.Constraints = layout.Exact(image.Point{
				X: gtx.Metric.Sp(10 * 20),
				Y: gtx.Constraints.Max.Y,
			})
			paint.FillShape(gtx.Ops, secondaryBackground, clip.Rect{Max: size}.Op())
			return ui.layoutMatches(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			size := image.Point{
				X: gtx.Metric.Dp(1),
				Y: gtx.Constraints.Max.Y,
			}
			paint.FillShape(gtx.Ops, splitterColor, clip.Rect{Max: size}.Op())
			return layout.Dimensions{
				Size: size,
			}
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints = layout.Exact(gtx.Constraints.Max)
			if ui.Selected == nil {
				return material.H4(ui.Theme, "no matches").Layout(gtx)
			}
			return layout.Stack{
				Alignment: layout.SE,
			}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					return ui.layoutCode(gtx, ui.Selected)
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

func WidgetWindow(widget layout.Widget) func(*app.Window) error {
	return func(w *app.Window) error {
		var ops op.Ops
		for {
			select {
			case e := <-w.Events():
				switch e := e.(type) {
				case system.FrameEvent:
					gtx := layout.NewContext(&ops, e)
					widget(gtx)
					e.Frame(gtx.Ops)

				case system.DestroyEvent:
					return e.Err
				}
			}
		}
	}
}

func (ui *UI) openInNew(gtx layout.Context) {
	state := ui.MatchUI
	style := MatchUIStyle{
		Theme:        ui.Theme,
		Match:        ui.Selected,
		MatchUIState: &state,

		TextHeight: unit.Sp(12),
		LineHeight: unit.Sp(14),
	}

	size := gtx.Constraints.Max
	size.X = int(float32(size.X) / gtx.Metric.PxPerDp)
	size.Y = int(float32(size.Y) / gtx.Metric.PxPerDp)
	ui.Windows.Open(ui.Selected.Name, size, WidgetWindow(style.Layout))
}

func (ui *UI) layoutMatches(gtx layout.Context) layout.Dimensions {
	n := len(ui.Output.Matches)
	if ui.Output.More {
		n += 1
	}

	for i := range ui.Output.Matches {
		match := &ui.Output.Matches[i]
		for match.Select.Clicked() {
			ui.selectMatch(match)
		}
	}

	return material.List(ui.Theme, &ui.Matches).Layout(gtx, n,
		func(gtx layout.Context, index int) layout.Dimensions {
			if index >= len(ui.Output.Matches) {
				return material.Body2(ui.Theme, "... too many matches ...").Layout(gtx)
			}
			return ui.layoutMatch(gtx, &ui.Output.Matches[index])
		})
}

func (ui *UI) layoutMatch(gtx layout.Context, match *Match) layout.Dimensions {
	return material.Clickable(gtx, &match.Select, func(gtx layout.Context) layout.Dimensions {
		style := material.Body2(ui.Theme, match.Name)
		style.MaxLines = 1
		style.TextSize = unit.Sp(10)
		if match == ui.Selected {
			style.Font.Weight = text.Heavy
		}
		tgtx := gtx
		tgtx.Constraints.Max.X = 100000
		dims := style.Layout(tgtx) // layout.UniformInset(unit.Dp(8)).Layout(gtx, style.Layout)
		return layout.Dimensions{
			Size: image.Point{
				X: gtx.Constraints.Max.X,
				Y: dims.Size.Y,
			},
		}
	})
}

func (ui *UI) selectMatch(target *Match) {
	if ui.Selected == target {
		return
	}
	ui.Selected = target
	ui.MatchUI.asm.scroll = 100000
	ui.MatchUI.src.scroll = 100000
}

func (ui *UI) layoutCode(gtx layout.Context, match *Match) layout.Dimensions {
	return MatchUIStyle{
		Theme:        ui.Theme,
		Match:        ui.Selected,
		MatchUIState: &ui.MatchUI,
		TextHeight:   unit.Sp(12),
		LineHeight:   unit.Sp(14),
	}.Layout(gtx)
}

var (
	secondaryBackground = color.NRGBA{R: 0xF0, G: 0xF0, B: 0xF0, A: 0xFF}
	splitterColor       = color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xFF}
)
