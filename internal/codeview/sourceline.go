package codeview

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

	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/syntax"
)

// sourceLine is a single line of text.
type sourceLine struct {
	TopLeft    image.Point
	Width      int
	Text       string
	Spans      []syntax.Span
	TextHeight unit.Sp
	Italic     bool
	Bold       bool
	Color      color.NRGBA
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
func (line sourceLine) Layout(th *material.Theme, gtx layout.Context) {
	gtx.Constraints.Min.X = 0
	gtx.Constraints.Max.X = gui.MaxLineWidth
	gtx.Constraints.Min.Y = 0
	gtx.Constraints.Max.Y = gui.MaxLineWidth

	defer op.Offset(line.TopLeft).Push(gtx.Ops).Pop()
	if line.Width > 0 {
		maxSize := image.Pt(line.Width, codeLineHeightPx(gtx, line.TextHeight))
		defer clip.Rect{Max: maxSize}.Push(gtx.Ops).Pop()
	}

	spans := line.Spans
	if len(spans) == 0 {
		spans = []syntax.Span{{
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

func layoutSourceSpan(th *material.Theme, gtx layout.Context, line sourceLine, span syntax.Span, left int) layout.Dimensions {
	f := font.Font{Typeface: gui.Monospace, Weight: font.Normal}
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

type scrollAnimation struct {
	active   bool
	from, to float32
	duration time.Duration
	start    time.Time
}

func (anim *scrollAnimation) Start(gtx layout.Context, from, to float32, duration time.Duration) {
	anim.active = true
	anim.from = from
	anim.to = to
	anim.duration = duration
	anim.start = gtx.Now
	gtx.Execute(op.InvalidateCmd{})
}

func (anim *scrollAnimation) Stop() { anim.active = false }

func (anim *scrollAnimation) Update(gtx layout.Context) (float32, bool) {
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

// SyntaxColors is the base of the syntax palette for the UI colours.
func SyntaxColors(c gui.UIColors) syntax.Colors {
	return syntax.Colors{Text: c.Text, MutedText: c.MutedText, Background: c.Background}
}
