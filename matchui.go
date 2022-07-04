package main

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/gesture"
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

type MatchUIState struct {
	ScrollAsm float32
	scrollAsm gesture.Scroll
	ScrollSrc float32
	scrollSrc gesture.Scroll

	ScrollbarAsm widget.Scrollbar
	ScrollbarSrc widget.Scrollbar

	MousePosition f32.Point
}

type MatchUIStyle struct {
	Theme *material.Theme
	Match *Match
	*MatchUIState

	TextHeight unit.Sp
	LineHeight unit.Sp
}

type Bounds struct{ Min, Max float32 }

func BoundsWidth(min, width int) Bounds {
	return Bounds{Min: float32(min), Max: float32(min + width)}
}

func (b Bounds) Width() float32 { return b.Max - b.Min }

func (b Bounds) Lerp(p float32) float32 {
	return b.Min + p*(b.Max-b.Min)
}

func (b Bounds) Contains(v float32) bool {
	return b.Min <= v && v <= b.Max
}

func (ui MatchUIStyle) Layout(gtx layout.Context) layout.Dimensions {
	gtx.Constraints = layout.Exact(gtx.Constraints.Max)

	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()

	pointer.InputOp{
		Tag:   ui.Match,
		Types: pointer.Move,
	}.Add(gtx.Ops)
	for _, ev := range gtx.Queue.Events(ui.Match) {
		if ev, ok := ev.(pointer.Event); ok {
			switch ev.Type {
			case pointer.Move:
				ui.MousePosition = ev.Position
			}
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

	mousePosition := ui.MousePosition
	mouseInDisasm := disasm.Contains(mousePosition.X)
	mouseInSource := source.Contains(mousePosition.X)
	highlightDisasmIndex := -1
	if mouseInDisasm {
		highlightDisasmIndex = int(mousePosition.Y-ui.ScrollAsm) / lineHeight
	}
	var highlightRanges []Range

	// relations underlay
	top := int(ui.ScrollSrc)
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
							if float32(r.From*lineHeight)+ui.ScrollAsm <= mousePosition.Y && mousePosition.Y < float32(r.To*lineHeight)+ui.ScrollAsm {
								highlight = true
								highlightRanges = ranges
							}
						}
						const S = 0.1
						p.CubeTo(
							f32.Pt(gutter.Lerp(0.5-S), pin),
							f32.Pt(gutter.Lerp(0.5+S), float32(r.From*lineHeight)+ui.ScrollAsm),
							f32.Pt(gutter.Min, float32(r.From*lineHeight)+ui.ScrollAsm))
						p.LineTo(f32.Pt(disasm.Min, float32(r.From*lineHeight)+ui.ScrollAsm))
						p.LineTo(f32.Pt(disasm.Min, float32(r.To*lineHeight)+ui.ScrollAsm))
						p.LineTo(f32.Pt(gutter.Min, float32(r.To*lineHeight)+ui.ScrollAsm))
						pin = float32(top) + float32(lineHeight)*float32(i+1)/float32(len(ranges))
						p.CubeTo(
							f32.Pt(gutter.Lerp(0.5+S), float32(r.To*lineHeight)+ui.ScrollAsm),
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
		paint.FillShape(gtx.Ops, color.NRGBA{A: 0x40}, clip.Stroke{Path: *highlightPath, Width: 1}.Op())
	}

	// disassembly
	disasmClip := clip.Rect{
		Min: image.Pt(int(jump.Min), 0),
		Max: image.Pt(int(gutter.Min), gtx.Constraints.Max.Y),
	}.Push(gtx.Ops)
	for i, ix := range ui.Match.Code {
		SourceLine{
			TopLeft:    image.Pt(int(disasm.Min)+pad/2, i*lineHeight+int(ui.ScrollAsm)),
			Text:       ix.Text,
			TextHeight: ui.TextHeight,
			Bold:       highlightDisasmIndex == i,
			Color:      f32color.Black,
		}.Layout(ui.Theme, gtx)

		// jump line
		if ix.RefOffset != 0 {
			lineWidth := gtx.Metric.Dp(1)
			align := float32(lineWidth%2) / 2
			stack := op.Affine(f32.Affine2D{}.Offset(
				f32.Pt(jump.Max+align, float32(i*lineHeight)+align+ui.ScrollAsm))).Push(gtx.Ops)

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

			width := float32(lineWidth)
			alpha := float32(0.7)
			if highlightDisasmIndex >= 0 && (highlightDisasmIndex == i || highlightDisasmIndex == i+ix.RefOffset) {
				width *= 3
				alpha = 1
			} else if rangesContains(highlightRanges, i, i+ix.RefOffset) {
				width *= 3
			}
			jumpColor := f32color.HSLA(float32(math.Mod(float64(ix.PC)*math.Phi, 1)), 0.8, 0.4, alpha)
			paint.FillShape(gtx.Ops, jumpColor, clip.Stroke{Path: path.End(), Width: width}.Op())

			stack.Pop()
		}
	}
	disasmClip.Pop()

	// source
	sourceClip := clip.Rect{
		Min: image.Pt(int(source.Min), 0),
		Max: image.Pt(int(source.Max), gtx.Constraints.Max.Y),
	}.Push(gtx.Ops)
	top = int(ui.ScrollSrc)
	for i, src := range ui.Match.Source {
		if i > 0 {
			top += lineHeight
		}
		SourceLine{
			TopLeft:    image.Pt(int(source.Min), top),
			Text:       src.File,
			TextHeight: ui.TextHeight,
			Bold:       highlightDisasmIndex == i,
			Color:      f32color.Black,
		}.Layout(ui.Theme, gtx)
		top += lineHeight
		for i, block := range src.Blocks {
			if i > 0 {
				top += lineHeight
			}
			for off, line := range block.Lines {
				highlight := mouseInSource && float32(top) <= mousePosition.Y && mousePosition.Y < float32(top+lineHeight)
				SourceLine{
					TopLeft:    image.Pt(int(source.Min), top),
					Text:       fmt.Sprintf("%-4d %s", block.From+off, line),
					TextHeight: ui.TextHeight,
					Bold:       highlight,
					Color:      f32color.Black,
				}.Layout(ui.Theme, gtx)
				top += lineHeight
			}
		}
	}
	sourceClip.Pop()
	sourceContentHeight := top - int(ui.ScrollSrc)

	{
		stack := clip.Rect{
			Min: image.Pt(int(jump.Min)-pad, 0),
			Max: image.Pt(int(disasm.Max), gtx.Constraints.Max.Y),
		}.Push(gtx.Ops)

		// overflow := gtx.Constraints.Max.Y / 3
		overflow := lineHeight
		contentTop := float32(-overflow)
		contentBot := float32(len(ui.Match.Code)*lineHeight + overflow)
		viewTop := -ui.ScrollAsm
		viewBot := -ui.ScrollAsm + float32(gtx.Constraints.Max.Y)

		ui.scrollAsm.Add(gtx.Ops, image.Rect(0, -1000, 0, 1000))

		{
			stack := op.Offset(image.Pt(int(jump.Min)-pad, 0)).Push(gtx.Ops)
			gtx := gtx
			gtx.Constraints = layout.Exact(image.Pt(pad, gtx.Constraints.Max.Y))
			material.Scrollbar(ui.Theme, &ui.ScrollbarAsm).Layout(gtx, layout.Vertical,
				(viewTop-contentTop)/(contentBot-contentTop),
				(viewBot-contentTop)/(contentBot-contentTop),
			)
			stack.Pop()
		}

		if distance := ui.ScrollbarAsm.ScrollDistance(); distance != 0 {
			ui.ScrollAsm -= distance * (contentBot - contentTop)
		}
		if distance := ui.scrollAsm.Scroll(gtx.Metric, gtx, gtx.Now, gesture.Vertical); distance != 0 {
			ui.ScrollAsm -= float32(distance)
		}

		if -ui.ScrollAsm < contentTop {
			ui.ScrollAsm = -contentTop
		}
		if -ui.ScrollAsm+float32(gtx.Constraints.Max.Y) > contentBot {
			if contentBot < float32(gtx.Constraints.Max.Y) {
				ui.ScrollAsm = -contentTop
			} else {
				ui.ScrollAsm = float32(gtx.Constraints.Max.Y) - contentBot
			}
		}
		stack.Pop()
	}

	{
		stack := clip.Rect{
			Min: image.Pt(int(source.Min), 0),
			Max: image.Pt(int(source.Max)+pad, gtx.Constraints.Max.Y),
		}.Push(gtx.Ops)

		// overflow := gtx.Constraints.Max.Y / 3
		overflow := lineHeight
		contentTop := float32(-overflow)
		contentBot := float32(sourceContentHeight + overflow)
		viewTop := -ui.ScrollSrc
		viewBot := -ui.ScrollSrc + float32(gtx.Constraints.Max.Y)

		ui.scrollSrc.Add(gtx.Ops, image.Rect(0, -1000, 0, 1000))

		{
			stack := op.Offset(image.Pt(int(source.Max), 0)).Push(gtx.Ops)
			gtx := gtx
			gtx.Constraints = layout.Exact(image.Pt(pad, gtx.Constraints.Max.Y))
			material.Scrollbar(ui.Theme, &ui.ScrollbarSrc).Layout(gtx, layout.Vertical,
				(viewTop-contentTop)/(contentBot-contentTop),
				(viewBot-contentTop)/(contentBot-contentTop),
			)
			stack.Pop()
		}

		if distance := ui.ScrollbarSrc.ScrollDistance(); distance != 0 {
			ui.ScrollSrc -= distance * (contentBot - contentTop)
		}
		if distance := ui.scrollSrc.Scroll(gtx.Metric, gtx, gtx.Now, gesture.Vertical); distance != 0 {
			ui.ScrollSrc -= float32(distance)
		}

		if -ui.ScrollSrc < contentTop {
			ui.ScrollSrc = -contentTop
		}
		if -ui.ScrollSrc+float32(gtx.Constraints.Max.Y) > contentBot {
			if contentBot < float32(gtx.Constraints.Max.Y) {
				ui.ScrollSrc = -contentTop
			} else {
				ui.ScrollSrc = float32(gtx.Constraints.Max.Y) - contentBot
			}
		}
		stack.Pop()
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

type SourceLine struct {
	TopLeft    image.Point
	Width      int
	Text       string
	TextHeight unit.Sp
	Bold       bool
	Color      color.NRGBA
}

func (line SourceLine) Layout(th *material.Theme, gtx layout.Context) {
	gtx.Constraints.Min.X = 0
	gtx.Constraints.Max.X = math.MaxInt
	gtx.Constraints.Min.Y = 0
	gtx.Constraints.Max.Y = math.MaxInt

	defer op.Offset(line.TopLeft).Push(gtx.Ops).Pop()
	if line.Width > 0 {
		defer clip.Rect{Max: image.Pt(line.Width, gtx.Metric.Sp(line.TextHeight))}.Push(gtx.Ops).Pop()
	}

	font := text.Font{Variant: "Mono"}
	if line.Bold {
		font.Weight = text.Heavy
	}
	paint.ColorOp{Color: line.Color}.Add(gtx.Ops)
	widget.Label{MaxLines: 1}.Layout(gtx, th.Shaper, font, line.TextHeight, line.Text)
}
