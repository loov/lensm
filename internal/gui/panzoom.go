package gui

import (
	"math"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
)

// PanZoom turns the pointer over a view into gestures: a drag or a
// scroll pans, a scroll with Command or Ctrl held zooms about the
// pointer, and a press let go without moving past a click's slop
// clicks.
type PanZoom struct {
	// Pointer is where the pointer is, while Inside.
	Pointer f32.Point
	Inside  bool

	press, moved bool
	drag         f32.Point
}

// PanZoomKind is the kind of a PanZoomEvent.
type PanZoomKind uint8

const (
	// Hover is a move of the pointer over the view.
	Hover PanZoomKind = iota
	// Leave is the pointer leaving the view.
	Leave
	// Pan moves the content by Pan.
	Pan
	// Zoom scales the content by Zoom about Position.
	Zoom
	// Click is a press and release at Position.
	Click
)

// PanZoomEvent is a gesture over a view.
type PanZoomEvent struct {
	Kind      PanZoomKind
	Position  f32.Point
	Pan       f32.Point
	Zoom      float32
	Modifiers key.Modifiers
	Time      time.Duration
}

// Add makes the current clip area the view.
func (p *PanZoom) Add(ops *op.Ops) { event.Op(ops, p) }

// Update returns the next gesture over the view.
func (p *PanZoom) Update(gtx layout.Context) (PanZoomEvent, bool) {
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  p,
			Kinds:   pointer.Move | pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Leave | pointer.Scroll,
			ScrollX: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20},
			ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20},
		})
		if !ok {
			return PanZoomEvent{}, false
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		out := PanZoomEvent{Position: e.Position, Modifiers: e.Modifiers, Time: e.Time}
		switch e.Kind {
		case pointer.Scroll:
			if e.Modifiers.Contain(key.ModShortcut) {
				out.Kind, out.Zoom = Zoom, float32(math.Exp(float64(-e.Scroll.Y)/200))
				return out, true
			}
			out.Kind, out.Pan = Pan, e.Scroll.Mul(-1)
			return out, true
		case pointer.Move:
			p.Pointer, p.Inside = e.Position, true
			out.Kind = Hover
			return out, true
		case pointer.Leave:
			p.Inside = false
			out.Kind = Leave
			return out, true
		case pointer.Press:
			p.press, p.moved, p.drag = true, false, e.Position
		case pointer.Drag:
			d := e.Position.Sub(p.drag)
			if !p.moved && d.X*d.X+d.Y*d.Y <= float32(gtx.Dp(4)*gtx.Dp(4)) {
				continue
			}
			p.moved, p.drag = true, e.Position
			out.Kind, out.Pan = Pan, d
			return out, true
		case pointer.Release:
			click := p.press && !p.moved
			p.press = false
			if click {
				out.Kind = Click
				return out, true
			}
		case pointer.Cancel:
			p.press = false
		}
	}
}
