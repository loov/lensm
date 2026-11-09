package gui

import (
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// PanelFrame is a sidebar panel: its title, an optional action right of
// the title, and the content below.
type PanelFrame struct {
	Theme  *Theme
	Title  string
	Action layout.Widget
}

// Layout draws the frame around w.
func (p PanelFrame) Layout(gtx layout.Context, w layout.Widget) layout.Dimensions {
	paint.FillShape(gtx.Ops, p.Theme.Colors.Sidebar, clip.Rect{Max: gtx.Constraints.Max}.Op())
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			label := p.Theme.Muted(p.Title, 0.85)
			label.Font.Weight = font.SemiBold
			return layout.Inset{Top: 12, Bottom: 8, Left: 16, Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
					layout.Rigid(label.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if p.Action == nil {
							return layout.Dimensions{}
						}
						return p.Action(gtx)
					}),
				)
			})
		}),
		layout.Flexed(1, w),
	)
}

// PanelNote is a line of muted text in a panel, such as why it is
// empty.
func PanelNote(th *Theme, text string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: 16, Right: 16}.Layout(gtx, th.Muted(text, 0.9).Layout)
	}
}
