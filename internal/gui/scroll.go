package gui

import (
	"image"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// ScrollRegion is the vertical scroll state for a manually drawn
// column: wheel/gesture scrolling, a scrollbar, and animated jumps.
// Offset is the pixel offset of the content top relative to the
// viewport top, so it goes negative as the content scrolls up.
type ScrollRegion struct {
	Offset  float32
	Gesture gesture.Scroll
	Bar     widget.Scrollbar
	Anim    ScrollAnimation
}

// LayoutBar draws a vertical scrollbar at barX with the given width,
// applies scrollbar drags and scroll gestures, and clamps the offset
// so the viewport stays within [contentTop, contentBot].
func (region *ScrollRegion) LayoutBar(gtx layout.Context, th *material.Theme, barX, barWidth int, contentTop, contentBot float32) {
	viewTop := -region.Offset
	viewBot := -region.Offset + float32(gtx.Constraints.Max.Y)

	stack := op.Offset(image.Pt(barX, 0)).Push(gtx.Ops)
	bgtx := gtx
	bgtx.Constraints = layout.Exact(image.Pt(barWidth, gtx.Constraints.Max.Y))
	material.Scrollbar(th, &region.Bar).Layout(bgtx, layout.Vertical,
		(viewTop-contentTop)/(contentBot-contentTop),
		(viewBot-contentTop)/(contentBot-contentTop),
	)
	stack.Pop()

	if distance := region.Bar.ScrollDistance(); distance != 0 {
		region.Offset -= distance * (contentBot - contentTop)
	}
	if distance := region.Gesture.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Vertical,
		pointer.ScrollRange{},
		pointer.ScrollRange{Min: -1000, Max: 1000},
	); distance != 0 {
		region.Offset -= float32(distance)
	}
	region.Clamp(gtx, contentTop, contentBot)
}

// Clamp keeps the viewport within the content, stopping any running
// jump animation when it hits an edge.
func (region *ScrollRegion) Clamp(gtx layout.Context, contentTop, contentBot float32) {
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
