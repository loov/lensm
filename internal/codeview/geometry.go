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

	goTextLeft         int
	goInstructionWidth int
	commentLeft        int
	commentWidth       int

	nativeTextLeft         int
	nativeTextWidth        int
	nativeCommentLeft      int
	nativeCommentWidth     int
	nativeInstructionWidth int

	sourceTextLeft     int
	sourceTextWidth    int
	sourceCommentLeft  int
	sourceCommentWidth int
	sourceCodeWidth    int
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

	c.sourceTextLeft = int(source.Min)
	c.sourceTextWidth = max(int(source.Max)-c.sourceTextLeft, 0)
	c.sourceCommentLeft = c.sourceTextLeft + c.sourceTextWidth*70/100
	c.sourceCommentWidth = int(source.Max) - c.sourceCommentLeft
	c.sourceCodeWidth = c.sourceCommentLeft - c.sourceTextLeft - pad/2
	if c.sourceCodeWidth < 0 || c.sourceCommentWidth < minimumCommentWidth {
		c.sourceCodeWidth = c.sourceTextWidth
		c.sourceCommentWidth = 0
	}

	c.goTextLeft = int(asm.Min) + pad/2
	goTextWidth := max(int(asm.Max)-c.goTextLeft, 0)
	c.nativeTextLeft = int(native.Min)
	c.nativeTextWidth = max(int(native.Max)-c.nativeTextLeft, 0)
	c.nativeCommentLeft = c.nativeTextLeft + c.nativeTextWidth*62/100
	c.nativeCommentWidth = int(native.Max) - c.nativeCommentLeft
	c.nativeInstructionWidth = c.nativeCommentLeft - c.nativeTextLeft - pad/2
	if c.nativeInstructionWidth < 0 || c.nativeCommentWidth < minimumCommentWidth {
		c.nativeInstructionWidth = c.nativeTextWidth
		c.nativeCommentWidth = 0
	}
	c.commentLeft = max(c.goTextLeft+goTextWidth*62/100, c.goTextLeft)
	c.commentWidth = max(int(asm.Max)-c.commentLeft, 0)
	c.goInstructionWidth = c.commentLeft - c.goTextLeft - pad/2
	if c.goInstructionWidth < 0 || c.commentWidth < minimumCommentWidth {
		c.goInstructionWidth = goTextWidth
		c.commentWidth = 0
	}

	return c
}
