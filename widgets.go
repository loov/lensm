package main

import (
	"image"
	"image/color"
	"math"
	"time"

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

type ScrollAnimation struct {
	active   bool
	from, to float32
	duration time.Duration
	start    time.Time
}

func (anim *ScrollAnimation) Start(gtx layout.Context, from, to float32, duration time.Duration) {
	anim.active = true
	anim.from = from
	anim.to = to
	anim.duration = duration
	anim.start = gtx.Now
	op.InvalidateOp{}.Add(gtx.Ops)
}

func (anim *ScrollAnimation) Stop() { anim.active = false }

func (anim *ScrollAnimation) Update(gtx layout.Context) (float32, bool) {
	if !anim.active {
		return anim.to, false
	}
	op.InvalidateOp{}.Add(gtx.Ops)

	elapsed := gtx.Now.Sub(anim.start)
	if elapsed > anim.duration {
		anim.active = false
		return anim.to, true
	}

	progress := float32(elapsed) / float32(anim.duration)
	progress = easeInOutCubic(progress)

	pos := anim.from + progress*(anim.to-anim.from)
	return pos, true
}

func easeInOutCubic(t float32) float32 {
	if t < .5 {
		return 4 * t * t * t
	}
	return (t-1)*(2*t-2)*(2*t-2) + 1
}
