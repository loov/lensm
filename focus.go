package main

import (
	"image/color"

	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// FocusBorderStyle implements a styling a focused widget.
type FocusBorderStyle struct {
	Focused     bool
	BorderWidth unit.Dp
	Color       color.NRGBA
}

// FocusBorder creates a focus border for a focused widget.
func FocusBorder(th *material.Theme, focused bool) FocusBorderStyle {
	return FocusBorderStyle{
		Focused:     focused,
		BorderWidth: unit.Dp(2),
		Color:       th.ContrastBg,
	}
}

// Layout adds a focus border and styling.
func (focus FocusBorderStyle) Layout(gtx layout.Context, w layout.Widget) layout.Dimensions {
	inset := layout.UniformInset(focus.BorderWidth)
	if !focus.Focused {
		return inset.Layout(gtx, w)
	}

	return widget.Border{
		Color: focus.Color,
		Width: focus.BorderWidth,
	}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return inset.Layout(gtx, w)
	})
}
