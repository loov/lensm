package codeview

import (
	"math/bits"
	"strconv"

	"gioui.org/f32"
	"gioui.org/layout"

	"loov.dev/lensm/internal/disasm"
)

// codeColumns is the horizontal geometry of the code view: the pixel
// bounds of each column and the derived text/comment sub-regions. It all
// derives from the viewport width, line height, and whether the native
// assembly column is shown.
type codeColumns struct {
	lineHeight int
	pad        int
	jumpStep   int
	// indentStep is the offset per Inst.Indent level.
	indentStep int

	// profile and sourceProfile are the lanes for the profile samples,
	// left of the assembly and of the source, zero width without them.
	profile       bounds
	sourceProfile bounds

	jump   bounds
	asm    bounds
	native bounds
	// marks is the lane for the run time cost marks, right of the
	// assembly. It has zero width when no marks are shown.
	marks  bounds
	gutter bounds
	// sourceMarks is the marks lane at the right edge of the source
	// column, zero width when no source line has a shown mark. The
	// marks draw over the source text, which only lines with marks cut
	// short.
	sourceMarks bounds
	// sourceMarkSlots are the kinds that get a column in sourceMarks.
	// sourceMarkDigits is, per kind in disasm.Marks order, the digits
	// of the largest count on a line, zero without one; the columns of
	// counted kinds are wider to fit it.
	sourceMarkSlots  disasm.Mark
	sourceMarkDigits markDigits
	source           bounds

	// goCol, nativeCol and sourceCol split each column into text and
	// comment regions.
	goCol, nativeCol, sourceCol columnSplit
}

// columnSplit divides a column into a text region and, when there is room,
// a comment region to its right. codeWidth is the text width to use when a
// comment shares the row; commentWidth is 0 when the column is too narrow.
type columnSplit struct {
	textLeft, textWidth       int
	codeWidth                 int
	commentLeft, commentWidth int
}

// splitColumn places the comment region at commentAt percent of the
// column [left, right), keeping pad/2 between text and comment.
func splitColumn(left, right, pad, minCommentWidth, commentAt int) columnSplit {
	s := columnSplit{textLeft: left, textWidth: max(right-left, 0)}
	s.commentLeft = max(left+s.textWidth*commentAt/100, left)
	s.commentWidth = max(right-s.commentLeft, 0)
	s.codeWidth = s.commentLeft - left - pad/2
	if s.codeWidth < 0 || s.commentWidth < minCommentWidth {
		s.codeWidth = s.textWidth
		s.commentWidth = 0
	}
	return s
}

// codeHover is the transient pointer state for one frame: where the mouse
// is and which instruction, if any, it hovers.
type codeHover struct {
	position f32.Point
	inAsm    bool
	inSource bool
	asmIndex int
}

