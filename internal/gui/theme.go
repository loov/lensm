package gui

import (
	"image/color"

	"gioui.org/unit"
	"gioui.org/widget/material"
)

// Theme bundles the material theme with the app palette so layout
// code takes one argument. Colors is recomputed by SetDark, not per
// frame.
type Theme struct {
	*material.Theme
	Colors UIColors
}

func NewTheme(base *material.Theme, dark bool) *Theme {
	th := &Theme{Theme: base}
	th.SetDark(dark)
	return th
}

// SetDark switches the material palette and app colors between the
// dark and light schemes.
func (th *Theme) SetDark(dark bool) {
	th.Colors = applyPalette(th.Theme, dark)
}

// Label returns a body label in the app text color, scaled relative
// to the theme text size.
func (th *Theme) Label(text string, scale float32) material.LabelStyle {
	label := material.Body1(th.Theme, text)
	label.TextSize = th.TextSize * unit.Sp(scale)
	label.Color = th.Colors.Text
	return label
}

// Muted is Label in the muted text color.
func (th *Theme) Muted(text string, scale float32) material.LabelStyle {
	label := th.Label(text, scale)
	label.Color = th.Colors.MutedText
	return label
}

// ErrorLabel is Label in the error color.
func (th *Theme) ErrorLabel(text string, scale float32) material.LabelStyle {
	label := th.Label(text, scale)
	label.Color = th.Colors.Error
	return label
}

type UIColors struct {
	// Background is the editor surface, SecondaryBackground the chrome
	// around it: toolbar, sidebar and tab strip.
	Background          color.NRGBA
	SecondaryBackground color.NRGBA
	// Splitter separates panes and outlines fields.
	Splitter  color.NRGBA
	Gutter    color.NRGBA
	Text      color.NRGBA
	MutedText color.NRGBA
	Error     color.NRGBA
	// Hover is the background of a row or button under the pointer.
	Hover     color.NRGBA
	Selection color.NRGBA
	// Accent marks the focused selection, the active tab and focused
	// fields; AccentText is text on top of it.
	Accent         color.NRGBA
	AccentText     color.NRGBA
	RelationStroke color.NRGBA
	// RelationLightness is the HSL lightness of the bands linking source
	// lines to their assembly; each line gets its own hue.
	RelationLightness float32
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func applyPalette(th *material.Theme, dark bool) UIColors {
	var c UIColors
	if dark {
		c = UIColors{
			Background:          rgb(0x1e1f22),
			SecondaryBackground: rgb(0x2b2d30),
			Splitter:            rgb(0x3c3f43),
			Gutter:              rgb(0x232427),
			Text:                rgb(0xdfe1e5),
			MutedText:           rgb(0x8c9097),
			Error:               rgb(0xf2827a),
			Hover:               rgb(0x36383c),
			Selection:           rgb(0x2e436e),
			Accent:              rgb(0x3574f0),
			AccentText:          rgb(0xffffff),
			RelationStroke:      color.NRGBA{R: 0xdf, G: 0xe1, B: 0xe5, A: 0x60},
			RelationLightness:   0.3,
		}
	} else {
		c = UIColors{
			Background:          rgb(0xffffff),
			SecondaryBackground: rgb(0xf7f8fa),
			Splitter:            rgb(0xdfe1e5),
			Gutter:              rgb(0xf2f3f5),
			Text:                rgb(0x1e1f22),
			MutedText:           rgb(0x6c707e),
			Error:               rgb(0xc4232f),
			Hover:               rgb(0xebecf0),
			Selection:           rgb(0xd5e1ff),
			Accent:              rgb(0x3574f0),
			AccentText:          rgb(0xffffff),
			RelationStroke:      color.NRGBA{A: 0x40},
			RelationLightness:   0.8,
		}
	}
	th.Palette = material.Palette{
		Bg:         c.Background,
		Fg:         c.Text,
		ContrastBg: c.Accent,
		ContrastFg: c.AccentText,
	}
	return c
}

// StyleScrollbar gives a scrollbar a quiet indicator that darkens on
// hover, instead of material's near-black one.
func (th *Theme) StyleScrollbar(s *material.ScrollbarStyle) {
	s.Indicator.MajorMinLen = 24
	s.Indicator.Color = th.Colors.MutedText
	s.Indicator.Color.A = 0x50
	s.Indicator.HoverColor = th.Colors.MutedText
	s.Indicator.HoverColor.A = 0xa0
}
