package codeview

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/syntax"
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
	viewHeight := float32(gtx.Constraints.Max.Y)
	var highlightPath clip.PathSpec
	var highlightColor color.NRGBA
	related := 0
	for row := range sourceRows(ui.Code, ui.noteRows()) {
		rowTop := top
		top += lineHeight
		if row.kind != sourceRowLine {
			continue
		}
		block := ui.Code.Source[row.file].Blocks[row.block]
		if row.off >= len(block.Related) || len(block.Related[row.off]) == 0 {
			continue
		}
		ranges := block.Related[row.off]
		// Each line keeps its hue while scrolling, so count every
		// related line, drawn or not.
		related++

		highlight := mouseInSource && float32(rowTop) <= mousePosition.Y && mousePosition.Y < float32(rowTop+lineHeight)
		minY, maxY := float32(rowTop), float32(rowTop+lineHeight)
		for _, r := range ranges {
			from := float32(r.From*lineHeight) + ui.asm.Offset
			to := float32(r.To*lineHeight) + ui.asm.Offset
			if mouseInAsm && from <= mousePosition.Y && mousePosition.Y < to {
				highlight = true
			}
			minY, maxY = min(minY, from), max(maxY, to)
		}
		if maxY < 0 || minY > viewHeight || ui.HoverLinksOnly && !highlight {
			continue
		}

		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(gutter.Max, float32(rowTop+lineHeight)))
		p.LineTo(f32.Pt(source.Max, float32(rowTop+lineHeight)))
		p.LineTo(f32.Pt(source.Max, float32(rowTop)))
		p.LineTo(f32.Pt(gutter.Max, float32(rowTop)))
		pin := float32(rowTop)
		for i, r := range ranges {
			const S = 0.1
			p.CubeTo(
				f32.Pt(gutter.Lerp(0.5-S), pin),
				f32.Pt(gutter.Lerp(0.5+S), float32(r.From*lineHeight)+ui.asm.Offset),
				f32.Pt(gutter.Min, float32(r.From*lineHeight)+ui.asm.Offset))
			p.LineTo(f32.Pt(asm.Min, float32(r.From*lineHeight)+ui.asm.Offset))
			p.LineTo(f32.Pt(asm.Min, float32(r.To*lineHeight)+ui.asm.Offset))
			p.LineTo(f32.Pt(gutter.Min, float32(r.To*lineHeight)+ui.asm.Offset))
			pin = float32(rowTop) + float32(lineHeight)*float32(i+1)/float32(len(ranges))
			p.CubeTo(
				f32.Pt(gutter.Lerp(0.5+S), float32(r.To*lineHeight)+ui.asm.Offset),
				f32.Pt(gutter.Lerp(0.5-S), pin),
				f32.Pt(gutter.Max, pin))
		}
		path := p.End()

		hue := float32(math.Mod(float64(related)*math.Phi, 1))
		c := ui.Theme.Colors.Relation
		if !highlight {
			paint.FillShape(gtx.Ops, hsla(hue, c.Saturation, c.Lightness, c.Alpha), clip.Outline{Path: path}.Op())
			continue
		}
		// The hovered band is drawn last so it stays on top.
		highlightRanges = ranges
		highlightPath = path
		highlightColor = hsla(hue, c.Saturation, c.Lightness, min(2*c.Alpha, 1))
	}
	if highlightRanges != nil {
		paint.FillShape(gtx.Ops, highlightColor, clip.Outline{Path: highlightPath}.Op())
		paint.FillShape(gtx.Ops, ui.Theme.Colors.Relation.Stroke, clip.Stroke{Path: highlightPath, Width: 1}.Op())
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
	var tip markTip
	defer func() { ui.layoutMarkTip(gtx, c, tip) }()

	asmClip := clip.Rect{
		Min: image.Pt(int(jump.Min), 0),
		Max: image.Pt(int(gutter.Min), gtx.Constraints.Max.Y),
	}.Push(gtx.Ops)
	if ui.ShowGo && ui.ShowNative {
		paint.FillShape(gtx.Ops, ui.Theme.Colors.Splitter, clip.Rect{
			Min: image.Pt(int(native.Min)-pad/2, 0),
			Max: image.Pt(int(native.Min)-pad/2+1, gtx.Constraints.Max.Y),
		}.Op())
	}
	// Only rows on screen are laid out: shaping every row of a 2800
	// instruction function took 65 ms a frame, the visible ones 7 ms.
	viewHeight := gtx.Constraints.Max.Y
	first := max(0, -int(ui.asm.Offset)/lineHeight)
	last := min(len(ui.Code.Insts), first+viewHeight/lineHeight+2)
	for i := first; i < last; i++ {
		ix := ui.Code.Insts[i]
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
		indent := ix.Indent * c.indentStep
		if ui.ShowGo {
			sourceLine{
				TopLeft:    image.Pt(c.goCol.textLeft+indent, i*lineHeight+int(ui.asm.Offset)),
				Width:      max(c.goCol.codeWidth-indent, 0),
				Text:       ix.Text,
				Spans:      hl.asm[i],
				TextHeight: ui.TextHeight,
				Italic:     ix.Call != "",
				Bold:       highlightAsmIndex == i || ui.SelectedAsm == i,
				Color:      ui.Syntax.Plain,
			}.Layout(ui.Theme.Theme, gtx)
		}
		if ui.ShowGo && ix.Text != "" {
			ui.layoutRowComment(gtx, ui.asmCoord(ViewGoAsm, ix), ";", c.goCol, i*lineHeight+int(ui.asm.Offset), lineHeight,
				ui.SelectedAsm == i && ui.SelectedView == ViewGoAsm)
		}
		if marks := ix.Marks & ui.Marks; marks != 0 && c.marks.Width() > 0 {
			if t := ui.layoutMarks(gtx, c, int(c.marks.Min), marks, 0, nil, nil, i*lineHeight+int(ui.asm.Offset), hover.position); t.text != "" {
				tip = t
			}
		}
		if ui.ShowNative {
			width := ui.layoutRowComment(gtx, ui.asmCoord(ViewNativeAsm, ix), ";", c.nativeCol, i*lineHeight+int(ui.asm.Offset), lineHeight,
				ui.SelectedAsm == i && ui.SelectedView == ViewNativeAsm)
			sourceLine{
				TopLeft:    image.Pt(c.nativeCol.textLeft, i*lineHeight+int(ui.asm.Offset)),
				Width:      width,
				Text:       hl.nativeText[i],
				Spans:      hl.native[i],
				TextHeight: ui.TextHeight,
				Bold:       highlightAsmIndex == i || ui.SelectedAsm == i,
				Color:      ui.Syntax.Plain,
			}.Layout(ui.Theme.Theme, gtx)
		}
	}

	// Jump lines span rows, so any whose span crosses the view is drawn.
	for i, ix := range ui.Code.Insts {
		if ix.RefOffset == 0 {
			continue
		}
		from, to := min(i, i+ix.RefOffset), max(i, i+ix.RefOffset)
		if (to+1)*lineHeight+int(ui.asm.Offset) < 0 || from*lineHeight+int(ui.asm.Offset) > viewHeight {
			continue
		}
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
		jumpColor := hsla(float32(math.Mod(float64(ix.PC)*math.Phi, 1)), 0.8, 0.4, alpha)
		paint.FillShape(gtx.Ops, jumpColor, clip.Stroke{Path: path.End(), Width: width}.Op())

		stack.Pop()
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
	viewHeight := gtx.Constraints.Max.Y
	// flow is the hovered note, whose explanation pops up.
	var flow *disasm.Note
	var flowLeft, flowTop int
	var tip markTip
	for row := range sourceRows(ui.Code, ui.noteRows()) {
		rowTop := top
		top += lineHeight
		sourceRow++
		// Rows off screen still count towards the content height.
		if rowTop+lineHeight < 0 || rowTop > viewHeight {
			continue
		}
		paintSourceSelection(sourceRow-1, rowTop)
		switch row.kind {
		case sourceRowHeader:
			sourceLine{
				TopLeft:    image.Pt(int(source.Min), rowTop),
				Text:       row.text,
				TextHeight: ui.TextHeight,
				Color:      ui.Theme.Colors.MutedText,
			}.Layout(ui.Theme.Theme, gtx)
		case sourceRowNote:
			left := ui.layoutNote(gtx, c, row, rowTop)
			if mouseInSource && float32(rowTop) <= mousePosition.Y && mousePosition.Y < float32(rowTop+lineHeight) {
				flow, flowLeft, flowTop = row.note, left, rowTop
			}
		case sourceRowLine:
			src := ui.Code.Source[row.file]
			lineNo := src.Blocks[row.block].From + row.off
			highlight := mouseInSource && float32(rowTop) <= mousePosition.Y && mousePosition.Y < float32(rowTop+lineHeight)
			if highlight && mouseClicked {
				ui.SelectedAsm = -1
				ui.SelectedView = ViewSource
				ui.SelectedFile = src.File
				ui.SelectedLine = lineNo
				if ui.doubleClicked(gtx, fmt.Sprint(src.File, lineNo)) {
					ui.editComment(gtx)
				}
			}
			selectedSource := ui.SelectedView == ViewSource && ui.SelectedFile == src.File && ui.SelectedLine == lineNo
			// The text and the comment stop where this line's marks start.
			line := hl.lineMarks[sourcePos{src.File, lineNo}]
			var marks disasm.Mark
			if line != nil && c.sourceMarks.Width() > 0 {
				marks = line.marks & ui.Marks
			}
			right := c.sourceMarksLeft(marks)
			col := c.sourceCol
			col.commentWidth = min(col.commentWidth, right-col.commentLeft)
			width := ui.layoutRowComment(gtx, ui.sourceCoord(src.File, lineNo), "//", col, rowTop, lineHeight, selectedSource)
			width = min(width, right-int(source.Min))
			if marks != 0 {
				if t := ui.layoutMarks(gtx, c, int(c.sourceMarks.Min), marks, c.sourceMarkSlots, c.markWidth, &line.counts, rowTop, mousePosition); t.text != "" {
					tip = t
				}
			}
			sourceLine{
				TopLeft:    image.Pt(int(source.Min), rowTop),
				Width:      width,
				Spans:      hl.source[row.file][row.block][row.off],
				TextHeight: ui.TextHeight,
				Bold:       highlight,
				Color:      ui.Syntax.Plain,
			}.Layout(ui.Theme.Theme, gtx)
		}
	}
	sourceClip.Pop()
	if flow != nil {
		ui.layoutNoteFlow(gtx, c, flow, flowLeft, flowTop)
	}
	ui.layoutMarkTip(gtx, c, tip)
	return top - int(ui.src.Offset)
}

// layoutNote draws one row of a compiler note below its line. The
// first row starts under the column the note is about, with a
// connector from the line above; it returns where the text starts.
func (ui Style) layoutNote(gtx layout.Context, c codeColumns, row sourceRow, top int) int {
	x := int(c.source.Min) + row.noteX
	arm := c.lineHeight / 2
	color := ui.noteColor(row.note)
	if row.noteFirst {
		stroke := max(gtx.Dp(1), 1)
		mid := top + c.lineHeight/2
		paint.FillShape(gtx.Ops, color, clip.Rect{Min: image.Pt(x, top), Max: image.Pt(x+stroke, mid+stroke)}.Op())
		paint.FillShape(gtx.Ops, color, clip.Rect{Min: image.Pt(x, mid), Max: image.Pt(x+arm, mid+stroke)}.Op())
	}
	left := x + noteGap(c)
	sourceLine{
		TopLeft:    image.Pt(left, top),
		Width:      int(c.source.Max) - left,
		Text:       row.noteText,
		TextHeight: ui.TextHeight * noteScale,
		Italic:     true,
		Color:      color,
	}.Layout(ui.Theme.Theme, gtx)
	return left
}

// flowStep is one step of a note's explanation and where it happens.
type flowStep struct{ at, text string }

// noteFlow is the explanation of a note, such as how a value escapes.
func noteFlow(note *disasm.Note) []flowStep {
	var steps []flowStep
	for _, r := range note.Related {
		// An inlineLoc names where the previous step was inlined from.
		if r.Message == "inlineLoc" {
			continue
		}
		text := strings.TrimPrefix(r.Message, "escflow:")
		text = strings.TrimPrefix(text, "    ")
		steps = append(steps, flowStep{fmt.Sprintf("%s:%d  ", filepath.Base(r.File), r.Line), text})
	}
	return steps
}

// layoutNoteFlow shows the explanation of the hovered note in a popup
// below its row, or above when there is no room below.
func (ui Style) layoutNoteFlow(gtx layout.Context, c codeColumns, note *disasm.Note, left, top int) {
	steps := noteFlow(note)
	if len(steps) == 0 {
		return
	}
	colors := ui.Theme.Colors
	pad := gtx.Dp(8)
	lines := make([]sourceLine, len(steps))
	width := 0
	for i, step := range steps {
		// A flow names a step, the rows under it say why.
		color := ui.noteColor(note)
		if strings.HasPrefix(step.text, "flow:") {
			color = colors.Text
		}
		lines[i] = sourceLine{
			TextHeight: ui.TextHeight * noteScale,
			Spans: []syntax.Span{
				{Text: step.at, Color: colors.MutedText},
				{Text: step.text, Color: color},
			},
		}
		width = max(width, glyphWidth(gtx, ui.Theme.Theme, sourceLine{Text: step.at + step.text, TextHeight: lines[i].TextHeight}))
	}
	size := image.Pt(width+2*pad, len(steps)*c.lineHeight+2*pad)
	pos := image.Pt(left, top+c.lineHeight)
	pos.X = max(min(pos.X, gtx.Constraints.Max.X-size.X), 0)
	if pos.Y+size.Y > gtx.Constraints.Max.Y {
		pos.Y = max(top-size.Y, 0)
	}

	macro := op.Record(gtx.Ops)
	trans := op.Offset(pos).Push(gtx.Ops)
	gui.Panel(gtx, ui.Theme, size, gtx.Dp(5))
	for i, line := range lines {
		line.TopLeft = image.Pt(pad, pad+i*c.lineHeight)
		line.Layout(ui.Theme.Theme, gtx)
	}
	trans.Pop()
	op.Defer(gtx.Ops, macro.Stop())
}

// noteColor ties a note to the cost mark of the same kind.
func (ui Style) noteColor(note *disasm.Note) color.NRGBA {
	colors := ui.Theme.Colors
	if note.Good() {
		return colors.Marks.Good
	}
	switch note.Kind() {
	case disasm.NoteEscape, disasm.NoteLeak:
		return colors.Marks.Alloc
	case disasm.NoteBounds:
		return colors.Marks.BoundsCheck
	case disasm.NoteCopy:
		return colors.Marks.Spill
	}
	return colors.MutedText
}

// layoutScrollbars draws and services the assembly and source scrollbars,
// clamping each column's scroll offset to its content.
func (ui Style) layoutScrollbars(gtx layout.Context, c codeColumns, sourceContentHeight int) {
	lineHeight := c.lineHeight
	pad := c.pad
	jump, gutter, source := c.jump, c.gutter, c.source

	overflow := float32(lineHeight)

	if ui.ShowGo || ui.ShowNative {
		stack := clip.Rect{
			Min: image.Pt(int(jump.Min)-pad, 0),
			Max: image.Pt(int(gutter.Min), gtx.Constraints.Max.Y),
		}.Push(gtx.Ops)
		ui.asm.LayoutBar(gtx, ui.Theme, int(jump.Min)-pad, pad,
			-overflow, float32(len(ui.Code.Insts)*lineHeight)+overflow)
		stack.Pop()
	}

	if ui.ShowSource {
		stack := clip.Rect{
			Min: image.Pt(int(source.Min), 0),
			Max: image.Pt(int(source.Max)+pad, gtx.Constraints.Max.Y),
		}.Push(gtx.Ops)
		ui.src.LayoutBar(gtx, ui.Theme, int(source.Max), pad,
			-overflow, float32(sourceContentHeight)+overflow)
		stack.Pop()
	}
}

// layoutMarks draws one cell per mark from x on, each with the mark's
// letter and, when counts has more than one of it, the count. Each
// kind in slots keeps its own column, empty when the row lacks it;
// with no slots the cells pack together. widthOf gives the width of
// a cell, a square when nil. It returns the tip of the cell under
// pointer, if any.
func (ui Style) layoutMarks(gtx layout.Context, c codeColumns, x int, marks, slots disasm.Mark, widthOf func(disasm.Mark) int, counts *markCounts, top int, pointer f32.Point) markTip {
	var tip markTip
	height := c.lineHeight
	inset := height / 10
	for i, m := range disasm.Marks {
		insetX := inset
		width := height
		if widthOf != nil {
			width = widthOf(m)
		}
		if marks&m == 0 {
			if slots&m != 0 {
				x += width
			}
			continue
		}
		col := MarkColor(ui.Theme.Colors.Marks, m)
		bg := col
		bg.A = 0x28
		if width < height {
			insetX = height / 20 // a narrow source cell
		}
		rect := image.Rect(x+insetX, top+inset, x+width-insetX, top+height-inset)
		if image.Pt(int(pointer.X), int(pointer.Y)).In(image.Rect(x, top, x+width, top+height)) {
			n := 1
			if counts != nil {
				n = counts[i]
			}
			tip = markTip{text: markTipText(m, n), chip: rect, color: col}
		}
		paint.FillShape(gtx.Ops, bg, clip.UniformRRect(rect, rect.Dy()/4).Op(gtx.Ops))

		// Measure the letter and count to centre them in the cell.
		letter := sourceLine{Text: MarkGlyph(m), TextHeight: ui.TextHeight, Italic: true, Color: col}
		total := glyphWidth(gtx, ui.Theme.Theme, letter)
		var count sourceLine
		if counts != nil && counts[i] > 1 {
			count = sourceLine{Text: strconv.Itoa(counts[i]), TextHeight: ui.TextHeight * 0.85, Color: col}
			total += glyphWidth(gtx, ui.Theme.Theme, count)
		}
		left := x + (width-total)/2
		letter.TopLeft = image.Pt(left, top)
		letter.Layout(ui.Theme.Theme, gtx)
		if count.Text != "" {
			// Share the baseline with the letter.
			drop := codeLineHeightPx(gtx, letter.TextHeight) - codeLineHeightPx(gtx, count.TextHeight)
			count.TopLeft = image.Pt(left+glyphWidth(gtx, ui.Theme.Theme, letter), top+drop*3/4)
			count.Layout(ui.Theme.Theme, gtx)
		}
		x += width
	}
	return tip
}

// markTip explains the mark chip under the pointer.
type markTip struct {
	text  string
	chip  image.Rectangle
	color color.NRGBA
}

// markDescriptions say what each mark costs.
var markDescriptions = map[disasm.Mark]string{
	disasm.MarkSpill:        "spill: stores a register on the stack",
	disasm.MarkReload:       "reload: loads a register back from the stack",
	disasm.MarkBoundsCheck:  "bounds check: panics on an index out of range",
	disasm.MarkAlloc:        "allocation: calls the heap allocator",
	disasm.MarkWriteBarrier: "write barrier: tells the garbage collector about a pointer write",
}

// markTipText describes n marks m on one row.
func markTipText(m disasm.Mark, n int) string {
	if n > 1 {
		return fmt.Sprintf("%d× %s", n, markDescriptions[m])
	}
	return markDescriptions[m]
}

// layoutMarkTip draws tip in a popup below its chip, or above when
// there is no room below.
func (ui Style) layoutMarkTip(gtx layout.Context, c codeColumns, tip markTip) {
	if tip.text == "" {
		return
	}
	pad := gtx.Dp(6)
	line := sourceLine{Text: tip.text, TextHeight: ui.TextHeight * noteScale, Color: ui.Theme.Colors.Text}
	size := image.Pt(glyphWidth(gtx, ui.Theme.Theme, line)+2*pad, c.lineHeight+pad)
	pos := image.Pt(tip.chip.Min.X, tip.chip.Max.Y+gtx.Dp(2))
	pos.X = max(min(pos.X, gtx.Constraints.Max.X-size.X), 0)
	if pos.Y+size.Y > gtx.Constraints.Max.Y {
		pos.Y = max(tip.chip.Min.Y-size.Y-gtx.Dp(2), 0)
	}
	macro := op.Record(gtx.Ops)
	trans := op.Offset(pos).Push(gtx.Ops)
	gui.Panel(gtx, ui.Theme, size, gtx.Dp(5))
	line.TopLeft = image.Pt(pad, pad/2)
	line.Layout(ui.Theme.Theme, gtx)
	trans.Pop()
	op.Defer(gtx.Ops, macro.Stop())
}

// glyphWidth measures the width of a short line of text.
func glyphWidth(gtx layout.Context, th *material.Theme, line sourceLine) int {
	macro := op.Record(gtx.Ops)
	defer macro.Stop()
	gtx.Constraints = layout.Constraints{Max: image.Pt(gui.MaxLineWidth, gui.MaxLineWidth)}
	return layoutSourceSpan(th, gtx, line, syntax.Span{Text: line.Text}, 0).Size.X
}

// markGlyphs are the letters drawn for each mark.
var markGlyphs = map[disasm.Mark]string{
	disasm.MarkSpill:        "S",
	disasm.MarkReload:       "R",
	disasm.MarkBoundsCheck:  "B",
	disasm.MarkAlloc:        "A",
	disasm.MarkWriteBarrier: "W",
}

// MarkGlyph is the letter drawn for a single mark.
func MarkGlyph(m disasm.Mark) string {
	return markGlyphs[m]
}

// MarkColor is the colour of a single mark.
func MarkColor(colors gui.MarkColors, m disasm.Mark) color.NRGBA {
	switch m {
	case disasm.MarkSpill:
		return colors.Spill
	case disasm.MarkReload:
		return colors.Reload
	case disasm.MarkBoundsCheck:
		return colors.BoundsCheck
	case disasm.MarkAlloc:
		return colors.Alloc
	}
	return colors.WriteBarrier
}
