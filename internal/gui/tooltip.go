package gui

import (
	"image"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// tooltipDelay is how long the pointer rests on a widget before its
// hint shows.
const tooltipDelay = 600 * time.Millisecond

// Tooltip shows a hint to the right of a widget once the pointer rests
// on it, or below it with Below. Pressing the widget hides the hint
// until the pointer leaves.
type Tooltip struct {
	// Below puts the hint under the widget, its right edge level with
	// the widget's, for widgets in a row along the top of the window.
	Below bool

	hovered bool
	pressed bool
	since   time.Time
}

func (t *Tooltip) Layout(gtx layout.Context, th *Theme, text string, w layout.Widget) layout.Dimensions {
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: t, Kinds: pointer.Enter | pointer.Leave | pointer.Press | pointer.Cancel})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Enter:
			t.hovered, t.pressed, t.since = true, false, gtx.Now
		case pointer.Leave, pointer.Cancel:
			t.hovered, t.pressed = false, false
		case pointer.Press:
			t.pressed = true
		}
	}

	dims := w(gtx)

	area := clip.Rect{Max: dims.Size}.Push(gtx.Ops)
	pass := pointer.PassOp{}.Push(gtx.Ops)
	event.Op(gtx.Ops, t)
	pass.Pop()
	area.Pop()

	if !t.hovered || t.pressed || text == "" {
		return dims
	}
	if show := t.since.Add(tooltipDelay); gtx.Now.Before(show) {
		gtx.Execute(op.InvalidateCmd{At: show})
		return dims
	}

	macro := op.Record(gtx.Ops)
	// The hint is not bound by the widget's own constraints.
	gtx.Constraints = layout.Constraints{Max: image.Pt(gtx.Metric.Dp(400), gtx.Metric.Dp(100))}
	label := th.Label(text, 0.88)
	label.MaxLines = 1
	tip := measure(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 4, Bottom: 4, Left: 8, Right: 8}.Layout(gtx, label.Layout)
	}).Size
	at := image.Pt(dims.Size.X+gtx.Metric.Dp(6), (dims.Size.Y-tip.Y)/2)
	if t.Below {
		at = image.Pt(dims.Size.X-tip.X, dims.Size.Y+gtx.Metric.Dp(6))
	}
	op.Offset(at).Add(gtx.Ops)
	Panel(gtx, th, tip, gtx.Metric.Dp(5))
	layout.Inset{Top: 4, Bottom: 4, Left: 8, Right: 8}.Layout(gtx, label.Layout)
	op.Defer(gtx.Ops, macro.Stop())
	return dims
}

// PointerTip draws lines in a popup beside the pointer at p, kept inside
// the area; the first line is the title.
func PointerTip(gtx layout.Context, th *Theme, p f32.Point, lines []string) {
	gtx.Constraints.Min = image.Point{}
	pad := gtx.Dp(8)
	macro := op.Record(gtx.Ops)
	height, width := 0, 0
	for i, line := range lines {
		label := th.Label(line, 0.9)
		label.MaxLines = 1
		if i == 0 {
			label.Font.Weight = font.SemiBold
		}
		if line == "" {
			height += gtx.Dp(4)
			continue
		}
		trans := op.Offset(image.Pt(pad, pad+height)).Push(gtx.Ops)
		dims := label.Layout(gtx)
		trans.Pop()
		height += dims.Size.Y
		width = max(width, dims.Size.X)
	}
	content := macro.Stop()
	size := image.Pt(width+2*pad, height+2*pad)
	at := p.Round().Add(image.Pt(gtx.Dp(14), gtx.Dp(14)))
	area := gtx.Constraints.Max
	if at.X+size.X > area.X {
		at.X = p.Round().X - gtx.Dp(14) - size.X
	}
	if at.Y+size.Y > area.Y {
		at.Y = max(area.Y-size.Y, 0)
	}
	at.X = max(at.X, 0)
	defer op.Offset(at).Push(gtx.Ops).Pop()
	Panel(gtx, th, size, gtx.Dp(6))
	content.Add(gtx.Ops)
}
