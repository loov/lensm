package main

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/key"
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

	hl highlightCache

	mousePosition f32.Point
	SelectedAsm   int
	SelectedView  CommentView
	SelectedFile  string
	SelectedLine  int
	Selection     TextSelection

	selecting        bool
	selectionPointer pointer.ID
	selectionStart   f32.Point
	selectionMoved   bool
}

// highlightCache holds the syntax highlight spans for a Code. Layout
// runs every frame; highlighting is a pure function of the immutable
// code and the palette, so it is rebuilt only when either changes —
// re-highlighting per frame allocates a go/scanner per source line.
type highlightCache struct {
	code   *disasm.Code
	syntax SyntaxPalette

	asm        [][]SourceSpan
	nativeText []string
	native     [][]SourceSpan
	// source is indexed by source file, block, and line within the block.
	source [][][][]SourceSpan
}

func (hl *highlightCache) update(code *disasm.Code, syntax SyntaxPalette) {
	if hl.code == code && hl.syntax == syntax {
		return
	}
	hl.code = code
	hl.syntax = syntax

	hl.asm = make([][]SourceSpan, len(code.Insts))
	hl.nativeText = make([]string, len(code.Insts))
	hl.native = make([][]SourceSpan, len(code.Insts))
	for i := range code.Insts {
		ix := &code.Insts[i]
		hl.asm[i] = HighlightAsmLine(ix.Text, ix.Call, syntax)
		hl.nativeText[i] = strings.ToUpper(ix.NativeText)
		hl.native[i] = HighlightAsmLine(hl.nativeText[i], "", syntax)
	}

	hl.source = make([][][][]SourceSpan, len(code.Source))
	for i, src := range code.Source {
		blocks := make([][][]SourceSpan, len(src.Blocks))
		for j, block := range src.Blocks {
			lines := make([][]SourceSpan, len(block.Lines))
			for k, line := range block.Lines {
				lines[k] = HighlightSourceLine(block.From+k, line, syntax)
			}
			blocks[j] = lines
		}
		hl.source[i] = blocks
	}
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

	TryOpen          func(gtx layout.Context, funcname string)
	CommentFor       func(disasm.Inst) string
	NativeCommentFor func(disasm.Inst) string
	SourceCommentFor func(file string, line int) string
	CommentKey       *string
	CommentKeyFor    func(disasm.Inst) string
	SetComment       func(disasm.Inst, string)
	SetNativeComment func(disasm.Inst, string)
	SetSourceComment func(file string, line int, text string)
	CopyText         func(gtx layout.Context, text string)
	CommentEditor    *widget.Editor
	Theme            *material.Theme
	Colors           UIColors
	Syntax           SyntaxPalette

	ShowNative bool
	TextHeight unit.Sp
}

