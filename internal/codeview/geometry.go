package codeview

import (
	"gioui.org/f32"
	"gioui.org/layout"
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

	jump   bounds
	asm    bounds
	native bounds
	gutter bounds
	source bounds

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
	// pad | Jump | pad/2 | Go asm | pad | Native asm | pad | Gutter | pad | Source | pad
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
	blocksWidth := max(0, gtx.Constraints.Max.X-fixedWidth)
	width := func(i int) int {
		if total == 0 {
			return 0
		}
		return blocksWidth * shares[i] / total
	}

	x := pad
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
	gutter := next(gutterWidth, ui.ShowSource, pad)
	source := next(width(2), ui.ShowSource, pad)

	c := codeColumns{
		lineHeight: lineHeight,
		pad:        pad,
		jumpStep:   jumpStep,
		indentStep: lineHeight / 2,
		jump:       jump,
		asm:        asm,
		native:     native,
		gutter:     gutter,
		source:     source,
	}
	minimumCommentWidth := lineHeight * 4
	c.goCol = splitColumn(int(asm.Min)+pad/2, int(asm.Max), pad, minimumCommentWidth, 62)
	c.nativeCol = splitColumn(int(native.Min), int(native.Max), pad, minimumCommentWidth, 62)
	c.sourceCol = splitColumn(int(source.Min), int(source.Max), pad, minimumCommentWidth, 70)

	return c
}
