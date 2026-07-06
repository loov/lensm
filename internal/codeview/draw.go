package codeview

import (
	"image"
	"math"

	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/f32color"
	"loov.dev/lensm/internal/gui"
)

// layoutRelations draws the curved bands linking source lines to the
// assembly ranges they compiled to, and returns the assembly ranges under
// the pointer so the jump lines can highlight them.
func (ui Style) layoutRelations(gtx layout.Context, c codeColumns, hover codeHover) []disasm.LineRange {
	lineHeight := c.lineHeight
	gutter, source, asm := c.gutter, c.source, c.asm
	mousePosition := hover.position
	mouseInAsm, mouseInSource := hover.inAsm, hover.inSource

	var highlightRanges []disasm.LineRange
	top := int(ui.src.Offset)
	var highlightPaths []clip.PathSpec
	relationStroke := ui.Theme.Colors.RelationStroke
	relationFill := relationStroke
	relationFill.A /= 2
	for i, src := range ui.Code.Source {
		if i > 0 {
			top += lineHeight
		}
		top += lineHeight
		for i, block := range src.Blocks {
			if i > 0 {
				top += lineHeight
			}
			for _, ranges := range block.Related {
				if len(ranges) > 0 {
					highlight := mouseInSource && float32(top) <= mousePosition.Y && mousePosition.Y < float32(top+lineHeight)
					if !highlight && mouseInAsm {
						for _, r := range ranges {
							if float32(r.From*lineHeight)+ui.asm.Offset <= mousePosition.Y && mousePosition.Y < float32(r.To*lineHeight)+ui.asm.Offset {
								highlight = true
								break
							}
						}
					}
					if highlight {
						highlightRanges = ranges

						var p clip.Path
						p.Begin(gtx.Ops)
						p.MoveTo(f32.Pt(gutter.Max, float32(top+lineHeight)))
						p.LineTo(f32.Pt(source.Max, float32(top+lineHeight)))
						p.LineTo(f32.Pt(source.Max, float32(top)))
						p.LineTo(f32.Pt(gutter.Max, float32(top)))
						pin := float32(top)
						for i, r := range ranges {
							const S = 0.1
							p.CubeTo(
								f32.Pt(gutter.Lerp(0.5-S), pin),
								f32.Pt(gutter.Lerp(0.5+S), float32(r.From*lineHeight)+ui.asm.Offset),
								f32.Pt(gutter.Min, float32(r.From*lineHeight)+ui.asm.Offset))
							p.LineTo(f32.Pt(asm.Min, float32(r.From*lineHeight)+ui.asm.Offset))
							p.LineTo(f32.Pt(asm.Min, float32(r.To*lineHeight)+ui.asm.Offset))
							p.LineTo(f32.Pt(gutter.Min, float32(r.To*lineHeight)+ui.asm.Offset))
							pin = float32(top) + float32(lineHeight)*float32(i+1)/float32(len(ranges))
							p.CubeTo(
								f32.Pt(gutter.Lerp(0.5+S), float32(r.To*lineHeight)+ui.asm.Offset),
								f32.Pt(gutter.Lerp(0.5-S), pin),
								f32.Pt(gutter.Max, pin))
						}
						highlightPaths = append(highlightPaths, p.End())
					}
				}
				top += lineHeight
			}
		}
	}
	for _, path := range highlightPaths {
		paint.FillShape(gtx.Ops, relationFill, clip.Outline{Path: path}.Op())
		paint.FillShape(gtx.Ops, relationStroke, clip.Stroke{Path: path, Width: 1}.Op())
	}
	return highlightRanges
}