func (ui CodeUIStyle) Layout(gtx layout.Context) layout.Dimensions {
	gtx.Constraints = layout.Exact(gtx.Constraints.Max)
	if ui.Code == nil {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if ui.Colors.Background == (color.NRGBA{}) {
		ui.Colors = ApplyTheme(ui.Theme, false)
	}
	if ui.Syntax.Plain == (color.NRGBA{}) {
		ui.Syntax = SyntaxPaletteFor(SyntaxStyleGoLand, ui.Colors)
	}
	hl := &ui.CodeUI.hl
	hl.update(ui.Code, ui.Syntax)

	paint.FillShape(gtx.Ops, ui.Colors.Background, clip.Rect{Max: gtx.Constraints.Max}.Op())

	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()

	// The layout has the following sections:
	// pad | Jump | pad/2 | Go asm | pad | Native asm | pad | Gutter | pad | Source | pad

	lineHeight := codeLineHeightPx(gtx, ui.TextHeight)
	pad := lineHeight
	jumpStep := lineHeight / 2
	jumpWidth := jumpStep * ui.Code.MaxJump
	gutterWidth := lineHeight * 8
	fixedWidth := gutterWidth + jumpWidth + 4*pad + pad/2
	if ui.ShowNative {
		fixedWidth += pad
	}
	blocksWidth := max(0, gtx.Constraints.Max.X-fixedWidth)

	jump := BoundsWidth(pad, jumpWidth)
	asmWidth := blocksWidth * 40 / 100
	if ui.ShowNative {
		asmWidth = blocksWidth * 28 / 100
	}
	asm := BoundsWidth(int(jump.Max)+pad/2, asmWidth)
	native := BoundsWidth(int(asm.Max), 0)
	gutter := BoundsWidth(int(asm.Max)+pad, gutterWidth)
	sourceWidth := blocksWidth - int(asm.Width())
	if ui.ShowNative {
		native = BoundsWidth(int(asm.Max)+pad, blocksWidth*28/100)
		gutter = BoundsWidth(int(native.Max)+pad, gutterWidth)
		sourceWidth -= int(native.Width())
	}
	source := BoundsWidth(int(gutter.Max)+pad, max(0, sourceWidth))
	sourceTextLeft := int(source.Min)
	sourceTextWidth := int(source.Max) - sourceTextLeft
	if sourceTextWidth < 0 {
		sourceTextWidth = 0
	}
	sourceCommentLeft := sourceTextLeft + sourceTextWidth*70/100
	sourceCommentWidth := int(source.Max) - sourceCommentLeft
	sourceCodeWidth := sourceCommentLeft - sourceTextLeft - pad/2
	if sourceCodeWidth < 0 || sourceCommentWidth < lineHeight*8 {
		sourceCodeWidth = sourceTextWidth
		sourceCommentWidth = 0
	}
	goTextLeft := int(asm.Min) + pad/2
	goTextWidth := int(asm.Max) - goTextLeft
	if goTextWidth < 0 {
		goTextWidth = 0
	}
	nativeTextLeft := int(native.Min)
	nativeTextWidth := int(native.Max) - nativeTextLeft
	if nativeTextWidth < 0 {
		nativeTextWidth = 0
	}
	nativeCommentLeft := nativeTextLeft + nativeTextWidth*62/100
	nativeCommentWidth := int(native.Max) - nativeCommentLeft
	nativeInstructionWidth := nativeCommentLeft - nativeTextLeft - pad/2
	if nativeInstructionWidth < 0 || nativeCommentWidth < lineHeight*8 {
		nativeInstructionWidth = nativeTextWidth
		nativeCommentWidth = 0
	}
	commentLeft := goTextLeft + goTextWidth*62/100
	if commentLeft < goTextLeft {
		commentLeft = goTextLeft
	}
	commentWidth := int(asm.Max) - commentLeft
	if commentWidth < 0 {
		commentWidth = 0
	}
	goInstructionWidth := commentLeft - goTextLeft - pad/2
	if goInstructionWidth < 0 || commentWidth < lineHeight*8 {
		goInstructionWidth = goTextWidth
		commentWidth = 0
	}

	event.Op(gtx.Ops, ui.CodeUI)
	selectionAt := func(position f32.Point) (CodeView, int, bool) {
		if asm.Contains(position.X) {
			relative := position.Y - ui.asm.scroll
			if relative < 0 {
				return CodeViewNone, -1, false
			}
			line := int(relative) / lineHeight
			return CodeViewGoAsm, line, InRange(line, len(ui.Code.Insts))
		}
		if ui.ShowNative && native.Contains(position.X) {
			relative := position.Y - ui.asm.scroll
			if relative < 0 {
				return CodeViewNone, -1, false
			}
			line := int(relative) / lineHeight
			return CodeViewNativeAsm, line, InRange(line, len(ui.Code.Insts))
		}
		if source.Contains(position.X) {
			line := sourceRowAtY(ui.Code, ui.src.scroll, lineHeight, position.Y)
			return CodeViewSource, line, line >= 0
		}
		return CodeViewNone, -1, false
	}
	// selectionDragLine clamps a drag position to the selection view's
	// content, so a fast drag past an edge selects through to the first
	// or last line instead of stopping at the last in-range sample.
	selectionDragLine := func(view CodeView, position f32.Point) (int, bool) {
		var line, count int
		switch view {
		case CodeViewGoAsm, CodeViewNativeAsm:
			count = len(ui.Code.Insts)
			line = int(position.Y-ui.asm.scroll) / lineHeight
		case CodeViewSource:
			count = sourceRowCount(ui.Code)
			line = int(position.Y-ui.src.scroll) / lineHeight
		default:
			return -1, false
		}
		if count == 0 {
			return -1, false
		}
		return min(max(line, 0), count-1), true
	}
	mouseClicked := false
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: ui.CodeUI,
			Kinds:  pointer.Move | pointer.Leave | pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll,
			ScrollY: pointer.ScrollRange{
				Min: int(ui.asm.scroll) - lineHeight,
				Max: len(ui.Code.Insts)*lineHeight + lineHeight - int(ui.asm.scroll),
			},
		})
		if !ok {
			break
		}
		if ev, ok := ev.(pointer.Event); ok {
			switch ev.Kind {
			case pointer.Move:
				ui.mousePosition = ev.Position
			case pointer.Leave:
				if !ui.selecting {
					ui.mousePosition = f32.Pt(-1, -1)
				}
			case pointer.Press:
				ui.mousePosition = ev.Position
				if ev.Buttons.Contain(pointer.ButtonPrimary) {
					view, line, selectable := selectionAt(ev.Position)
					if selectable {
						ui.Selection.Begin(view, line, ev.Modifiers.Contain(key.ModShift))
						ui.selecting = true
						ui.selectionPointer = ev.PointerID
						ui.selectionStart = ev.Position
						ui.selectionMoved = false
						gtx.Execute(pointer.GrabCmd{Tag: ui.CodeUI, ID: ev.PointerID})
						gtx.Execute(key.FocusCmd{Tag: ui.CodeUI})
					} else {
						ui.Selection.Clear()
					}
				}
			case pointer.Drag:
				ui.mousePosition = ev.Position
				if ui.selecting && ev.PointerID == ui.selectionPointer {
					if math.Abs(float64(ev.Position.X-ui.selectionStart.X)) > 3 || math.Abs(float64(ev.Position.Y-ui.selectionStart.Y)) > 3 {
						ui.selectionMoved = true
					}
					if line, ok := selectionDragLine(ui.Selection.View, ev.Position); ok {
						ui.Selection.Extend(ui.Selection.View, line)
					}
				}
			case pointer.Release:
				ui.mousePosition = ev.Position
				if ui.selecting && ev.PointerID == ui.selectionPointer {
					if line, ok := selectionDragLine(ui.Selection.View, ev.Position); ok {
						ui.Selection.Extend(ui.Selection.View, line)
					}
					mouseClicked = !ui.selectionMoved
					ui.selecting = false
				}
			case pointer.Cancel:
				ui.selecting = false
			case pointer.Scroll:
				ui.mousePosition = ev.Position
				switch {
				case asm.Contains(ev.Position.X):
					ui.asm.scroll -= ev.Scroll.Y
				case ui.ShowNative && native.Contains(ev.Position.X):
					ui.asm.scroll -= ev.Scroll.Y
				case source.Contains(ev.Position.X):
					ui.src.scroll -= ev.Scroll.Y
				}
			}
		}
	}
	for {
		ev, ok := gtx.Event(
			key.FocusFilter{Target: ui.CodeUI},
			key.Filter{Focus: ui.CodeUI, Required: key.ModShortcut, Name: key.Name("C")},
			key.Filter{Focus: ui.CodeUI, Required: key.ModShortcut, Name: key.Name("A")},
			key.Filter{Focus: ui.CodeUI, Name: key.NameEscape},
		)
		if !ok {
			break
		}
		keyEvent, ok := ev.(key.Event)
		if !ok || keyEvent.State != key.Press {
			continue
		}
		switch keyEvent.Name {
		case key.Name("C"):
			if text := ui.Selection.Text(ui.Code); text != "" && ui.CopyText != nil {
				ui.CopyText(gtx, text)
			}
		case key.Name("A"):
			view := ui.Selection.View
			if view == CodeViewNone {
				view = CodeViewGoAsm
			}
			lineCount := len(ui.Code.Insts)
			if view == CodeViewSource {
				lineCount = sourceRowCount(ui.Code)
			}
			if lineCount > 0 {
				ui.Selection = TextSelection{View: view, Anchor: 0, Head: lineCount - 1, Active: true}
			}
		case key.NameEscape:
			ui.Selection.Clear()
		}
	}

	// draw gutter
	paint.FillShape(gtx.Ops, ui.Colors.Gutter, clip.Rect{
		Min: image.Pt(int(gutter.Min), 0),
		Max: image.Pt(int(gutter.Max), gtx.Constraints.Max.Y),
	}.Op())

	if scroll, ok := ui.asm.anim.Update(gtx); ok {
		ui.asm.scroll = scroll
	}

	mousePosition := ui.mousePosition
	mouseInAsm := asm.Contains(mousePosition.X) || (ui.ShowNative && native.Contains(mousePosition.X))
	mouseInSource := source.Contains(mousePosition.X)
	if mouseInAsm || mouseInSource {
		pointer.CursorText.Add(gtx.Ops)
	}
	highlightAsmIndex := -1
	if relative := mousePosition.Y - ui.asm.scroll; mouseInAsm && relative >= 0 {
		highlightAsmIndex = int(relative) / lineHeight
	}
	var highlightRanges []disasm.LineRange

	if InRange(highlightAsmIndex, len(ui.Code.Insts)) {
		activateClicked := mouseClicked && ui.SelectedAsm == highlightAsmIndex
		ix := &ui.Code.Insts[highlightAsmIndex]
		callTargetHovered := ui.TryOpen != nil &&
			ix.Call != "" &&
			asm.Contains(mousePosition.X) &&
			mousePosition.X <= float32(goTextLeft+goInstructionWidth) &&
			ui.callTargetHit(gtx, *ix, goTextLeft, mousePosition.X)
		if callTargetHovered {
			pointer.CursorPointer.Add(gtx.Ops)
			if mouseClicked {
				ui.SelectedAsm = highlightAsmIndex
				ui.SelectedView = CommentViewGoAsm
				ui.SelectedFile = ""
				ui.SelectedLine = 0
				ui.TryOpen(gtx, ix.Call)
			}
		} else if mouseClicked && ix.Text != "" {
			// Spacer rows (empty synthetic instructions) have no inline
			// editor; focusing it would swallow subsequent typing.
			ui.SelectedAsm = highlightAsmIndex
			ui.SelectedFile = ""
			ui.SelectedLine = 0
			if ui.ShowNative && native.Contains(mousePosition.X) {
				ui.SelectedView = CommentViewNativeAsm
			} else {
				ui.SelectedView = CommentViewGoAsm
			}
			if ui.CommentEditor != nil {
				gtx.Execute(key.FocusCmd{Tag: ui.CommentEditor})
			}
		}
		if ix.Call == "" && ix.RefOffset != 0 {
			pointer.CursorPointer.Add(gtx.Ops)
			if activateClicked {
				// TODO: smooth scroll
				// highlightAsmIndex -= ix.RefOffset
				ui.asm.anim.Start(gtx, ui.asm.scroll, ui.asm.scroll-float32(ix.RefOffset*lineHeight), 150*time.Millisecond)
			}
		}
	}
	if !InRange(ui.SelectedAsm, len(ui.Code.Insts)) {
		ui.SelectedAsm = -1
	}

	// relations underlay
	top := int(ui.src.scroll)
	var highlightPaths []clip.PathSpec
	relationStroke := ui.Colors.RelationStroke
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
							if float32(r.From*lineHeight)+ui.asm.scroll <= mousePosition.Y && mousePosition.Y < float32(r.To*lineHeight)+ui.asm.scroll {
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

	// assembly
	asmClip := clip.Rect{
		Min: image.Pt(int(jump.Min), 0),
		Max: image.Pt(int(gutter.Min), gtx.Constraints.Max.Y),
	}.Push(gtx.Ops)
	if ui.ShowNative {
		paint.FillShape(gtx.Ops, ui.Colors.Splitter, clip.Rect{
			Min: image.Pt(int(native.Min)-pad/2, 0),
			Max: image.Pt(int(native.Min)-pad/2+1, gtx.Constraints.Max.Y),
		}.Op())
	}
	for i, ix := range ui.Code.Insts {
		if ui.Selection.Contains(CodeViewGoAsm, i) {
			paint.FillShape(gtx.Ops, ui.Colors.Selection, clip.Rect{
				Min: image.Pt(int(asm.Min), i*lineHeight+int(ui.asm.scroll)),
				Max: image.Pt(int(asm.Max), (i+1)*lineHeight+int(ui.asm.scroll)),
			}.Op())
		}
		if ui.ShowNative && ui.Selection.Contains(CodeViewNativeAsm, i) {
			paint.FillShape(gtx.Ops, ui.Colors.Selection, clip.Rect{
				Min: image.Pt(int(native.Min), i*lineHeight+int(ui.asm.scroll)),
				Max: image.Pt(int(native.Max), (i+1)*lineHeight+int(ui.asm.scroll)),
			}.Op())
		}
		if ui.SelectedAsm == i {
			paint.FillShape(gtx.Ops, ui.Colors.Selection, clip.Rect{
				Min: image.Pt(int(asm.Min), i*lineHeight+int(ui.asm.scroll)),
				Max: image.Pt(int(gutter.Min), (i+1)*lineHeight+int(ui.asm.scroll)),
			}.Op())
		}
		SourceLine{
			TopLeft:    image.Pt(goTextLeft, i*lineHeight+int(ui.asm.scroll)),
			Width:      goInstructionWidth,
			Text:       ix.Text,
			Spans:      hl.asm[i],
			TextHeight: ui.TextHeight,
			Italic:     ix.Call != "",
			Bold:       highlightAsmIndex == i || ui.SelectedAsm == i,
			Color:      ui.Syntax.Plain,
		}.Layout(ui.Theme, gtx)
		if commentWidth > 0 && ix.Text != "" {
			comment := ""
			if ui.CommentFor != nil {
				comment = ui.CommentFor(ix)
			}
			if ui.SelectedAsm == i && ui.SelectedView == CommentViewGoAsm {
				ui.layoutInlineCommentEditor(gtx, ix, i*lineHeight+int(ui.asm.scroll), commentLeft, commentWidth, lineHeight)
			} else if comment != "" {
				SourceLine{
					TopLeft:    image.Pt(commentLeft, i*lineHeight+int(ui.asm.scroll)),
					Width:      commentWidth,
					Text:       "; " + comment,
					TextHeight: ui.TextHeight,
					Italic:     true,
					Color:      ui.Colors.MutedText,
				}.Layout(ui.Theme, gtx)
			}
		}
		if ui.ShowNative {
			nativeComment := ""
			if ui.NativeCommentFor != nil {
				nativeComment = ui.NativeCommentFor(ix)
			}
			width := nativeTextWidth
			if (nativeComment != "" || (ui.SelectedAsm == i && ui.SelectedView == CommentViewNativeAsm)) && nativeCommentWidth > 0 {
				width = nativeInstructionWidth
			}
			SourceLine{
				TopLeft:    image.Pt(nativeTextLeft, i*lineHeight+int(ui.asm.scroll)),
				Width:      width,
				Text:       hl.nativeText[i],
				Spans:      hl.native[i],
				TextHeight: ui.TextHeight,
				Bold:       highlightAsmIndex == i || ui.SelectedAsm == i,
				Color:      ui.Syntax.Plain,
			}.Layout(ui.Theme, gtx)
			if ui.SelectedAsm == i && ui.SelectedView == CommentViewNativeAsm && nativeCommentWidth > 0 {
				ui.layoutInlineNativeCommentEditor(gtx, ix, i*lineHeight+int(ui.asm.scroll), nativeCommentLeft, nativeCommentWidth, lineHeight)
			} else if nativeComment != "" && nativeCommentWidth > 0 {
				SourceLine{
					TopLeft:    image.Pt(nativeCommentLeft, i*lineHeight+int(ui.asm.scroll)),
					Width:      nativeCommentWidth,
					Text:       "; " + nativeComment,
					TextHeight: ui.TextHeight,
					Italic:     true,
					Color:      ui.Colors.MutedText,
				}.Layout(ui.Theme, gtx)
			}
		}

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
	sourceRow := 0
	paintSourceSelection := func(row, rowTop int) {
		if ui.Selection.Contains(CodeViewSource, row) {
			paint.FillShape(gtx.Ops, ui.Colors.Selection, clip.Rect{
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
		SourceLine{
			TopLeft:    image.Pt(int(source.Min), top),
			Text:       src.File,
			TextHeight: ui.TextHeight,
			Bold:       highlightAsmIndex == i,
			Color:      ui.Colors.MutedText,
		}.Layout(ui.Theme, gtx)
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
					ui.SelectedView = CommentViewSource
					ui.SelectedFile = src.File
					ui.SelectedLine = lineNo
					if ui.CommentEditor != nil {
						gtx.Execute(key.FocusCmd{Tag: ui.CommentEditor})
					}
				}
				sourceComment := ""
				if ui.SourceCommentFor != nil {
					sourceComment = ui.SourceCommentFor(src.File, lineNo)
				}
				width := sourceTextWidth
				selectedSource := ui.SelectedView == CommentViewSource && ui.SelectedFile == src.File && ui.SelectedLine == lineNo
				if (sourceComment != "" || selectedSource) && sourceCommentWidth > 0 {
					width = sourceCodeWidth
				}
				SourceLine{
					TopLeft:    image.Pt(int(source.Min), top),
					Width:      width,
					Spans:      hl.source[i][blockIndex][off],
					TextHeight: ui.TextHeight,
					Bold:       highlight,
					Color:      ui.Syntax.Plain,
				}.Layout(ui.Theme, gtx)
				if selectedSource && sourceCommentWidth > 0 {
					ui.layoutInlineSourceCommentEditor(gtx, src.File, lineNo, top, sourceCommentLeft, sourceCommentWidth, lineHeight)
				} else if sourceComment != "" && sourceCommentWidth > 0 {
					SourceLine{
						TopLeft:    image.Pt(sourceCommentLeft, top),
						Width:      sourceCommentWidth,
						Text:       "// " + sourceComment,
						TextHeight: ui.TextHeight,
						Italic:     true,
						Color:      ui.Colors.MutedText,
					}.Layout(ui.Theme, gtx)
				}
				top += lineHeight
				sourceRow++
			}
		}
	}
	sourceClip.Pop()
	sourceContentHeight := top - int(ui.src.scroll)

	{
		stack := clip.Rect{
			Min: image.Pt(int(jump.Min)-pad, 0),
			Max: image.Pt(int(gutter.Min), gtx.Constraints.Max.Y),
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

	commentEditing := ui.CommentEditor != nil && gtx.Focused(ui.CommentEditor)
	if !ui.selecting && !commentEditing && InRange(highlightAsmIndex, len(ui.Code.Insts)) {
		inst := ui.Code.Insts[highlightAsmIndex]
		nativeHovered := ui.ShowNative && native.Contains(mousePosition.X)
		var help AssemblyHelp
		var ok bool
		if nativeHovered {
			help, ok = NativeAssemblyInstructionHelp(inst.NativeText)
		} else {
			help, ok = AssemblyInstructionHelp(ui.Code.Arch, inst.Text)
		}
		if ok {
			ui.layoutAssemblyHelp(gtx, help, mousePosition)
		}
	}

	return layout.Dimensions{
		Size: gtx.Constraints.Max,
	}
}

func (ui CodeUIStyle) layoutAssemblyHelp(gtx layout.Context, help AssemblyHelp, position f32.Point) {
	maxWidth := gtx.Metric.Dp(460)
	if maxWidth > gtx.Constraints.Max.X-16 {
		maxWidth = max(0, gtx.Constraints.Max.X-16)
	}
	if maxWidth == 0 {
		return
	}

	contentContext := gtx
	contentContext.Constraints.Min = image.Point{}
	contentContext.Constraints.Max = image.Pt(maxWidth, gtx.Metric.Dp(140))
	macro := op.Record(gtx.Ops)
	dims := layout.UniformInset(8).Layout(contentContext, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme, help.Mnemonic+" — "+help.Description)
				label.Font.Weight = font.Bold
				label.Color = ui.Colors.Text
				label.TextSize = ui.TextHeight * 9 / 10
				return label.Layout(gtx)
			}),
		}
		if help.Explanation != "" {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme, help.Explanation)
				label.Font.Typeface = "override-monospace,Go,monospace"
				label.Color = ui.Syntax.Plain
				label.TextSize = ui.TextHeight * 9 / 10
				return layout.Inset{Top: 5}.Layout(gtx, label.Layout)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
	call := macro.Stop()

	left := int(position.X) + gtx.Metric.Dp(12)
	top := int(position.Y) + gtx.Metric.Dp(18)
	if left+dims.Size.X > gtx.Constraints.Max.X-4 {
		left = gtx.Constraints.Max.X - dims.Size.X - 4
	}
	if top+dims.Size.Y > gtx.Constraints.Max.Y-4 {
		top = int(position.Y) - dims.Size.Y - gtx.Metric.Dp(8)
	}
	left = max(4, left)
	top = max(4, top)

	stack := op.Offset(image.Pt(left, top)).Push(gtx.Ops)
	paint.FillShape(gtx.Ops, ui.Colors.SecondaryBackground, clip.UniformRRect(image.Rectangle{Max: dims.Size}, 5).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, ui.Colors.Splitter, clip.Stroke{
		Path:  clip.UniformRRect(image.Rectangle{Max: dims.Size}, 5).Path(gtx.Ops),
		Width: 1,
	}.Op())
	call.Add(gtx.Ops)
	stack.Pop()
}

