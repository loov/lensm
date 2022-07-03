package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"log"
	"math"
	"os"
	"regexp"
	"strconv"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/f32color"
)

const N = 44

func Fibonacci(n int) int {
	if n <= 1 {
		return n
	}
	return Fibonacci(n-1) + Fibonacci(n-2)
}

func main() {
	flag.Parse()
	text := flag.Bool("text", false, "show text output")

	out, err := Parse(Options{
		Exe: flag.Arg(0),
		// Filter:     regexp.MustCompile("gioui.org.*decode"),
		Filter:     regexp.MustCompile("Fibonacci"),
		Context:    3,
		MaxSymbols: 5,
	})
	if err != nil {
		Fibonacci(3)
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
			app.Size(unit.Dp(800), unit.Dp(600)),
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

	ScrollAsm     widget.Scrollbar
	ScrollSrc     widget.Scrollbar
	MousePosition f32.Point
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
		ui.Selected = &ui.Output.Matches[0]
	}

	layout.Flex{
		Axis: layout.Horizontal,
	}.Layout(gtx,
		layout.Flexed(0.3, func(gtx layout.Context) layout.Dimensions {
			size := gtx.Constraints.Max
			gtx.Constraints = layout.Exact(size)
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
			ui.Selected = match
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
		if match == ui.Selected {
			style.Font.Weight = text.Heavy
		}
		return style.Layout(gtx) // layout.UniformInset(unit.Dp(8)).Layout(gtx, style.Layout)
	})
}

func (ui *UI) layoutCode(gtx layout.Context, match *Match) layout.Dimensions {
	return MatchUI{
		Theme:         ui.Theme,
		Match:         ui.Selected,
		ScrollAsm:     &ui.ScrollAsm,
		ScrollSrc:     &ui.ScrollSrc,
		TextHeight:    unit.Sp(12),
		LineHeight:    unit.Sp(14),
		MousePosition: &ui.MousePosition,
	}.Layout(gtx)
}

type MatchUI struct {
	Theme *material.Theme
	Match *Match

	ScrollAsm *widget.Scrollbar
	ScrollSrc *widget.Scrollbar

	TextHeight unit.Sp
	LineHeight unit.Sp

	MousePosition *f32.Point
}

func (ui MatchUI) Layout(gtx layout.Context) layout.Dimensions {
	gtx.Constraints = layout.Exact(gtx.Constraints.Max)

	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	pointer.InputOp{
		Tag:   ui.Match,
		Types: pointer.Move,
	}.Add(gtx.Ops)
	for _, ev := range gtx.Queue.Events(ui.Match) {
		if ev, ok := ev.(pointer.Event); ok {
			*ui.MousePosition = ev.Position
		}
	}

	// The layout has the following sections:
	// JumpLinks

	center := gtx.Constraints.Max.X / 2

	mousePosition := *ui.MousePosition
	mouseInDisasm := mousePosition.X < float32(center)

	lineText := material.Label(ui.Theme, ui.TextHeight, "")
	lineText.Font.Variant = "Mono"
	lineText.MaxLines = 1
	headText := material.Label(ui.Theme, ui.TextHeight, "")
	headText.MaxLines = 1
	headText.Font.Variant = "Mono"
	headText.Font.Weight = text.Heavy

	lineHeight := gtx.Metric.Sp(ui.LineHeight)

	gutter := lineHeight/2*ui.Match.CodeMaxStack + lineHeight

	// relations underlay
	sourceGtx := gtx
	sourceGtx.Constraints = layout.Exact(image.Point{X: gtx.Constraints.Max.X / 2, Y: gtx.Constraints.Max.Y})
	sourceGtx.Constraints.Min.X = 0
	top := 0
	for i, src := range ui.Match.Source {
		if i > 0 {
			top += lineHeight
		}
		top += lineHeight
		for i, block := range src.Blocks {
			if i > 0 {
				top += lineHeight
			}
			for off, ranges := range block.Disasm {
				if len(ranges) > 0 {
					highlight := false
					if !mouseInDisasm {
						if float32(top) <= mousePosition.Y && mousePosition.Y < float32(top+lineHeight) {
							highlight = true
						}
					}

					var p clip.Path
					p.Begin(gtx.Ops)
					p.MoveTo(f32.Pt(float32(center), float32(top+lineHeight)))
					p.LineTo(f32.Pt(float32(gtx.Constraints.Max.X), float32(top+lineHeight)))
					p.LineTo(f32.Pt(float32(gtx.Constraints.Max.X), float32(top)))
					p.LineTo(f32.Pt(float32(center), float32(top)))
					pin := float32(top)
					for i, r := range ranges {
						if mouseInDisasm {
							if float32(r.From*lineHeight) <= mousePosition.Y && mousePosition.Y < float32(r.To*lineHeight) {
								highlight = true
							}
						}
						p.CubeTo(
							f32.Pt(float32(center-50), pin),
							f32.Pt(float32(center-50), float32(r.From*lineHeight)),
							f32.Pt(float32(center-150), float32(r.From*lineHeight)))
						p.LineTo(f32.Pt(float32(gutter), float32(r.From*lineHeight)))
						p.LineTo(f32.Pt(float32(gutter), float32(r.To*lineHeight)))
						p.LineTo(f32.Pt(float32(center-150), float32(r.To*lineHeight)))
						pin = float32(top) + float32(lineHeight)*float32(i+1)/float32(len(ranges))
						p.CubeTo(
							f32.Pt(float32(center-50), float32(r.To*lineHeight)),
							f32.Pt(float32(center-50), pin),
							f32.Pt(float32(center), pin))
					}
					alpha := float32(0.4)
					if highlight {
						alpha = 0.8
					}
					relationColor := f32color.HSLA(float32(math.Mod(float64((i+1)*(off+1))*math.Phi, 1)), 0.9, 0.8, alpha)
					paint.FillShape(gtx.Ops, relationColor, clip.Outline{Path: p.End()}.Op())
				}
				top += lineHeight
			}
		}
	}

	// disassembly
	disasmGtx := gtx
	disasmGtx.Constraints = layout.Exact(image.Point{X: gtx.Constraints.Max.X / 2, Y: gtx.Constraints.Max.Y})
	disasmGtx.Constraints.Min.X = 0
	for i, ix := range ui.Match.Code {
		stack := op.Offset(image.Pt(gutter, i*lineHeight)).Push(gtx.Ops)
		lineText.Text = ix.Text
		lineText.Layout(disasmGtx)
		if ix.RefOffset != 0 {
			var path clip.Path
			path.Begin(gtx.Ops)
			path.MoveTo(f32.Pt(float32(-lineHeight/2), float32(lineHeight/2)))
			path.LineTo(f32.Pt(float32(-lineHeight/2-lineHeight/2*ix.RefStack), float32(lineHeight/2)))
			path.LineTo(f32.Pt(float32(-lineHeight/2-lineHeight/2*ix.RefStack), float32(lineHeight/2+ix.RefOffset*lineHeight)))
			path.LineTo(f32.Pt(float32(-lineHeight), float32(lineHeight/2+ix.RefOffset*lineHeight)))
			// draw arrow
			path.Line(f32.Pt(0, float32(lineHeight/4)))
			path.Line(f32.Pt(float32(lineHeight/3), float32(-lineHeight/4)))
			path.Line(f32.Pt(float32(-lineHeight/3), float32(-lineHeight/4)))
			path.Line(f32.Pt(0, float32(lineHeight/4)))

			jumpColor := f32color.HSLA(float32(math.Mod(float64(ix.PC)*math.Phi, 1)), 0.8, 0.4, 0.8)
			paint.FillShape(gtx.Ops, jumpColor, clip.Stroke{Path: path.End(), Width: 2}.Op())
		}
		stack.Pop()
	}

	// source
	top = 0
	for i, src := range ui.Match.Source {
		if i > 0 {
			top += lineHeight
		}
		stack := op.Offset(image.Pt(center, top)).Push(gtx.Ops)
		headText.Text = src.File
		headText.Layout(sourceGtx)
		stack.Pop()
		top += lineHeight
		for i, block := range src.Blocks {
			if i > 0 {
				top += lineHeight
			}
			for off, line := range block.Lines {
				stack := op.Offset(image.Pt(center, top)).Push(gtx.Ops)
				lineText.Text = strconv.Itoa(block.From + off)
				lineText.Layout(sourceGtx)
				stack.Pop()

				stack = op.Offset(image.Pt(center+gtx.Metric.Sp(30), top)).Push(gtx.Ops)
				lineText.Text = line
				lineText.Layout(sourceGtx)
				stack.Pop()

				top += lineHeight
			}
		}
	}

	return layout.Dimensions{
		Size: gtx.Constraints.Max,
	}
}

var (
	secondaryBackground = color.NRGBA{R: 0xF0, G: 0xF0, B: 0xF0, A: 0xFF}
	splitterColor       = color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xFF}
)
