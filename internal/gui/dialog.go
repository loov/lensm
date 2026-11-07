package gui

import (
	"image"

	"gioui.org/font"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
)

// Dialog is a modal window drawn over the scrim: an optional navigation
// column on the left, the page and a footer for the buttons.
type Dialog struct {
	Theme *Theme
	// The window size limits Width and Height.
	Width, Height     unit.Dp
	Nav, Page, Footer layout.Widget
}

// Layout covers the whole of gtx and blocks pointer input to what is
// below. tag is any value unique to the dialog.
func (d Dialog) Layout(gtx layout.Context, tag any) layout.Dimensions {
	c := d.Theme.Colors
	window := gtx.Constraints.Max
	Overlay(gtx, d.Theme, tag)

	margin := gtx.Metric.Dp(24)
	size := image.Pt(
		min(gtx.Metric.Dp(d.Width), window.X-2*margin),
		min(gtx.Metric.Dp(d.Height), window.Y-2*margin),
	)
	if size.X <= 0 || size.Y <= 0 {
		return layout.Dimensions{Size: window}
	}
	defer op.Offset(window.Sub(size).Div(2)).Push(gtx.Ops).Pop()
	radius := gtx.Metric.Dp(12)
	Panel(gtx, d.Theme, size, radius)
	defer clip.UniformRRect(image.Rectangle{Max: size}, radius).Push(gtx.Ops).Pop()

	gtx.Constraints = layout.Exact(size)
	layout.Flex{}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if d.Nav == nil {
				return layout.Dimensions{}
			}
			nav := image.Pt(gtx.Metric.Dp(200), size.Y)
			gtx.Constraints = layout.Exact(nav)
			paint.FillShape(gtx.Ops, c.Sidebar, clip.Rect{Max: nav}.Op())
			paint.FillShape(gtx.Ops, c.Splitter, clip.Rect{Min: image.Pt(nav.X-gtx.Metric.Dp(1), 0), Max: nav}.Op())
			return layout.Inset{Top: 12, Bottom: 12, Left: 8, Right: 9}.Layout(gtx, d.Nav)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min = gtx.Constraints.Max
					return layout.Inset{Top: 18, Bottom: 18, Left: 24, Right: 24}.Layout(gtx, d.Page)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if d.Footer == nil {
						return layout.Dimensions{}
					}
					footer := image.Pt(gtx.Constraints.Max.X, gtx.Metric.Dp(52))
					gtx.Constraints = layout.Exact(footer)
					paint.FillShape(gtx.Ops, c.Divider, clip.Rect{Max: image.Pt(footer.X, gtx.Metric.Dp(1))}.Op())
					return layout.Inset{Left: 18, Right: 18}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.E.Layout(gtx, d.Footer)
					})
				}),
			)
		}),
	)
	return layout.Dimensions{Size: window}
}

// Title is a page heading.
func (th *Theme) Title(text string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		label := th.Label(text, 1.15)
		label.Font.Weight = font.SemiBold
		return layout.Inset{Bottom: 10}.Layout(gtx, label.Layout)
	}
}

// NavList is a vertical list of categories with one selected.
type NavList struct {
	Selected int
	items    []widget.Clickable
}

func (n *NavList) Layout(gtx layout.Context, th *Theme, labels []string) layout.Dimensions {
	for len(n.items) < len(labels) {
		n.items = append(n.items, widget.Clickable{})
	}
	c := th.Colors
	children := make([]layout.FlexChild, len(labels))
	for i, text := range labels {
		click := &n.items[i]
		for click.Clicked(gtx) {
			n.Selected = i
		}
		children[i] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				semantic.Button.Add(gtx.Ops)
				label := th.Label(text, 0.95)
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						rect := image.Rectangle{Max: gtx.Constraints.Min}
						r := gtx.Metric.Dp(6)
						switch {
						case i == n.Selected:
							paint.FillShape(gtx.Ops, c.Accent, clip.UniformRRect(rect, r).Op(gtx.Ops))
						case click.Hovered():
							paint.FillShape(gtx.Ops, c.Hover, clip.UniformRRect(rect, r).Op(gtx.Ops))
						}
						return layout.Dimensions{Size: rect.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						if i == n.Selected {
							label.Color = c.AccentText
						}
						return layout.Inset{Top: 5, Bottom: 5, Left: 10, Right: 10}.Layout(gtx, label.Layout)
					},
				)
			})
		})
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}