func (ui CodeUIStyle) callTargetHit(gtx layout.Context, inst disasm.Inst, left int, x float32) bool {
	if inst.Call == "" {
		return false
	}
	start := strings.Index(inst.Text, inst.Call)
	if start < 0 {
		return false
	}
	// A hovered call line is drawn bold and italic; measure with the
	// same style, otherwise the hitbox drifts from the visible text
	// whenever the fallback font is proportional.
	f := font.Font{Typeface: "override-monospace,Go,monospace", Weight: font.Black, Style: font.Italic}
	end := start + len(inst.Call)
	targetLeft := left + ui.measureAsmTextWidth(gtx, f, inst.Text[:start])
	targetRight := left + ui.measureAsmTextWidth(gtx, f, inst.Text[:end])
	return float32(targetLeft) <= x && x <= float32(targetRight)
}

func (ui CodeUIStyle) measureAsmTextWidth(gtx layout.Context, f font.Font, text string) int {
	if text == "" {
		return 0
	}
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max = image.Pt(maxLineWidth, maxLineWidth)

	macro := op.Record(gtx.Ops)
	dims := widget.Label{MaxLines: 1}.Layout(gtx, ui.Theme.Shaper, f, ui.TextHeight, text, op.CallOp{})
	_ = macro.Stop()
	return dims.Size.X
}