// layoutAssembly draws the Go and native assembly columns: selection
// highlights, instruction text, inline comments, and jump lines.
func (ui Style) layoutAssembly(gtx layout.Context, c codeColumns, hover codeHover, highlightRanges []disasm.LineRange) {
	hl := &ui.UI.hl
	lineHeight := c.lineHeight
	pad, jumpStep := c.pad, c.jumpStep
	jump, asm, native, gutter := c.jump, c.asm, c.native, c.gutter
	highlightAsmIndex := hover.asmIndex

	asmClip := clip.Rect{
		Min: image.Pt(int(jump.Min), 0),
		Max: image.Pt(int(gutter.Min), gtx.Constraints.Max.Y),
	}.Push(gtx.Ops)
	if ui.ShowNative {
		paint.FillShape(gtx.Ops, ui.Theme.Colors.Splitter, clip.Rect{
			Min: image.Pt(int(native.Min)-pad/2, 0),
			Max: image.Pt(int(native.Min)-pad/2+1, gtx.Constraints.Max.Y),
		}.Op())
	}
	for i, ix := range ui.Code.Insts {
		if ui.Selection.Contains(ViewGoAsm, i) {
			paint.FillShape(gtx.Ops, ui.Theme.Colors.Selection, clip.Rect{
				Min: image.Pt(int(asm.Min), i*lineHeight+int(ui.asm.Offset)),
				Max: image.Pt(int(asm.Max), (i+1)*lineHeight+int(ui.asm.Offset)),
			}.Op())
		}
		if ui.ShowNative && ui.Selection.Contains(ViewNativeAsm, i) {
			paint.FillShape(gtx.Ops, ui.Theme.Colors.Selection, clip.Rect{
				Min: image.Pt(int(native.Min), i*lineHeight+int(ui.asm.Offset)),
				Max: image.Pt(int(native.Max), (i+1)*lineHeight+int(ui.asm.Offset)),
			}.Op())
		}
		if ui.SelectedAsm == i {
			paint.FillShape(gtx.Ops, ui.Theme.Colors.Selection, clip.Rect{
				Min: image.Pt(int(asm.Min), i*lineHeight+int(ui.asm.Offset)),
				Max: image.Pt(int(gutter.Min), (i+1)*lineHeight+int(ui.asm.Offset)),
			}.Op())
		}
		gui.SourceLine{
			TopLeft:    image.Pt(c.goTextLeft, i*lineHeight+int(ui.asm.Offset)),
			Width:      c.goInstructionWidth,
			Text:       ix.Text,
			Spans:      hl.asm[i],
			TextHeight: ui.TextHeight,
			Italic:     ix.Call != "",
			Bold:       highlightAsmIndex == i || ui.SelectedAsm == i,
			Color:      ui.Syntax.Plain,
		}.Layout(ui.Theme.Theme, gtx)
		if c.commentWidth > 0 && ix.Text != "" {
			comment := ui.Comments.Get(ui.asmCoord(ViewGoAsm, ix))
			if ui.SelectedAsm == i && ui.SelectedView == ViewGoAsm {
				ui.layoutInlineCommentEditor(gtx, ui.asmCoord(ViewGoAsm, ix), ";", i*lineHeight+int(ui.asm.Offset), c.commentLeft, c.commentWidth, lineHeight)
			} else if comment != "" {
				gui.SourceLine{
					TopLeft:    image.Pt(c.commentLeft, i*lineHeight+int(ui.asm.Offset)),
					Width:      c.commentWidth,
					Text:       "; " + comment,
					TextHeight: ui.TextHeight,
					Italic:     true,
					Color:      ui.Theme.Colors.MutedText,
				}.Layout(ui.Theme.Theme, gtx)
			}
		}
		if ui.ShowNative {
			nativeComment := ui.Comments.Get(ui.asmCoord(ViewNativeAsm, ix))
			width := c.nativeTextWidth
			if (nativeComment != "" || (ui.SelectedAsm == i && ui.SelectedView == ViewNativeAsm)) && c.nativeCommentWidth > 0 {
				width = c.nativeInstructionWidth
			}
			gui.SourceLine{
				TopLeft:    image.Pt(c.nativeTextLeft, i*lineHeight+int(ui.asm.Offset)),
				Width:      width,
				Text:       hl.nativeText[i],
				Spans:      hl.native[i],
				TextHeight: ui.TextHeight,
				Bold:       highlightAsmIndex == i || ui.SelectedAsm == i,
				Color:      ui.Syntax.Plain,
			}.Layout(ui.Theme.Theme, gtx)
			if ui.SelectedAsm == i && ui.SelectedView == ViewNativeAsm && c.nativeCommentWidth > 0 {
				ui.layoutInlineCommentEditor(gtx, ui.asmCoord(ViewNativeAsm, ix), ";", i*lineHeight+int(ui.asm.Offset), c.nativeCommentLeft, c.nativeCommentWidth, lineHeight)
			} else if nativeComment != "" && c.nativeCommentWidth > 0 {
				gui.SourceLine{
					TopLeft:    image.Pt(c.nativeCommentLeft, i*lineHeight+int(ui.asm.Offset)),
					Width:      c.nativeCommentWidth,
					Text:       "; " + nativeComment,
					TextHeight: ui.TextHeight,
					Italic:     true,
					Color:      ui.Theme.Colors.MutedText,
				}.Layout(ui.Theme.Theme, gtx)
			}
		}

		// jump line
		if ix.RefOffset != 0 {
			lineWidth := gtx.Metric.Dp(1)
			align := float32(lineWidth%2) / 2
			stack := op.Affine(f32.Affine2D{}.Offset(
				f32.Pt(jump.Max+align, float32(i*lineHeight)+align+ui.asm.Offset))).Push(gtx.Ops)

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
}

// layoutSource draws the source column: file headers, source lines,
// selection highlights, and inline comments. It returns the total pixel
// height of the source content for the scrollbar.
func (ui Style) layoutSource(gtx layout.Context, c codeColumns, hover codeHover, mouseClicked bool) int {
	hl := &ui.UI.hl
	lineHeight := c.lineHeight
	source := c.source
	mousePosition := hover.position
	mouseInSource := hover.inSource

	sourceClip := clip.Rect{
		Min: image.Pt(int(source.Min), 0),
		Max: image.Pt(int(source.Max), gtx.Constraints.Max.Y),
	}.Push(gtx.Ops)
	top := int(ui.src.Offset)
	sourceRow := 0
	paintSourceSelection := func(row, rowTop int) {
		if ui.Selection.Contains(ViewSource, row) {
			paint.FillShape(gtx.Ops, ui.Theme.Colors.Selection, clip.Rect{
				Min: image.Pt(int(source.Min), rowTop),
				Max: image.Pt(int(source.Max), rowTop+lineHeight),
			}.Op())
		}
	}
	for i, src := range ui.Code.Source {
		if i > 0 {
			paintSourceSelection(sourceRow, top)
			top += lineHeight
			sourceRow++
		}
		paintSourceSelection(sourceRow, top)
		gui.SourceLine{
			TopLeft:    image.Pt(int(source.Min), top),
			Text:       src.File,
			TextHeight: ui.TextHeight,
			Bold:       hover.asmIndex == i,
			Color:      ui.Theme.Colors.MutedText,
		}.Layout(ui.Theme.Theme, gtx)
		top += lineHeight
		sourceRow++
		for blockIndex, block := range src.Blocks {
			if blockIndex > 0 {
				paintSourceSelection(sourceRow, top)
				top += lineHeight
				sourceRow++
			}
			for off := range block.Lines {
				paintSourceSelection(sourceRow, top)
				highlight := mouseInSource && float32(top) <= mousePosition.Y && mousePosition.Y < float32(top+lineHeight)
				lineNo := block.From + off
				if highlight && mouseClicked {
					ui.SelectedAsm = -1
					ui.SelectedView = ViewSource
					ui.SelectedFile = src.File
					ui.SelectedLine = lineNo
					if ui.CommentEditor != nil {
						gtx.Execute(key.FocusCmd{Tag: ui.CommentEditor})
					}
				}
				sourceComment := ui.Comments.Get(ui.sourceCoord(src.File, lineNo))
				width := c.sourceTextWidth
				selectedSource := ui.SelectedView == ViewSource && ui.SelectedFile == src.File && ui.SelectedLine == lineNo
				if (sourceComment != "" || selectedSource) && c.sourceCommentWidth > 0 {
					width = c.sourceCodeWidth
				}
				gui.SourceLine{
					TopLeft:    image.Pt(int(source.Min), top),
					Width:      width,
					Spans:      hl.source[i][blockIndex][off],
					TextHeight: ui.TextHeight,
					Bold:       highlight,
					Color:      ui.Syntax.Plain,
				}.Layout(ui.Theme.Theme, gtx)
				if selectedSource && c.sourceCommentWidth > 0 {
					ui.layoutInlineCommentEditor(gtx, ui.sourceCoord(src.File, lineNo), "//", top, c.sourceCommentLeft, c.sourceCommentWidth, lineHeight)
				} else if sourceComment != "" && c.sourceCommentWidth > 0 {
					gui.SourceLine{
						TopLeft:    image.Pt(c.sourceCommentLeft, top),
						Width:      c.sourceCommentWidth,
						Text:       "// " + sourceComment,
						TextHeight: ui.TextHeight,
						Italic:     true,
						Color:      ui.Theme.Colors.MutedText,
					}.Layout(ui.Theme.Theme, gtx)
				}
				top += lineHeight
				sourceRow++
			}
		}
	}
	sourceClip.Pop()
	return top - int(ui.src.Offset)
}

// layoutScrollbars draws and services the assembly and source scrollbars,
// clamping each column's scroll offset to its content.
func (ui Style) layoutScrollbars(gtx layout.Context, c codeColumns, sourceContentHeight int) {
	lineHeight := c.lineHeight
	pad := c.pad
	jump, gutter, source := c.jump, c.gutter, c.source

	overflow := float32(lineHeight)

	{
		stack := clip.Rect{
			Min: image.Pt(int(jump.Min)-pad, 0),
			Max: image.Pt(int(gutter.Min), gtx.Constraints.Max.Y),
		}.Push(gtx.Ops)
		ui.asm.LayoutBar(gtx, ui.Theme.Theme, int(jump.Min)-pad, pad,
			-overflow, float32(len(ui.Code.Insts)*lineHeight)+overflow)
		stack.Pop()
	}

	{
		stack := clip.Rect{
			Min: image.Pt(int(source.Min), 0),
			Max: image.Pt(int(source.Max)+pad, gtx.Constraints.Max.Y),
		}.Push(gtx.Ops)
		ui.src.LayoutBar(gtx, ui.Theme.Theme, int(source.Max), pad,
			-overflow, float32(sourceContentHeight)+overflow)
		stack.Pop()
	}
}
