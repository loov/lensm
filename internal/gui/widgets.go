package gui

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
)

type HorizontalLine struct {
	Height unit.Dp
	Color  color.NRGBA
}

func (line HorizontalLine) Layout(gtx layout.Context) layout.Dimensions {
	size := image.Point{
		X: gtx.Constraints.Min.X,
		Y: gtx.Metric.Dp(line.Height),
	}
	paint.FillShape(gtx.Ops, line.Color, clip.Rect{Max: size}.Op())
	return layout.Dimensions{
		Size: size,
	}
}

const MaxLineWidth = 10 * 1024

// Start draws w at the left edge, centred vertically. layout.W keeps
// the minimum height, so a label would sit at the top instead.
func Start(gtx layout.Context, w layout.Widget) layout.Dimensions {
	return layout.W.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.Y = 0
		return w(gtx)
	})
}