func (ui CodeUIStyle) layoutInlineCommentEditor(gtx layout.Context, inst disasm.Inst, top, left, width, lineHeight int) {
	ui.layoutInlineAsmCommentEditor(gtx, inst, CommentViewGoAsm, ui.CommentFor, ui.CommentKeyFor, ui.SetComment, top, left, width, lineHeight)
}

func (ui CodeUIStyle) layoutInlineNativeCommentEditor(gtx layout.Context, inst disasm.Inst, top, left, width, lineHeight int) {
	keyFor := func(inst disasm.Inst) string {
		if ui.Code == nil {
			return ""
		}
		// The view is prefixed by layoutInlineAsmCommentEditor.
		return ui.Code.Name + ":" + formatPC(inst.PC)
	}
	ui.layoutInlineAsmCommentEditor(gtx, inst, CommentViewNativeAsm, ui.NativeCommentFor, keyFor, ui.SetNativeComment, top, left, width, lineHeight)
}

func (ui CodeUIStyle) layoutInlineAsmCommentEditor(gtx layout.Context, inst disasm.Inst, view CommentView, commentFor func(disasm.Inst) string, keyFor func(disasm.Inst) string, setComment func(disasm.Inst, string), top, left, width, lineHeight int) {
	if ui.CommentEditor == nil || ui.CommentKey == nil || keyFor == nil || setComment == nil {
		if commentFor == nil {
			return
		}
		comment := commentFor(inst)
		if comment == "" {
			return
		}
		SourceLine{
			TopLeft:    image.Pt(left, top),
			Width:      width,
			Text:       "; " + comment,
			TextHeight: ui.TextHeight,
			Italic:     true,
			Color:      ui.Colors.MutedText,
		}.Layout(ui.Theme, gtx)
		return
	}

	key := keyFor(inst)
	if view != "" {
		key = string(view) + ":" + key
	}
	if key != *ui.CommentKey {
		*ui.CommentKey = key
		comment := ""
		if commentFor != nil {
			comment = commentFor(inst)
		}
		ui.CommentEditor.SetText(comment)
	}

	changed := false
	for {
		ev, ok := ui.CommentEditor.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent, widget.SubmitEvent:
			changed = true
		}
	}
	if changed {
		setComment(inst, ui.CommentEditor.Text())
	}

	prefixWidth := lineHeight
	SourceLine{
		TopLeft:    image.Pt(left, top),
		Width:      prefixWidth,
		Text:       ";",
		TextHeight: ui.TextHeight,
		Color:      ui.Colors.MutedText,
	}.Layout(ui.Theme, gtx)

	editorLeft := left + prefixWidth
	editorWidth := width - prefixWidth
	if editorWidth < 0 {
		editorWidth = 0
	}
	stack := op.Offset(image.Pt(editorLeft, top)).Push(gtx.Ops)
	gtx.Constraints = layout.Exact(image.Pt(editorWidth, lineHeight))
	editor := material.Editor(ui.Theme, ui.CommentEditor, "comment")
	editor.TextSize = ui.TextHeight
	editor.Font.Typeface = "override-monospace,Go,monospace"
	editor.Color = ui.Colors.Text
	editor.Layout(gtx)
	stack.Pop()
}

