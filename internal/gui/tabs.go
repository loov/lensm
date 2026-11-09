package gui

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// Tabs is a strip of closable tabs that scrolls when they don't fit.
type Tabs struct {
	List widget.List
}

// Tab is one tab of a strip: its clickable areas and its label, in
// italics for a tab that the next one opened replaces.
type Tab struct {
	Tab, Close *widget.Clickable
	Label      string
	Italic     bool
}

// Layout draws tabs with active selected. The caller reads the clicks
// on each Tab and Close.
func (t *Tabs) Layout(gtx layout.Context, th *Theme, tabs []Tab, active int) layout.Dimensions {
	if len(tabs) == 0 {
		return layout.Dimensions{}
	}
	colors := th.Colors
	// 32 dp at the default 12 sp, growing with the text size.
	height := gtx.Metric.Sp(th.TextSize) + gtx.Metric.Dp(20)
	width := gtx.Constraints.Max.X
	gtx.Constraints = layout.Exact(image.Pt(width, height))
	paint.FillShape(gtx.Ops, colors.SecondaryBackground, clip.Rect{Max: gtx.Constraints.Max}.Op())
	paint.FillShape(gtx.Ops, colors.Splitter, clip.Rect{Min: image.Pt(0, height-gtx.Metric.Dp(1)), Max: gtx.Constraints.Max}.Op())

	tabWidth := gtx.Metric.Dp(220)
	minTabWidth := gtx.Metric.Dp(120)
	if len(tabs) <= 3 && width > 0 {
		tabWidth = min(tabWidth, max(minTabWidth, width/len(tabs)))
	}
	tabWidth = max(tabWidth, minTabWidth)

	t.List.Axis = layout.Horizontal
	list := material.List(th.Theme, &t.List)
	list.AnchorStrategy = material.Overlay
	th.StyleScrollbar(&list.ScrollbarStyle)
	list.Indicator.MinorWidth = 3
	return list.Layout(gtx, len(tabs), func(gtx layout.Context, i int) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(tabWidth, height))
		return layoutTab(gtx, th, tabs[i], i == active)
	})
}

func layoutTab(gtx layout.Context, th *Theme, tab Tab, active bool) layout.Dimensions {
	colors := th.Colors
	size := gtx.Constraints.Max
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()

	hovered := tab.Tab.Hovered() || tab.Close.Hovered()
	bg := colors.SecondaryBackground
	if active {
		bg = colors.Background
	} else if hovered {
		bg = colors.Hover
	}
	paint.FillShape(gtx.Ops, bg, clip.Rect{Max: size}.Op())
	if active {
		paint.FillShape(gtx.Ops, colors.Accent, clip.Rect{Max: image.Pt(size.X, gtx.Metric.Dp(2))}.Op())
	} else {
		// Inactive tabs sit on the strip's bottom border.
		paint.FillShape(gtx.Ops, colors.Splitter, clip.Rect{Min: image.Pt(0, size.Y-gtx.Metric.Dp(1)), Max: size}.Op())
	}
	paint.FillShape(gtx.Ops, colors.Divider, clip.Rect{Min: image.Pt(size.X-1, 0), Max: size}.Op())

	layout.Inset{Right: 6, Left: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(gtx.Constraints.Max)
				return tab.Tab.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					size := gtx.Constraints.Max
					defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()

					label := th.Label(tab.Label, 0.92)
					label.MaxLines = 1
					if !active {
						label.Color = colors.MutedText
					}
					if tab.Italic {
						label.Font.Style = font.Italic
					}
					dims := Start(gtx, label.Layout)
					return layout.Dimensions{Size: size, Baseline: dims.Baseline}
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				// The close button shows on the active and hovered tabs
				// only, but keeps its space so names don't shift.
				side := gtx.Metric.Dp(20)
				gtx.Constraints = layout.Exact(image.Pt(side, gtx.Constraints.Max.Y))
				return tab.Close.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					size := gtx.Constraints.Max
					if !active && !hovered {
						return layout.Dimensions{Size: size}
					}
					box := image.Rectangle{Min: image.Pt(0, (size.Y-side)/2), Max: image.Pt(side, (size.Y+side)/2)}
					if tab.Close.Hovered() {
						paint.FillShape(gtx.Ops, colors.Hover, clip.UniformRRect(box, gtx.Metric.Dp(3)).Op(gtx.Ops))
					}
					icon := gtx.Metric.Dp(12)
					defer op.Offset(image.Pt((side-icon)/2, (size.Y-icon)/2)).Push(gtx.Ops).Pop()
					gtx.Constraints = layout.Exact(image.Pt(icon, icon))
					CloseIcon.Layout(gtx, colors.MutedText)
					return layout.Dimensions{Size: size}
				})
			}),
		)
	})
	return layout.Dimensions{Size: size}
}
