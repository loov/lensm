package gui

import (
	"image"
	"image/color"

	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
)

// IconStrip is the narrow column of tool buttons at the window edge.
// Top buttons stack from the top, Bottom ones from the bottom.
type IconStrip struct {
	Theme  *Theme
	Top    []layout.Widget
	Bottom []layout.Widget
}

func (s IconStrip) Layout(gtx layout.Context) layout.Dimensions {
	c := s.Theme.Colors
	size := image.Pt(gtx.Metric.Dp(40), gtx.Constraints.Max.Y)
	gtx.Constraints = layout.Exact(size)
	paint.FillShape(gtx.Ops, c.Sidebar, clip.Rect{Max: size}.Op())
	paint.FillShape(gtx.Ops, c.Splitter, clip.Rect{Min: image.Pt(size.X-gtx.Metric.Dp(1), 0), Max: size}.Op())

	stack := func(widgets []layout.Widget) layout.Widget {
		return func(gtx layout.Context) layout.Dimensions {
			children := make([]layout.FlexChild, len(widgets))
			for i, w := range widgets {
				children[i] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: 2, Bottom: 2}.Layout(gtx, w)
				})
			}
			gtx.Constraints.Min.X = 0
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		}
	}
	return layout.Inset{Top: 6, Bottom: 6, Right: 1}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
			layout.Rigid(stack(s.Top)),
			layout.Rigid(stack(s.Bottom)),
		)
	})
}

// ToolButton is a square icon button for an IconStrip. Selected tints
// it with the accent. With a Tip it shows Description on hover.
type ToolButton struct {
	Theme       *Theme
	Clickable   *widget.Clickable
	Tip         *Tooltip
	Icon        *widget.Icon
	Description string
	Selected    bool
}

func (b ToolButton) Layout(gtx layout.Context) layout.Dimensions {
	if b.Tip != nil {
		return b.Tip.Layout(gtx, b.Theme, b.Description, b.layout)
	}
	return b.layout(gtx)
}

func (b ToolButton) layout(gtx layout.Context) layout.Dimensions {
	c := b.Theme.Colors
	return b.Clickable.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		semantic.Button.Add(gtx.Ops)
		semantic.DescriptionOp(b.Description).Add(gtx.Ops)
		size := image.Pt(gtx.Metric.Dp(30), gtx.Metric.Dp(30))
		var bg color.NRGBA
		fg := c.MutedText
		switch {
		case b.Selected:
			bg, fg = c.Selection, c.Accent
		case b.Clickable.Hovered():
			bg, fg = c.Hover, c.Text
		}
		if bg != (color.NRGBA{}) {
			paint.FillShape(gtx.Ops, bg, clip.UniformRRect(image.Rectangle{Max: size}, gtx.Metric.Dp(6)).Op(gtx.Ops))
		}
		icon := gtx.Metric.Dp(18)
		at := (size.X - icon) / 2
		trans := op.Offset(image.Pt(at, at)).Push(gtx.Ops)
		gtx.Constraints = layout.Exact(image.Pt(icon, icon))
		b.Icon.Layout(gtx, fg)
		trans.Pop()
		return layout.Dimensions{Size: size}
	})
}
