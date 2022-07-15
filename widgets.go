package main

import (
	"image"
	"image/color"
	"math"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// SourceLine is a single-line of text.
type SourceLine struct {
	TopLeft    image.Point
	Width      int
	Text       string
	TextHeight unit.Sp
	Italic     bool
	Bold       bool
	Color      color.NRGBA
}

// Layout draws the text.
func (line SourceLine) Layout(th *material.Theme, gtx layout.Context) {
	gtx.Constraints.Min.X = 0
	gtx.Constraints.Max.X = math.MaxInt
	gtx.Constraints.Min.Y = 0
	gtx.Constraints.Max.Y = math.MaxInt

	defer op.Offset(line.TopLeft).Push(gtx.Ops).Pop()
	if line.Width > 0 {
		defer clip.Rect{Max: image.Pt(line.Width, gtx.Metric.Sp(line.TextHeight))}.Push(gtx.Ops).Pop()
	}

	font := text.Font{Variant: "Mono"}
	if line.Italic {
		font.Style = text.Italic
	}
	if line.Bold {
		font.Weight = text.Heavy
	}
	paint.ColorOp{Color: line.Color}.Add(gtx.Ops)
	widget.Label{MaxLines: 1}.Layout(gtx, th.Shaper, font, line.TextHeight, line.Text)
}

type VerticalLine struct {
	Width unit.Dp
	Color color.NRGBA
}

func (line VerticalLine) Layout(gtx layout.Context) layout.Dimensions {
	size := image.Point{
		X: gtx.Metric.Dp(line.Width),
		Y: gtx.Constraints.Min.Y,
	}
	paint.FillShape(gtx.Ops, line.Color, clip.Rect{Max: size}.Op())
	return layout.Dimensions{
		Size: size,
	}
}

type HorizontalLine struct {
	Height unit.Dp
	Color  color.NRGBA
}

func (line HorizontalLine) Layout(gtx layout.Context) layout.Dimensions {
	size := image.Point{
		X: gtx.Constraints.Min.X,
		Y: gtx.Metric.Dp(line.Height),
	}
	paint.FillShape(gtx.Ops, line.Color, clip.Rect{Max: size}.Op())
	return layout.Dimensions{
		Size: size,
	}
}
