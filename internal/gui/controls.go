package gui

import (
	"image"
	"image/color"

	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// Button is a flat, outlined button. Primary fills it with the accent.
type Button struct {
	Theme     *Theme
	Clickable *widget.Clickable
	Text      string
	Primary   bool
	Disabled  bool
}

func (b Button) Layout(gtx layout.Context) layout.Dimensions {
	c := b.Theme.Colors
	if b.Disabled {
		gtx = gtx.Disabled()
	}
	gtx.Constraints.Min = image.Point{}
	return b.Clickable.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		semantic.Button.Add(gtx.Ops)
		label := b.Theme.Label(b.Text, 0.95)
		label.MaxLines = 1
		bg, border := c.Control, c.Splitter
		switch {
		case b.Disabled:
			label.Color = c.MutedText
		case b.Primary:
			bg, border, label.Color = c.Accent, c.Accent, c.AccentText
		case b.Clickable.Hovered():
			bg = c.Hover
		}
		return frame(gtx, bg, border, layout.Inset{Top: 5, Bottom: 5, Left: 12, Right: 12}, label.Layout)
	})
}

// IconButton is a borderless icon that shows a background on hover.
// With a Tip it shows Description below it on hover.
type IconButton struct {
	Theme       *Theme
	Clickable   *widget.Clickable
	Tip         *Tooltip
	Icon        *widget.Icon
	Description string
	// Disabled dims the icon and ignores clicks.
	Disabled bool
}

func (b IconButton) Layout(gtx layout.Context) layout.Dimensions {
	if b.Tip != nil {
		// The hint shows for a disabled button too, so it is laid out
		// before the button disables gtx.
		b.Tip.Below = true
		tip := b.Tip
		b.Tip = nil
		return tip.Layout(gtx, b.Theme, b.Description, b.Layout)
	}
	c := b.Theme.Colors
	fg := c.MutedText
	if b.Disabled {
		gtx = gtx.Disabled()
		fg.A /= 3
	}
	return b.Clickable.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		semantic.Button.Add(gtx.Ops)
		semantic.DescriptionOp(b.Description).Add(gtx.Ops)
		var bg color.NRGBA
		if b.Clickable.Hovered() && !b.Disabled {
			bg = c.Hover
		}
		return frame(gtx, bg, bg, layout.UniformInset(5), func(gtx layout.Context) layout.Dimensions {
			size := gtx.Metric.Dp(16)
			gtx.Constraints = layout.Exact(image.Pt(size, size))
			return b.Icon.Layout(gtx, fg)
		})
	})
}

// Field outlines an editor, in the accent while it has focus.
func Field(gtx layout.Context, th *Theme, focused bool, w layout.Widget) layout.Dimensions {
	border := th.Colors.Splitter
	if focused {
		border = th.Colors.Accent
	}
	return frame(gtx, th.Colors.Background, border, layout.Inset{Top: 4, Bottom: 4, Left: 6, Right: 6}, w)
}

// SearchField is a filled, rounded field with a search icon in front
// of w. While focused it turns into an outlined field.
func SearchField(gtx layout.Context, th *Theme, focused bool, w layout.Widget) layout.Dimensions {
	c := th.Colors
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			rect := image.Rectangle{Max: gtx.Constraints.Min}
			r := gtx.Metric.Dp(7)
			if focused {
				ring := gtx.Metric.Dp(2)
				paint.FillShape(gtx.Ops, c.Selection, clip.UniformRRect(rect.Inset(-ring), r+ring).Op(gtx.Ops))
				paint.FillShape(gtx.Ops, c.Accent, clip.UniformRRect(rect, r).Op(gtx.Ops))
				b := gtx.Metric.Dp(1)
				paint.FillShape(gtx.Ops, c.Background, clip.UniformRRect(rect.Inset(b), r-b).Op(gtx.Ops))
			} else {
				paint.FillShape(gtx.Ops, c.Fill, clip.UniformRRect(rect, r).Op(gtx.Ops))
			}
			return layout.Dimensions{Size: rect.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 5, Bottom: 5, Left: 8, Right: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Right: 6}.Layout(gtx, iconWidget(SearchIcon, 14, c.MutedText))
					}),
					layout.Flexed(1, w),
				)
			})
		},
	)
}

// frame draws w inside a rounded rectangle with a 1 dp border.
func frame(gtx layout.Context, bg, border color.NRGBA, inset layout.Inset, w layout.Widget) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			size := gtx.Constraints.Min
			r := gtx.Metric.Dp(4)
			paint.FillShape(gtx.Ops, border, clip.UniformRRect(image.Rectangle{Max: size}, r).Op(gtx.Ops))
			b := gtx.Metric.Dp(1)
			paint.FillShape(gtx.Ops, bg, clip.UniformRRect(image.Rect(b, b, size.X-b, size.Y-b), max(r-b, 0)).Op(gtx.Ops))
			return layout.Dimensions{Size: size}
		},
		func(gtx layout.Context) layout.Dimensions {
			return inset.Layout(gtx, w)
		},
	)
}

// Editor returns a text editor in the theme colours.
func Editor(th *Theme, e *widget.Editor, hint string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		editor := material.Editor(th.Theme, e, hint)
		editor.TextSize = th.TextSize * 0.95
		editor.Color = th.Colors.Text
		editor.HintColor = th.Colors.MutedText
		editor.SelectionColor = th.Colors.Selection
		return editor.Layout(gtx)
	}
}
