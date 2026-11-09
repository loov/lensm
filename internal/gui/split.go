package gui

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/x/component"
)

// Split lays out two panes along Axis with a line between them that
// drags to resize them. Ratio is the first pane's share of the room.
type Split struct {
	Axis  layout.Axis
	Ratio float32

	resize component.Resize
}

// Layout draws a and b, each at least minA and minB long where the room
// allows. Ratio is kept as set when the room is too small for it, so a
// small window doesn't lose it; Layout reports whether a drag changed
// it.
func (s *Split) Layout(gtx layout.Context, th *Theme, minA, minB unit.Dp, a, b layout.Widget) (layout.Dimensions, bool) {
	total := float32(s.Axis.Convert(gtx.Constraints.Max).X)
	shown := min(max(s.Ratio, float32(gtx.Dp(minA))/total), 1-float32(gtx.Dp(minB))/total)
	s.resize.Axis, s.resize.Ratio = s.Axis, shown
	dims := s.resize.Layout(gtx, a, b, func(gtx layout.Context) layout.Dimensions {
		return s.handle(gtx, th)
	})
	if s.resize.Ratio == shown {
		return dims, false
	}
	s.Ratio = s.resize.Ratio
	return dims, true
}

// handle is a hairline across the middle of the area that drags.
func (s *Split) handle(gtx layout.Context, th *Theme) layout.Dimensions {
	width := gtx.Dp(8)
	size := s.Axis.Convert(image.Pt(width, s.Axis.Convert(gtx.Constraints.Max).Y))
	line := clip.Rect{
		Min: s.Axis.Convert(image.Pt(width/2, 0)),
		Max: s.Axis.Convert(image.Pt(width/2+1, s.Axis.Convert(size).Y)),
	}
	paint.FillShape(gtx.Ops, th.Colors.Splitter, line.Op())
	return layout.Dimensions{Size: size}
}
