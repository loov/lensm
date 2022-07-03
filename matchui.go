package main

import (
	"image"
	"image/color"
	"math"
	"strconv"

	"gioui.org/f32"
	"gioui.org/io/pointer"
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
	var highlightDisasmIndex int
	if mouseInDisasm {
		highlightDisasmIndex = int(mousePosition.Y) / lineHeight
	}
	var highlightRanges []Range

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
							highlightRanges = ranges
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
								highlightRanges = ranges
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
		stack := op.Offset(image.Pt(int(disasm.Min)+pad/2, i*lineHeight)).Push(gtx.Ops)
		lineText.Text = ix.Text
		lineText.Layout(disasmGtx)
		stack.Pop()

		// jump line
		if ix.RefOffset != 0 {
			stack := op.Offset(image.Pt(int(jump.Max), i*lineHeight)).Push(gtx.Ops)

			var path clip.Path
			path.Begin(gtx.Ops)
			path.MoveTo(f32.Pt(float32(pad/2), float32(lineHeight*2/3)))
			path.LineTo(f32.Pt(float32(-jumpStep*ix.RefStack), float32(lineHeight*2/3)))
			path.LineTo(f32.Pt(float32(-jumpStep*ix.RefStack), float32(lineHeight/3+ix.RefOffset*lineHeight)))
			path.LineTo(f32.Pt(float32(-jumpStep/2), float32(lineHeight/3+ix.RefOffset*lineHeight)))
			// draw arrow
			path.Line(f32.Pt(0, float32(lineHeight/4)))
			path.Line(f32.Pt(float32(lineHeight/3), float32(-lineHeight/4)))
			path.Line(f32.Pt(float32(-lineHeight/3), float32(-lineHeight/4)))
			path.Line(f32.Pt(0, float32(lineHeight/4)))

			width := float32(2)
			alpha := float32(0.7)
			if highlightDisasmIndex >= 0 && (highlightDisasmIndex == i || highlightDisasmIndex == i+ix.RefOffset) {
				width = 8
				alpha = 1
			} else if rangesContains(highlightRanges, i, i+ix.RefOffset) {
				width = 8
			}
			jumpColor := f32color.HSLA(float32(math.Mod(float64(ix.PC)*math.Phi, 1)), 0.8, 0.4, alpha)
			paint.FillShape(gtx.Ops, jumpColor, clip.Stroke{Path: path.End(), Width: width}.Op())

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

func rangesContains(ranges []Range, a, b int) bool {
	for _, r := range ranges {
		if (r.From <= a && a < r.To) || (r.From <= b && b < r.To) {
			return true
		}
	}
	return false
}
