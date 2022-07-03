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

type Bounds struct{ Min, Max float32 }

func BoundsWidth(min, width int) Bounds {
	return Bounds{Min: float32(min), Max: float32(min + width)}
}

func (b Bounds) Lerp(p float32) float32 {
	return b.Min + p*(b.Max-b.Min)
}

func (b Bounds) Contains(v float32) bool {
	return b.Min <= v && v <= b.Max
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
	// pad | Jump | pad/2 | Disasm | pad | Gutter | pad | Source | pad

	lineHeight := gtx.Metric.Sp(ui.LineHeight)
	pad := lineHeight
	jumpStep := lineHeight / 2
	jumpWidth := jumpStep * ui.Match.CodeMaxStack
	gutterWidth := lineHeight * 8
	blocksWidth := (gtx.Constraints.Max.X - gutterWidth - jumpWidth - 4*pad - pad/2)

	jump := BoundsWidth(pad, jumpWidth)
	disasm := BoundsWidth(int(jump.Max)+pad/2, blocksWidth*3/10)
	gutter := BoundsWidth(int(disasm.Max)+pad, gutterWidth)
	source := BoundsWidth(int(gutter.Max)+pad, blocksWidth*7/10)

	// draw gutter
	paint.FillShape(gtx.Ops, f32color.Gray8(0xE8), clip.Rect{
		Min: image.Pt(int(gutter.Min), 0),
		Max: image.Pt(int(gutter.Max), gtx.Constraints.Max.Y),
	}.Op())

	mousePosition := *ui.MousePosition
	mouseInDisasm := disasm.Contains(mousePosition.X)
	mouseInSource := source.Contains(mousePosition.X)

	lineText := material.Label(ui.Theme, ui.TextHeight, "")
	lineText.Font.Variant = "Mono"
	lineText.MaxLines = 1
	headText := material.Label(ui.Theme, ui.TextHeight, "")
	headText.MaxLines = 1
	headText.Font.Variant = "Mono"
	headText.Font.Weight = text.Heavy

	// relations underlay
	top := 0
	var highlightPath *clip.PathSpec
	var highlightColor color.NRGBA
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
					if mouseInSource {
						if float32(top) <= mousePosition.Y && mousePosition.Y < float32(top+lineHeight) {
							highlight = true
						}
					}

					var p clip.Path
					p.Begin(gtx.Ops)
					p.MoveTo(f32.Pt(gutter.Max, float32(top+lineHeight)))
					p.LineTo(f32.Pt(source.Max, float32(top+lineHeight)))
					p.LineTo(f32.Pt(source.Max, float32(top)))
					p.LineTo(f32.Pt(gutter.Max, float32(top)))
					pin := float32(top)
					for i, r := range ranges {
						if mouseInDisasm {
							if float32(r.From*lineHeight) <= mousePosition.Y && mousePosition.Y < float32(r.To*lineHeight) {
								highlight = true
							}
						}
						const S = 0.1
						p.CubeTo(
							f32.Pt(gutter.Lerp(0.5-S), pin),
							f32.Pt(gutter.Lerp(0.5+S), float32(r.From*lineHeight)),
							f32.Pt(gutter.Min, float32(r.From*lineHeight)))
						p.LineTo(f32.Pt(disasm.Min, float32(r.From*lineHeight)))
						p.LineTo(f32.Pt(disasm.Min, float32(r.To*lineHeight)))
						p.LineTo(f32.Pt(gutter.Min, float32(r.To*lineHeight)))
						pin = float32(top) + float32(lineHeight)*float32(i+1)/float32(len(ranges))
						p.CubeTo(
							f32.Pt(gutter.Lerp(0.5+S), float32(r.To*lineHeight)),
							f32.Pt(gutter.Lerp(0.5-S), pin),
							f32.Pt(gutter.Max, pin))
					}
					alpha := float32(0.4)
					pathSpec := p.End()
					if highlight {
						alpha = 0.8
					}
					relationColor := f32color.HSLA(float32(math.Mod(float64((i+1)*(off+1))*math.Phi, 1)), 0.9, 0.8, alpha)
					if !highlight {
						paint.FillShape(gtx.Ops, relationColor, clip.Outline{Path: pathSpec}.Op())
					} else {
						highlightPath = &pathSpec
						highlightColor = relationColor
					}
				}
				top += lineHeight
			}
		}
	}
	if highlightPath != nil {
		paint.FillShape(gtx.Ops, highlightColor, clip.Outline{Path: *highlightPath}.Op())
		paint.FillShape(gtx.Ops, color.NRGBA{A: 0x40}, clip.Stroke{Path: *highlightPath, Width: 2}.Op())
	}

	// disassembly
	disasmGtx := gtx
	disasmGtx.Constraints = layout.Exact(image.Point{X: gtx.Constraints.Max.X / 2, Y: gtx.Constraints.Max.Y})
	disasmGtx.Constraints.Min.X = 0
	for i, ix := range ui.Match.Code {
		stack := op.Offset(image.Pt(int(disasm.Min), i*lineHeight)).Push(gtx.Ops)
		lineText.Text = ix.Text
		lineText.Layout(disasmGtx)
		stack.Pop()

		if ix.RefOffset != 0 {
			stack := op.Offset(image.Pt(int(jump.Max), i*lineHeight)).Push(gtx.Ops)

			var path clip.Path
			path.Begin(gtx.Ops)
			path.MoveTo(f32.Pt(0, float32(lineHeight/2)))
			path.LineTo(f32.Pt(float32(-jumpStep*ix.RefStack), float32(lineHeight/2)))
			path.LineTo(f32.Pt(float32(-jumpStep*ix.RefStack), float32(lineHeight/2+ix.RefOffset*lineHeight)))
			path.LineTo(f32.Pt(float32(-jumpStep/2), float32(lineHeight/2+ix.RefOffset*lineHeight)))
			// draw arrow
			path.Line(f32.Pt(0, float32(lineHeight/4)))
			path.Line(f32.Pt(float32(lineHeight/3), float32(-lineHeight/4)))
			path.Line(f32.Pt(float32(-lineHeight/3), float32(-lineHeight/4)))
			path.Line(f32.Pt(0, float32(lineHeight/4)))

			jumpColor := f32color.HSLA(float32(math.Mod(float64(ix.PC)*math.Phi, 1)), 0.8, 0.4, 0.8)
			paint.FillShape(gtx.Ops, jumpColor, clip.Stroke{Path: path.End(), Width: 2}.Op())

			stack.Pop()
		}
	}

	// source
	top = 0
	for i, src := range ui.Match.Source {
		if i > 0 {
			top += lineHeight
		}
		stack := op.Offset(image.Pt(int(source.Min), top)).Push(gtx.Ops)
		headText.Text = src.File
		headText.Layout(gtx)
		stack.Pop()
		top += lineHeight
		for i, block := range src.Blocks {
			if i > 0 {
				top += lineHeight
			}
			for off, line := range block.Lines {
				stack := op.Offset(image.Pt(int(source.Min), top)).Push(gtx.Ops)
				lineText.Text = strconv.Itoa(block.From + off)
				lineText.Layout(gtx)
				stack.Pop()

				stack = op.Offset(image.Pt(int(source.Min)+gtx.Metric.Sp(30), top)).Push(gtx.Ops)
				lineText.Text = line
				lineText.Layout(gtx)
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
