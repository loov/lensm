package main

import (
	"image"
	"image/color"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// NewVerticalSelectList creates a new select list with the specified item height.
func NewVerticalSelectList(itemHeight unit.Dp) SelectList {
	return SelectList{
		List: widget.List{
			List: layout.List{
				Axis: layout.Vertical,
			},
		},
		ItemHeight: itemHeight,
	}
}

// SelectList draws a list where items can be selected.
type SelectList struct {
	widget.List

	Selected int
	Hovered  int

	ItemHeight unit.Dp

	focused bool
}

// Focused returns true when the list is in focus.
func (list *SelectList) Focused() bool { return list.focused }

// Layout draws the list.
func (list *SelectList) Layout(th *material.Theme, gtx layout.Context, length int, element layout.ListElement) layout.Dimensions {
	return FocusBorder(th, list.focused).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		size := gtx.Constraints.Max
		gtx.Constraints = layout.Exact(size)
		defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()

		key.InputOp{
			Tag: list,
			Keys: key.NameUpArrow + "|" + key.NameDownArrow + "|" +
				key.NameHome + "|" + key.NameEnd + "|" +
				key.NamePageUp + "|" + key.NamePageDown,
		}.Add(gtx.Ops)

		pointer.InputOp{
			Tag:          list,
			Types:        pointer.Press | pointer.Move,
			ScrollBounds: image.Rectangle{},
		}.Add(gtx.Ops)

		changed := false
		grabbed := false

		itemHeight := gtx.Metric.Dp(list.ItemHeight)
		if itemHeight == 0 {
			itemHeight = gtx.Metric.Sp(th.TextSize)
		}

		pointerClicked := false
		pointerHovered := false
		pointerPosition := f32.Point{}
		for _, ev := range gtx.Events(list) {
			switch ev := ev.(type) {
			case key.Event:
				if ev.State == key.Press {
					offset := 0
					switch ev.Name {
					case key.NameHome:
						offset = -length
					case key.NameEnd:
						offset = length
					case key.NameUpArrow:
						offset = -1
					case key.NameDownArrow:
						offset = 1
					case key.NamePageUp:
						offset = -list.List.Position.Count
					case key.NamePageDown:
						offset = list.List.Position.Count
					}

					if offset != 0 {
						target := list.Selected + offset
						if target < 0 {
							target = 0
						}
						if target >= length {
							target = length - 1
						}
						if list.Selected != target {
							list.Selected = target
							changed = true
						}
					}

					// if we get input and don't have a focus, then grab it
					if !list.focused {
						list.focused = true
						op.InvalidateOp{}.Add(gtx.Ops)
					}
				}

			case key.FocusEvent:
				if list.focused != ev.Focus {
					list.focused = ev.Focus
					op.InvalidateOp{}.Add(gtx.Ops)
				}

			case pointer.Event:
				switch ev.Type {
				case pointer.Press:
					if !list.focused && !grabbed {
						grabbed = true
						key.FocusOp{Tag: list}.Add(gtx.Ops)
					}
					pointerClicked = true
					pointerPosition = ev.Position
				case pointer.Move:
					pointerHovered = true
					pointerPosition = ev.Position
				case pointer.Cancel:
					list.Hovered = -1
				}
			}
		}

		if pointerClicked || pointerHovered {
			// TODO: make this independent of fixed item height
			clientClickY := list.Position.First*itemHeight + list.Position.Offset + int(pointerPosition.Y)
			target := clientClickY / itemHeight
			if 0 <= target && target <= length {
				if pointerClicked && list.Selected != target {
					list.Selected = target
				}
				if pointerHovered && list.Hovered != target {
					list.Hovered = target
				}
			}
		}

		if changed {
			pos := &list.List.Position
			switch {
			case list.Selected < pos.First+1:
				list.List.Position = layout.Position{First: list.Selected - 1}
			case pos.First+pos.Count-1 <= list.Selected:
				list.List.Position = layout.Position{First: list.Selected - pos.Count + 2}
			}
		}

		style := material.List(th, &list.List)
		style.AnchorStrategy = material.Overlay
		return style.Layout(gtx, length,
			func(gtx layout.Context, index int) layout.Dimensions {
				gtx.Constraints = layout.Exact(image.Point{
					X: gtx.Constraints.Max.X,
					Y: itemHeight,
				})
				return element(gtx, index)
			})
	})
}

// StringListItem creates a string item drawer that reacts to hover and selection.
func StringListItem(th *material.Theme, state *SelectList, item func(int) string) layout.ListElement {
	return func(gtx layout.Context, index int) layout.Dimensions {
		defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()

		bg := color.NRGBA{}
		fg := th.Fg
		weight := font.Normal

		switch {
		case state.Selected == index:
			if state.Focused() {
				bg = th.ContrastBg
				fg = th.ContrastFg
			}
			weight = font.Black
		case state.Hovered == index:
			bg = th.ContrastBg
			bg.A /= 4
		}

		if bg != (color.NRGBA{}) {
			paint.Fill(gtx.Ops, bg)
		}
		inset := layout.Inset{Top: 1, Right: 4, Bottom: 1, Left: 4}
		return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			label := material.Body1(th, item(index))
			label.Color = fg
			label.MaxLines = 1
			label.TextSize = th.TextSize * 8 / 10
			label.Font.Weight = weight
			gtx.Constraints.Max.X = maxLineWidth
			return label.Layout(gtx)
		})
	}
}