func (ui Style) columns(gtx layout.Context) codeColumns {
	// The layout has the following sections, hidden columns and their
	// padding take no space:
	// pad | Profile | pad/2 | Jump | pad/2 | Go asm | pad | Native asm | pad | Marks | pad | Gutter | pad | Profile | pad/2 | Source | pad
	lineHeight := codeLineHeightPx(gtx, ui.TextHeight)
	pad := lineHeight
	jumpStep := lineHeight / 2
	showAsm := ui.ShowGo || ui.ShowNative
	jumpWidth, gutterWidth := 0, 0
	if showAsm {
		jumpWidth = jumpStep * ui.Code.MaxJump
	}
	if ui.ShowSource {
		gutterWidth = lineHeight * 8
	}
	perRow := 0
	if showAsm {
		perRow = ui.maxMarks()
	}
	showMarks := perRow > 0
	showProfile := ui.profileShown()
	profileWidth := lineHeight * 3
	marksWidth := lineHeight * perRow

	// Each shown column gets its share of the width left after the
	// fixed parts.
	shares := [3]int{}
	for i, show := range [3]bool{ui.ShowGo, ui.ShowNative, ui.ShowSource} {
		if show {
			shares[i] = [3]int{28, 28, 44}[i]
		}
	}
	total, shown := 0, 0
	for _, share := range shares {
		total += share
		if share > 0 {
			shown++
		}
	}
	fixedWidth := 2*pad + max(shown-1, 0)*pad
	if showAsm {
		fixedWidth += jumpWidth + pad/2
	}
	if ui.ShowSource {
		fixedWidth += gutterWidth + pad
	}
	if showMarks {
		fixedWidth += marksWidth + pad
	}
	if showProfile && showAsm {
		fixedWidth += profileWidth + pad/2
	}
	if showProfile && ui.ShowSource {
		fixedWidth += profileWidth + pad/2
	}
	blocksWidth := max(0, gtx.Constraints.Max.X-fixedWidth)
	width := func(i int) int {
		if total == 0 {
			return 0
		}
		return blocksWidth * shares[i] / total
	}

	x := pad
	profile := boundsWidth(x, 0)
	if showProfile && showAsm {
		profile = boundsWidth(x, profileWidth)
		x += profileWidth + pad/2
	}
	jump := boundsWidth(x, jumpWidth)
	if showAsm {
		x += jumpWidth + pad/2
	}
	next := func(w int, show bool, gap int) bounds {
		b := boundsWidth(x, w)
		if show {
			x += w + gap
		}
		return b
	}
	asm := next(width(0), ui.ShowGo, pad)
	native := next(width(1), ui.ShowNative, pad)
	marks := next(marksWidth, showMarks, pad)
	gutter := next(gutterWidth, ui.ShowSource, pad)
	sourceProfile := next(0, false, 0)
	if showProfile && ui.ShowSource {
		sourceProfile = next(profileWidth, true, pad/2)
	}
	source := next(width(2), ui.ShowSource, pad)

	c := codeColumns{
		lineHeight:    lineHeight,
		pad:           pad,
		jumpStep:      jumpStep,
		indentStep:    lineHeight / 2,
		profile:       profile,
		sourceProfile: sourceProfile,
		jump:          jump,
		asm:           asm,
		native:        native,
		marks:         marks,
		gutter:        gutter,
		source:        source,
	}
	minimumCommentWidth := lineHeight * 4
	c.goCol = splitColumn(int(asm.Min)+pad/2, int(asm.Max), pad, minimumCommentWidth, 62)
	c.nativeCol = splitColumn(int(native.Min), int(native.Max), pad, minimumCommentWidth, 62)
	sourceRight := int(source.Max)
	if slots, digits := ui.lineMarkKinds(); ui.ShowSource && slots != 0 {
		c.sourceMarkSlots, c.sourceMarkDigits = slots, digits
		w := 0
		for _, m := range disasm.Marks {
			if slots&m != 0 {
				w += c.markWidth(m)
			}
		}
		c.sourceMarks = boundsWidth(sourceRight-w, w)
	}
	c.sourceCol = splitColumn(int(source.Min), sourceRight, pad, minimumCommentWidth, 70)

	return c
}

// maxMarks is the most shown marks on one instruction.
func (ui Style) maxMarks() int {
	most := 0
	if ui.Marks == 0 {
		return 0
	}
	for _, ix := range ui.Code.Insts {
		if m := ix.Marks & ui.Marks; m != 0 {
			most = max(most, bits.OnesCount8(uint8(m)))
		}
	}
	return most
}

// markDigits counts digits per kind, in disasm.Marks order.
type markDigits [len(disasm.Marks)]int

// lineMarkKinds are the shown kinds found on any source line, and the
// digits of the largest count above one of each.
func (ui Style) lineMarkKinds() (kinds disasm.Mark, digits markDigits) {
	for _, line := range ui.hl.lineMarks {
		kinds |= line.marks
		for i, n := range line.counts {
			if n > 1 && ui.Marks&disasm.Marks[i] != 0 {
				digits[i] = max(digits[i], len(strconv.Itoa(n)))
			}
		}
	}
	return kinds & ui.Marks, digits
}

// sourceMarksLeft is where the first cell of marks starts in the source
// lane, or the right edge of the source column without marks.
func (c codeColumns) sourceMarksLeft(marks disasm.Mark) int {
	x := int(c.sourceMarks.Min)
	for _, m := range disasm.Marks {
		if marks&m != 0 {
			return x
		}
		if c.sourceMarkSlots&m != 0 {
			x += c.markWidth(m)
		}
	}
	return int(c.source.Max)
}

// markWidth is the width of the source lane column for m, a narrow
// cell around the letter and, after it, room for the count's digits.
func (c codeColumns) markWidth(m disasm.Mark) int {
	for i, k := range disasm.Marks {
		if k == m {
			return c.lineHeight*3/4 + c.sourceMarkDigits[i]*c.lineHeight/2
		}
	}
	return c.lineHeight * 3 / 4
}
