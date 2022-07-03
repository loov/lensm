package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"regexp"

	"gioui.org/app"
	"gioui.org/font/gofont"
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

const N = 44

func Fibonacci(n int) int {
	if n <= 1 {
		return n
	}
	return Fibonacci(n-1) + Fibonacci(n-2)
}

var _ = Fibonacci(0)

func main() {
	text := flag.Bool("text", false, "show text output")
	filter := flag.String("filter", "", "filter the symbol by regexp")
	context := flag.Int("context", 3, "source line context")
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

	if *text {
		for _, symbol := range out.Matches {
			fmt.Printf("\n\n// func %v (%v)\n", symbol.Name, symbol.File)
			for _, ix := range symbol.Code {
				if ix.RefPC != 0 {
					fmt.Printf("    %-60v %v@%3v %08x --> %08x\n", ix.Text, ix.File, ix.Line, ix.PC, ix.RefPC)
				} else {
					fmt.Printf("    %-60v %v@%3v %08x\n", ix.Text, ix.File, ix.Line, ix.PC)
				}
			}

			fmt.Printf("// CONTEXT\n")
			for _, source := range symbol.Source {
				fmt.Printf("// FILE  %v\n", source.File)
				for i, block := range source.Blocks {
					if i > 0 {
						fmt.Printf("...:\n")
					}
					for line, text := range block.Lines {
						fmt.Printf("%3d:  %v\n", block.From+line, text)
					}
				}
			}
		}
		fmt.Println("MORE", out.More)
		os.Exit(0)
	}

	ui := NewUI()
	ui.Output = out

	// This creates a new application window and starts the UI.
	go func() {
		w := app.NewWindow(
			app.Title("lensm"),
			app.Size(unit.Dp(1400), unit.Dp(900)),
		)
		if err := ui.Run(w); err != nil {
			log.Println(err)
			os.Exit(1)
		}
		os.Exit(0)
	}()

	// This starts Gio main.
	app.Main()
}

type UI struct {
	Theme *material.Theme

	Output   *Output
	Matches  widget.List
	Selected *Match
	MatchUI  MatchUIState
}

func NewUI() *UI {
	ui := &UI{}
	ui.Theme = material.NewTheme(gofont.Collection())
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
				return material.H4(ui.Theme, "nothing selected").Layout(gtx)
			}
			return ui.layoutCode(gtx, ui.Selected)
		}),
	)
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
	ui.MatchUI.ScrollAsm = 100000
	ui.MatchUI.ScrollSrc = 100000
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
