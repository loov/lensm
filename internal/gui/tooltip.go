package gui

import (
	"image"
	"time"

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
// on it. Pressing the widget hides the hint until the pointer leaves.
type Tooltip struct {
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
	op.Offset(image.Pt(dims.Size.X+gtx.Metric.Dp(6), (dims.Size.Y-tip.Y)/2)).Add(gtx.Ops)
	Panel(gtx, th, tip, gtx.Metric.Dp(5))
	layout.Inset{Top: 4, Bottom: 4, Left: 8, Right: 8}.Layout(gtx, label.Layout)
	op.Defer(gtx.Ops, macro.Stop())
	return dims
}
