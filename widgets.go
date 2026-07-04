package main

import (
	"image"
	"image/color"
	"time"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// SourceLine is a single-line of text.
type SourceLine struct {
	TopLeft    image.Point
	Width      int
	Text       string
	Spans      []SourceSpan
	TextHeight unit.Sp
	Italic     bool
	Bold       bool
	Color      color.NRGBA
}

type SourceSpan struct {
	Text   string
	Color  color.NRGBA
	Italic bool
	Bold   bool
}

// codeLineHeightScale leaves room for ascenders and descenders when a
// line is clipped to its width; rows themselves advance by the tighter
// CodeUIStyle.LineHeight.
const codeLineHeightScale = 4.0 / 3.0

func codeLineHeightPx(gtx layout.Context, textHeight unit.Sp) int {
	height := gtx.Metric.Sp(textHeight * codeLineHeightScale)
	if height < 1 {
		return 1
	}
	return height
}

// Layout draws the text.
func (line SourceLine) Layout(th *material.Theme, gtx layout.Context) {
	gtx.Constraints.Min.X = 0
	gtx.Constraints.Max.X = maxLineWidth
	gtx.Constraints.Min.Y = 0
	gtx.Constraints.Max.Y = maxLineWidth

	defer op.Offset(line.TopLeft).Push(gtx.Ops).Pop()
	if line.Width > 0 {
		maxSize := image.Pt(line.Width, codeLineHeightPx(gtx, line.TextHeight))
		defer clip.Rect{Max: maxSize}.Push(gtx.Ops).Pop()
	}

	spans := line.Spans
	if len(spans) == 0 {
		spans = []SourceSpan{{
			Text:   line.Text,
			Color:  line.Color,
			Italic: line.Italic,
			Bold:   line.Bold,
		}}
	}

	left := 0
	for _, span := range spans {
		if span.Text == "" {
			continue
		}
		dims := layoutSourceSpan(th, gtx, line, span, left)
		left += dims.Size.X
	}
}

func layoutSourceSpan(th *material.Theme, gtx layout.Context, line SourceLine, span SourceSpan, left int) layout.Dimensions {
	f := font.Font{Typeface: "override-monospace,Go,monospace", Weight: font.Normal}
	if line.Italic || span.Italic {
		f.Style = font.Italic
	}
	if line.Bold || span.Bold {
		f.Weight = font.Black
	}
	col := span.Color
	if col == (color.NRGBA{}) {
		col = line.Color
	}

	defer op.Offset(image.Pt(left, 0)).Push(gtx.Ops).Pop()
	paint.ColorOp{Color: col}.Add(gtx.Ops)
	return widget.Label{MaxLines: 1}.Layout(gtx, th.Shaper, f, line.TextHeight, span.Text, op.CallOp{})
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
	gtx.Execute(op.InvalidateCmd{})
}

func (anim *ScrollAnimation) Stop() { anim.active = false }

func (anim *ScrollAnimation) Update(gtx layout.Context) (float32, bool) {
	if !anim.active {
		return anim.to, false
	}
	gtx.Execute(op.InvalidateCmd{})

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

const maxLineWidth = 10 * 1024
