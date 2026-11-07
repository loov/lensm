package codeview

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/gui"
)

// scrollRegion is the vertical scroll state for a manually drawn
// column: a scrollbar and animated jumps. The owner applies wheel
// scrolling to Offset.
// Offset is the pixel offset of the content top relative to the
// viewport top, so it goes negative as the content scrolls up.
type scrollRegion struct {
	Offset float32
	Bar    widget.Scrollbar
	Anim   scrollAnimation
}

// LayoutBar draws a vertical scrollbar at barX with the given width,
// applies scrollbar drags, and clamps the offset
// so the viewport stays within [contentTop, contentBot].
func (region *scrollRegion) LayoutBar(gtx layout.Context, th *gui.Theme, barX, barWidth int, contentTop, contentBot float32) {
	viewTop := -region.Offset
	viewBot := -region.Offset + float32(gtx.Constraints.Max.Y)

	stack := op.Offset(image.Pt(barX, 0)).Push(gtx.Ops)
	bgtx := gtx
	bgtx.Constraints = layout.Exact(image.Pt(barWidth, gtx.Constraints.Max.Y))
	bar := material.Scrollbar(th.Theme, &region.Bar)
	th.StyleScrollbar(&bar)
	bar.Layout(bgtx, layout.Vertical,
		(viewTop-contentTop)/(contentBot-contentTop),
		(viewBot-contentTop)/(contentBot-contentTop),
	)
	stack.Pop()

	if distance := region.Bar.ScrollDistance(); distance != 0 {
		region.Offset -= distance * (contentBot - contentTop)
	}
	region.Clamp(gtx, contentTop, contentBot)
}

// Clamp keeps the viewport within the content, stopping any running
// jump animation when it hits an edge.
func (region *scrollRegion) Clamp(gtx layout.Context, contentTop, contentBot float32) {
	if -region.Offset < contentTop {
		region.Offset = -contentTop
		region.Anim.Stop()
	}
	if -region.Offset+float32(gtx.Constraints.Max.Y) > contentBot {
		if contentBot < float32(gtx.Constraints.Max.Y) {
			region.Offset = -contentTop
		} else {
			region.Offset = float32(gtx.Constraints.Max.Y) - contentBot
		}
		region.Anim.Stop()
	}
}
