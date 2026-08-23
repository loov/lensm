package codeview

import (
	"gioui.org/f32"
	"gioui.org/layout"

	"loov.dev/lensm/internal/gui"
)

// codeColumns is the horizontal geometry of the code view: the pixel
// bounds of each column and the derived text/comment sub-regions. It all
// derives from the viewport width, line height, and whether the native
// assembly column is shown.
type codeColumns struct {
	lineHeight int
	pad        int
	jumpStep   int

	jump   gui.Bounds
	asm    gui.Bounds
	native gui.Bounds
	gutter gui.Bounds
	source gui.Bounds

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
	// The layout has the following sections:
	// pad | Jump | pad/2 | Go asm | pad | Native asm | pad | Gutter | pad | Source | pad
	lineHeight := gui.CodeLineHeightPx(gtx, ui.TextHeight)
	pad := lineHeight
	jumpStep := lineHeight / 2
	jumpWidth := jumpStep * ui.Code.MaxJump
	gutterWidth := lineHeight * 8
	fixedWidth := gutterWidth + jumpWidth + 4*pad + pad/2
	if ui.ShowNative {
		fixedWidth += pad
	}
	blocksWidth := max(0, gtx.Constraints.Max.X-fixedWidth)

	jump := gui.BoundsWidth(pad, jumpWidth)
	asmWidth := blocksWidth * 40 / 100
	if ui.ShowNative {
		asmWidth = blocksWidth * 28 / 100
	}
	asm := gui.BoundsWidth(int(jump.Max)+pad/2, asmWidth)
	native := gui.BoundsWidth(int(asm.Max), 0)
	gutter := gui.BoundsWidth(int(asm.Max)+pad, gutterWidth)
	sourceWidth := blocksWidth - int(asm.Width())
	if ui.ShowNative {
		native = gui.BoundsWidth(int(asm.Max)+pad, blocksWidth*28/100)
		gutter = gui.BoundsWidth(int(native.Max)+pad, gutterWidth)
		sourceWidth -= int(native.Width())
	}
	source := gui.BoundsWidth(int(gutter.Max)+pad, max(0, sourceWidth))

	c := codeColumns{
		lineHeight: lineHeight,
		pad:        pad,
		jumpStep:   jumpStep,
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
