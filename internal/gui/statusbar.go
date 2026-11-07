package gui

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// StatusBar is the strip along the bottom of the window. Left items
// start at the left edge, Right items end at the right one.
type StatusBar struct {
	Theme *Theme
	Left  []layout.Widget
	Right []layout.Widget
}

func (s StatusBar) Layout(gtx layout.Context) layout.Dimensions {
	c := s.Theme.Colors
	height := gtx.Metric.Dp(24)
	size := image.Pt(gtx.Constraints.Max.X, height)
	gtx.Constraints = layout.Exact(size)
	paint.FillShape(gtx.Ops, c.SecondaryBackground, clip.Rect{Max: size}.Op())
	paint.FillShape(gtx.Ops, c.Splitter, clip.Rect{Max: image.Pt(size.X, gtx.Metric.Dp(1))}.Op())

	row := func(widgets []layout.Widget) layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.Y = 0
			children := make([]layout.FlexChild, 0, len(widgets))
			for i, w := range widgets {
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if i == 0 {
						return w(gtx)
					}
					return layout.Inset{Left: 16}.Layout(gtx, w)
				}))
			}
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
		})
	}
	return layout.Inset{Left: 12, Right: 12, Top: 1}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
			row(s.Left),
			row(s.Right),
		)
	})
}

// StatusText is a label sized for the status bar.
func (th *Theme) StatusText(text string) layout.Widget {
	label := th.Muted(text, 0.88)
	label.MaxLines = 1
	return label.Layout
}

// Dot draws a small filled circle, for example a server state light.
func Dot(c color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		d := gtx.Metric.Dp(7)
		paint.FillShape(gtx.Ops, c, clip.Ellipse{Max: image.Pt(d, d)}.Op(gtx.Ops))
		return layout.Dimensions{Size: image.Pt(d, d)}
	}
}
