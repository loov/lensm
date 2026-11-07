package gui

import (
	"image"

	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// ClickList is a scrolling list of two-line rows that react to clicks,
// for panels such as comments and history.
type ClickList struct {
	list widget.List
	rows []widget.Clickable
}

// Clicked returns the index of a clicked row. Call it before Layout.
func (l *ClickList) Clicked(gtx layout.Context) (int, bool) {
	for i := range l.rows {
		if l.rows[i].Clicked(gtx) {
			return i, true
		}
	}
	return 0, false
}

// Layout draws n rows; row returns the title and the muted detail line
// of row i. The selected row, if any, is drawn in the accent.
func (l *ClickList) Layout(gtx layout.Context, th *Theme, n, selected int, row func(i int) (title, detail string)) layout.Dimensions {
	for len(l.rows) < n {
		l.rows = append(l.rows, widget.Clickable{})
	}
	l.list.Axis = layout.Vertical
	c := th.Colors
	style := material.List(th.Theme, &l.list)
	th.StyleScrollbar(&style.ScrollbarStyle)
	style.AnchorStrategy = material.Overlay
	return style.Layout(gtx, n, func(gtx layout.Context, i int) layout.Dimensions {
		title, detail := row(i)
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		click := &l.rows[i]
		return layout.Inset{Left: 8, Right: 8, Bottom: 2}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				semantic.Button.Add(gtx.Ops)
				fg, muted := c.Text, c.MutedText
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						rect := image.Rectangle{Max: gtx.Constraints.Min}
						r := gtx.Metric.Dp(6)
						switch {
						case i == selected:
							paint.FillShape(gtx.Ops, c.Accent, clip.UniformRRect(rect, r).Op(gtx.Ops))
						case click.Hovered():
							paint.FillShape(gtx.Ops, c.Hover, clip.UniformRRect(rect, r).Op(gtx.Ops))
						}
						return layout.Dimensions{Size: rect.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						if i == selected {
							fg, muted = c.AccentText, c.AccentText
						}
						return layout.Inset{Top: 4, Bottom: 4, Left: 8, Right: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									label := th.Label(title, 0.92)
									label.Color = fg
									label.MaxLines = 1
									return label.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if detail == "" {
										return layout.Dimensions{}
									}
									label := th.Label(detail, 0.85)
									label.Color = muted
									label.MaxLines = 2
									return label.Layout(gtx)
								}),
							)
						})
					},
				)
			})
		})
	})
}