func (ui CodeUIStyle) layoutInlineSourceCommentEditor(gtx layout.Context, file string, line int, top, left, width, lineHeight int) {
	if ui.CommentEditor == nil || ui.CommentKey == nil || ui.SetSourceComment == nil {
		return
	}

	key := string(CommentViewSource) + ":" + ui.Code.Name + ":" + file + ":" + strconv.Itoa(line)
	if key != *ui.CommentKey {
		*ui.CommentKey = key
		comment := ""
		if ui.SourceCommentFor != nil {
			comment = ui.SourceCommentFor(file, line)
		}
		ui.CommentEditor.SetText(comment)
	}

	changed := false
	for {
		ev, ok := ui.CommentEditor.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent, widget.SubmitEvent:
			changed = true
		}
	}
	if changed {
		ui.SetSourceComment(file, line, ui.CommentEditor.Text())
	}

	prefixWidth := lineHeight
	SourceLine{
		TopLeft:    image.Pt(left, top),
		Width:      prefixWidth,
		Text:       "//",
		TextHeight: ui.TextHeight,
		Color:      ui.Colors.MutedText,
	}.Layout(ui.Theme, gtx)

	editorLeft := left + prefixWidth
	editorWidth := width - prefixWidth
	if editorWidth < 0 {
		editorWidth = 0
	}
	stack := op.Offset(image.Pt(editorLeft, top)).Push(gtx.Ops)
	gtx.Constraints = layout.Exact(image.Pt(editorWidth, lineHeight))
	editor := material.Editor(ui.Theme, ui.CommentEditor, "comment")
	editor.TextSize = ui.TextHeight
	editor.Font.Typeface = "override-monospace,Go,monospace"
	editor.Color = ui.Colors.Text
	editor.Layout(gtx)
	stack.Pop()
}
