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
	// around it: toolbar, tab strip and status bar. Sidebar is the
	// icon strip and the panel next to it.
	Background          color.NRGBA
	SecondaryBackground color.NRGBA
	Sidebar             color.NRGBA
	// Splitter separates panes; Divider is the quieter line inside one.
	Splitter  color.NRGBA
	Divider   color.NRGBA
	Gutter    color.NRGBA
	Text      color.NRGBA
	MutedText color.NRGBA
	Error     color.NRGBA
	// Hover is the background of a row or button under the pointer.
	// It is translucent so it works on every surface.
	Hover color.NRGBA
	// Fill is the background of search fields and segmented controls.
	Fill color.NRGBA
	// Control is the face of buttons, pop-up buttons and the chosen
	// segment, raised above Fill.
	Control   color.NRGBA
	Selection color.NRGBA
	// Accent marks the selected row, the active tab and focused fields;
	// AccentText is text on top of it.
	Accent     color.NRGBA
	AccentText color.NRGBA
	// Positive is a switch that is on and a server that is running.
	Positive color.NRGBA
	// Shadow is the outline and drop shadow of popups and dialogs.
	Shadow   color.NRGBA
	Scrim    color.NRGBA
	Relation RelationColors
}

// RelationColors styles the bands linking source lines to their
// assembly. Each line gets its own hue at this saturation and lightness;
// the hovered band doubles Alpha and gets the Stroke outline.
type RelationColors struct {
	Saturation, Lightness, Alpha float32
	Stroke                       color.NRGBA
}

func rgb(v uint32) color.NRGBA {
	return color.NRGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
}

func rgba(v uint32, a uint8) color.NRGBA {
	c := rgb(v)
	c.A = a
	return c
}

// applyPalette follows the macOS system colours, in light and dark.
func applyPalette(th *material.Theme, dark bool) UIColors {
	var c UIColors
	if dark {
		c = UIColors{
			Background:          rgb(0x1e1e1e),
			SecondaryBackground: rgb(0x2a2a2c),
			Sidebar:             rgb(0x252527),
			Splitter:            rgb(0x3a3a3c),
			Divider:             rgb(0x313133),
			Gutter:              rgb(0x222224),
			Text:                rgb(0xe5e5e7),
			MutedText:           rgb(0x98989d),
			Error:               rgb(0xff6961),
			Hover:               rgba(0xffffff, 0x14),
			Fill:                rgba(0xffffff, 0x14),
			Control:             rgb(0x4a4a4d),
			Selection:           rgba(0x0a84ff, 0x48),
			Accent:              rgb(0x0a84ff),
			AccentText:          rgb(0xffffff),
			Positive:            rgb(0x30d158),
			Shadow:              rgba(0x000000, 0x80),
			Scrim:               rgba(0x000000, 0x60),
			Relation: RelationColors{
				Saturation: 0.55, Lightness: 0.5, Alpha: 0.05,
				Stroke: rgba(0x0a84ff, 0xa0),
			},
		}
	} else {
		c = UIColors{
			Background:          rgb(0xffffff),
			SecondaryBackground: rgb(0xf6f5f8),
			Sidebar:             rgb(0xecebf0),
			Splitter:            rgb(0xd9d8dd),
			Divider:             rgb(0xe6e5ea),
			Gutter:              rgb(0xfafafa),
			Text:                rgb(0x1d1d1f),
			MutedText:           rgb(0x86868b),
			Error:               rgb(0xd70015),
			Hover:               rgba(0x000000, 0x0d),
			Fill:                rgba(0x000000, 0x0f),
			Control:             rgb(0xffffff),
			Selection:           rgba(0x007aff, 0x24),
			Accent:              rgb(0x007aff),
			AccentText:          rgb(0xffffff),
			Positive:            rgb(0x34c759),
			Shadow:              rgba(0x000000, 0x38),
			Scrim:               rgba(0x000000, 0x2e),
			Relation: RelationColors{
				Saturation: 0.85, Lightness: 0.62, Alpha: 0.16,
				Stroke: rgba(0x007aff, 0x8c),
			},
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
