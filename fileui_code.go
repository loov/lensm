package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/f32color"
)

type CodeUI struct {
	*disasm.Code

	asm struct {
		scroll  float32
		gesture gesture.Scroll
		bar     widget.Scrollbar
		anim    ScrollAnimation
	}
	src struct {
		scroll  float32
		gesture gesture.Scroll
		bar     widget.Scrollbar
	}

	mousePosition f32.Point
}

func (ui *CodeUI) Loaded() bool {
	return ui.Code != nil
}

func (ui *CodeUI) ResetScroll() {
	ui.asm.scroll = 100000
	ui.src.scroll = 100000
}

type CodeUIStyle struct {
	*CodeUI

	TryOpen func(gtx layout.Context, funcname string)
	Theme   *material.Theme

	TextHeight unit.Sp
	LineHeight unit.Sp
}

func (ui CodeUIStyle) Layout(gtx layout.Context) layout.Dimensions {
	gtx.Constraints = layout.Exact(gtx.Constraints.Max)
	if ui.Code == nil {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}

	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()

	mouseClicked := false

	event.Op(gtx.Ops, ui.Code)
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: ui.Code,
			Kinds:  pointer.Move | pointer.Press,
		})
		if !ok {
			break
		}
		if ev, ok := ev.(pointer.Event); ok {
			switch ev.Kind {
			case pointer.Move:
				ui.mousePosition = ev.Position
			case pointer.Press:
				mouseClicked = true
			}
		}
	}

	// The layout has the following sections:
	// pad | Jump | pad/2 | Related | pad | Gutter | pad | Source | pad

	lineHeight := gtx.Metric.Sp(ui.LineHeight)
	pad := lineHeight
	jumpStep := lineHeight / 2
	jumpWidth := jumpStep * ui.Code.MaxJump
	gutterWidth := lineHeight * 8
	blocksWidth := gtx.Constraints.Max.X - gutterWidth - jumpWidth - 4*pad - pad/2

	jump := BoundsWidth(pad, jumpWidth)
	asm := BoundsWidth(int(jump.Max)+pad/2, blocksWidth*3/10)
	gutter := BoundsWidth(int(asm.Max)+pad, gutterWidth)
	source := BoundsWidth(int(gutter.Max)+pad, blocksWidth*7/10)

	// draw gutter
	paint.FillShape(gtx.Ops, f32color.Gray8(0xE8), clip.Rect{
		Min: image.Pt(int(gutter.Min), 0),
		Max: image.Pt(int(gutter.Max), gtx.Constraints.Max.Y),
	}.Op())

	if scroll, ok := ui.asm.anim.Update(gtx); ok {
		ui.asm.scroll = scroll
	}

	mousePosition := ui.mousePosition
	mouseInAsm := asm.Contains(mousePosition.X)
	mouseInSource := source.Contains(mousePosition.X)
	highlightAsmIndex := -1
	if mouseInAsm {
		highlightAsmIndex = int(mousePosition.Y-ui.asm.scroll) / lineHeight
	}
	var highlightRanges []disasm.LineRange

	if InRange(highlightAsmIndex, len(ui.Code.Insts)) {
		ix := &ui.Code.Insts[highlightAsmIndex]
		if ui.TryOpen != nil && ix.Call != "" {
			pointer.CursorPointer.Add(gtx.Ops)
			if mouseClicked {
				ui.TryOpen(gtx, ix.Call)
			}
		}
		if ix.Call == "" && ix.RefOffset != 0 {
			pointer.CursorPointer.Add(gtx.Ops)
			if mouseClicked {
				// TODO: smooth scroll
				// highlightAsmIndex -= ix.RefOffset
				ui.asm.anim.Start(gtx, ui.asm.scroll, ui.asm.scroll-float32(ix.RefOffset*lineHeight), 150*time.Millisecond)
			}
		}
	}

	// relations underlay
	top := int(ui.src.scroll)
	var highlightPath *clip.PathSpec
	var highlightColor color.NRGBA
	for i, src := range ui.Code.Source {
		if i > 0 {
			top += lineHeight
		}
		top += lineHeight
		for i, block := range src.Blocks {
			if i > 0 {
				top += lineHeight
			}
			for off, ranges := range block.Related {
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
						if mouseInAsm {
							if float32(r.From*lineHeight)+ui.asm.scroll <= mousePosition.Y && mousePosition.Y < float32(r.To*lineHeight)+ui.asm.scroll {
								highlight = true
								highlightRanges = ranges
							}
						}
						const S = 0.1
						p.CubeTo(
							f32.Pt(gutter.Lerp(0.5-S), pin),
							f32.Pt(gutter.Lerp(0.5+S), float32(r.From*lineHeight)+ui.asm.scroll),
							f32.Pt(gutter.Min, float32(r.From*lineHeight)+ui.asm.scroll))
						p.LineTo(f32.Pt(asm.Min, float32(r.From*lineHeight)+ui.asm.scroll))
						p.LineTo(f32.Pt(asm.Min, float32(r.To*lineHeight)+ui.asm.scroll))
						p.LineTo(f32.Pt(gutter.Min, float32(r.To*lineHeight)+ui.asm.scroll))
						pin = float32(top) + float32(lineHeight)*float32(i+1)/float32(len(ranges))
						p.CubeTo(
							f32.Pt(gutter.Lerp(0.5+S), float32(r.To*lineHeight)+ui.asm.scroll),
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

	// assembly
	asmClip := clip.Rect{
		Min: image.Pt(int(jump.Min), 0),
		Max: image.Pt(int(gutter.Min), gtx.Constraints.Max.Y),
	}.Push(gtx.Ops)
	for i, ix := range ui.Code.Insts {
		SourceLine{
			TopLeft:    image.Pt(int(asm.Min)+pad/2, i*lineHeight+int(ui.asm.scroll)),
			Text:       ix.Text,
			TextHeight: ui.TextHeight,
			Italic:     ix.Call != "",
			Bold:       highlightAsmIndex == i,
			Color:      f32color.Black,
		}.Layout(ui.Theme, gtx)

		// jump line
		if ix.RefOffset != 0 {
			lineWidth := gtx.Metric.Dp(1)
			align := float32(lineWidth%2) / 2
			stack := op.Affine(f32.Affine2D{}.Offset(
				f32.Pt(jump.Max+align, float32(i*lineHeight)+align+ui.asm.scroll))).Push(gtx.Ops)

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
			if highlightAsmIndex >= 0 && (highlightAsmIndex == i || highlightAsmIndex == i+ix.RefOffset) {
				width *= 3
				alpha = 1
			} else if disasm.LineRangesContain(highlightRanges, i, i+ix.RefOffset) {
				width *= 3
			}
			jumpColor := f32color.HSLA(float32(math.Mod(float64(ix.PC)*math.Phi, 1)), 0.8, 0.4, alpha)
			paint.FillShape(gtx.Ops, jumpColor, clip.Stroke{Path: path.End(), Width: width}.Op())

			stack.Pop()
		}
	}
	asmClip.Pop()

	// source
	sourceClip := clip.Rect{
		Min: image.Pt(int(source.Min), 0),
		Max: image.Pt(int(source.Max), gtx.Constraints.Max.Y),
	}.Push(gtx.Ops)
	top = int(ui.src.scroll)
	for i, src := range ui.Code.Source {
		if i > 0 {
			top += lineHeight
		}
		SourceLine{
			TopLeft:    image.Pt(int(source.Min), top),
			Text:       src.File,
			TextHeight: ui.TextHeight,
			Bold:       highlightAsmIndex == i,
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
	sourceContentHeight := top - int(ui.src.scroll)

	{
		stack := clip.Rect{
			Min: image.Pt(int(jump.Min)-pad, 0),
			Max: image.Pt(int(asm.Max), gtx.Constraints.Max.Y),
		}.Push(gtx.Ops)

		// overflow := gtx.Constraints.Max.Y / 3
		overflow := lineHeight
		contentTop := float32(-overflow)
		contentBot := float32(len(ui.Code.Insts)*lineHeight + overflow)
		viewTop := -ui.asm.scroll
		viewBot := -ui.asm.scroll + float32(gtx.Constraints.Max.Y)

		{
			stack := op.Offset(image.Pt(int(jump.Min)-pad, 0)).Push(gtx.Ops)
			gtx := gtx
			gtx.Constraints = layout.Exact(image.Pt(pad, gtx.Constraints.Max.Y))
			material.Scrollbar(ui.Theme, &ui.asm.bar).Layout(gtx, layout.Vertical,
				(viewTop-contentTop)/(contentBot-contentTop),
				(viewBot-contentTop)/(contentBot-contentTop),
			)
			stack.Pop()
		}

		if distance := ui.asm.bar.ScrollDistance(); distance != 0 {
			ui.asm.scroll -= distance * (contentBot - contentTop)
		}
		image.Rect(0, -1000, 1, 1000)
		if distance := ui.asm.gesture.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Vertical,
			pointer.ScrollRange{},
			pointer.ScrollRange{Min: -1000, Max: 1000},
		); distance != 0 {
			ui.asm.scroll -= float32(distance)
		}

		if -ui.asm.scroll < contentTop {
			ui.asm.scroll = -contentTop
			ui.asm.anim.Stop()
		}
		if -ui.asm.scroll+float32(gtx.Constraints.Max.Y) > contentBot {
			if contentBot < float32(gtx.Constraints.Max.Y) {
				ui.asm.scroll = -contentTop
			} else {
				ui.asm.scroll = float32(gtx.Constraints.Max.Y) - contentBot
			}
			ui.asm.anim.Stop()
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
		viewTop := -ui.src.scroll
		viewBot := -ui.src.scroll + float32(gtx.Constraints.Max.Y)

		{
			stack := op.Offset(image.Pt(int(source.Max), 0)).Push(gtx.Ops)
			gtx := gtx
			gtx.Constraints = layout.Exact(image.Pt(pad, gtx.Constraints.Max.Y))
			material.Scrollbar(ui.Theme, &ui.src.bar).Layout(gtx, layout.Vertical,
				(viewTop-contentTop)/(contentBot-contentTop),
				(viewBot-contentTop)/(contentBot-contentTop),
			)
			stack.Pop()
		}

		if distance := ui.src.bar.ScrollDistance(); distance != 0 {
			ui.src.scroll -= distance * (contentBot - contentTop)
		}
		if distance := ui.src.gesture.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Vertical,
			pointer.ScrollRange{},
			pointer.ScrollRange{Min: -1000, Max: 1000},
		); distance != 0 {
			ui.src.scroll -= float32(distance)
		}

		if -ui.src.scroll < contentTop {
			ui.src.scroll = -contentTop
		}
		if -ui.src.scroll+float32(gtx.Constraints.Max.Y) > contentBot {
			if contentBot < float32(gtx.Constraints.Max.Y) {
				ui.src.scroll = -contentTop
			} else {
				ui.src.scroll = float32(gtx.Constraints.Max.Y) - contentBot
			}
		}
		stack.Pop()
	}

	return layout.Dimensions{
		Size: gtx.Constraints.Max,
	}
}
