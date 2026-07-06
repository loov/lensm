package gui

import (
	"image/color"

	"gioui.org/unit"
	"gioui.org/widget/material"
	"loov.dev/lensm/internal/syntax"
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
	Background          color.NRGBA
	SecondaryBackground color.NRGBA
	Splitter            color.NRGBA
	Gutter              color.NRGBA
	Text                color.NRGBA
	MutedText           color.NRGBA
	Error               color.NRGBA
	Selection           color.NRGBA
	RelationStroke      color.NRGBA
}

func (c UIColors) SyntaxColors() syntax.Colors {
	return syntax.Colors{Text: c.Text, MutedText: c.MutedText, Background: c.Background}
}

func applyPalette(th *material.Theme, dark bool) UIColors {
	if dark {
		th.Palette = material.Palette{
			Bg:         color.NRGBA{R: 0x11, G: 0x13, B: 0x18, A: 0xff},
			Fg:         color.NRGBA{R: 0xe8, G: 0xea, B: 0xed, A: 0xff},
			ContrastBg: color.NRGBA{R: 0x8a, G: 0xb4, B: 0xf8, A: 0xff},
			ContrastFg: color.NRGBA{R: 0x10, G: 0x23, B: 0x3f, A: 0xff},
		}
		return UIColors{
			Background:          th.Palette.Bg,
			SecondaryBackground: color.NRGBA{R: 0x1b, G: 0x1f, B: 0x27, A: 0xff},
			Splitter:            color.NRGBA{R: 0x4b, G: 0x55, B: 0x63, A: 0xff},
			Gutter:              color.NRGBA{R: 0x18, G: 0x1c, B: 0x22, A: 0xff},
			Text:                th.Palette.Fg,
			MutedText:           color.NRGBA{R: 0xa0, G: 0xa7, B: 0xb5, A: 0xff},
			Error:               color.NRGBA{R: 0xff, G: 0xb4, B: 0xab, A: 0xff},
			Selection:           color.NRGBA{R: 0x26, G: 0x32, B: 0x47, A: 0xff},
			RelationStroke:      color.NRGBA{R: 0xe8, G: 0xea, B: 0xed, A: 0x70},
		}
	}

	th.Palette = material.Palette{
		Bg:         color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
		Fg:         color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xff},
		ContrastBg: color.NRGBA{R: 0x3f, G: 0x51, B: 0xb5, A: 0xff},
		ContrastFg: color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff},
	}
	return UIColors{
		Background:          th.Palette.Bg,
		SecondaryBackground: color.NRGBA{R: 0xf0, G: 0xf0, B: 0xf0, A: 0xff},
		Splitter:            color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff},
		Gutter:              color.NRGBA{R: 0xe8, G: 0xe8, B: 0xe8, A: 0xff},
		Text:                th.Palette.Fg,
		MutedText:           color.NRGBA{R: 0x66, G: 0x66, B: 0x66, A: 0xff},
		Error:               color.NRGBA{R: 0xb0, G: 0x00, B: 0x20, A: 0xff},
		Selection:           color.NRGBA{R: 0xe8, G: 0xf0, B: 0xfe, A: 0xff},
		RelationStroke:      color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x40},
	}
}
