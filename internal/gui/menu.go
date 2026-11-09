package gui

import (
	"image"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
)

// MenuItem is one row of a Menu.
type MenuItem struct {
	Label string
	// Shortcut is shown right-aligned, for example "⌘O".
	Shortcut string
	Checked  bool
	Disabled bool
	// Separator draws a line instead of an item.
	Separator bool
}

// Menu is a popup list of items anchored below a widget. Clicking the
// anchor opens it; choosing an item, pressing Escape or clicking
// anywhere else closes it.
type Menu struct {
	Open bool
	// Up opens the popup above the anchor, for anchors at the bottom of
	// the window.
	Up bool

	anchor widget.Clickable
	items  []widget.Clickable
	// scrim catches clicks outside the popup.
	scrim int
}

// Update returns the index of the chosen item, if any. Call it before
// Layout in each frame.
func (m *Menu) Update(gtx layout.Context) (int, bool) {
	for m.anchor.Clicked(gtx) {
		m.Open = !m.Open
	}
	if !m.Open {
		return 0, false
	}
	for {
		ev, ok := gtx.Event(
			pointer.Filter{Target: &m.scrim, Kinds: pointer.Press},
			key.Filter{Name: key.NameEscape},
		)
		if !ok {
			break
		}
		switch ev := ev.(type) {
		case pointer.Event:
			m.Open = false
		case key.Event:
			if ev.State == key.Press {
				m.Open = false
			}
		}
	}
	for i := range m.items {
		if m.items[i].Clicked(gtx) {
			m.Open = false
			return i, true
		}
	}
	return 0, false
}

// Hovered reports whether the pointer is over the anchor.
func (m *Menu) Hovered() bool { return m.anchor.Hovered() }

// Layout draws anchor and, while open, the popup below it on top of
// everything else in the window.
func (m *Menu) Layout(gtx layout.Context, th *Theme, items []MenuItem, anchor func(gtx layout.Context, open bool) layout.Dimensions) layout.Dimensions {
	dims := m.anchor.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		semantic.Button.Add(gtx.Ops)
		return anchor(gtx, m.Open)
	})
	if !m.Open {
		return dims
	}
	for len(m.items) < len(items) {
		m.items = append(m.items, widget.Clickable{})
	}

	macro := op.Record(gtx.Ops)
	m.layoutScrim(gtx)
	gtx.Constraints.Min = image.Point{}
	popup := op.Record(gtx.Ops)
	size := m.layoutPopup(gtx, th, items).Size
	call := popup.Stop()
	at := dims.Size.Y + gtx.Metric.Dp(4)
	if m.Up {
		at = -size.Y - gtx.Metric.Dp(4)
	}
	op.Offset(image.Pt(0, at)).Add(gtx.Ops)
	call.Add(gtx.Ops)
	op.Defer(gtx.Ops, macro.Stop())
	return dims
}

// layoutScrim covers the whole window, wherever the menu is, with an
// invisible area that closes the menu when pressed.
func (m *Menu) layoutScrim(gtx layout.Context) {
	const far = 1 << 20
	defer clip.Rect{Min: image.Pt(-far, -far), Max: image.Pt(far, far)}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, &m.scrim)
}

func (m *Menu) layoutPopup(gtx layout.Context, th *Theme, items []MenuItem) layout.Dimensions {
	pad := gtx.Metric.Dp(5)
	width := gtx.Metric.Dp(220)
	for _, item := range items {
		if !item.Separator {
			width = max(width, m.itemWidth(gtx, th, item))
		}
	}

	macro := op.Record(gtx.Ops)
	op.Offset(image.Pt(pad, pad)).Add(gtx.Ops)
	height := 0
	for i, item := range items {
		trans := op.Offset(image.Pt(0, height)).Push(gtx.Ops)
		dims := m.layoutItem(gtx, th, i, item, width)
		trans.Pop()
		height += dims.Size.Y
	}
	content := macro.Stop()

	size := image.Pt(width+2*pad, height+2*pad)
	Panel(gtx, th, size, gtx.Metric.Dp(9))
	// Clicks between rows must not fall through to the scrim.
	area := clip.Rect{Max: size}.Push(gtx.Ops)
	event.Op(gtx.Ops, m)
	area.Pop()
	content.Add(gtx.Ops)
	return layout.Dimensions{Size: size}
}

// Item geometry in dp: the check column, and the gaps around the label
// and before the shortcut.
const menuCheck, menuRight, menuShortcutGap = 26, 10, 24

func (m *Menu) itemWidth(gtx layout.Context, th *Theme, item MenuItem) int {
	w := gtx.Metric.Dp(menuCheck+menuRight) + measure(gtx, th.Label(item.Label, 0.95).Layout).Size.X
	if item.Shortcut != "" {
		w += gtx.Metric.Dp(menuShortcutGap) + measure(gtx, th.Label(item.Shortcut, 0.95).Layout).Size.X
	}
	return w
}

func (m *Menu) layoutItem(gtx layout.Context, th *Theme, i int, item MenuItem, width int) layout.Dimensions {
	c := th.Colors
	if item.Separator {
		size := image.Pt(width, gtx.Metric.Dp(11))
		inset := gtx.Metric.Dp(8)
		y := size.Y / 2
		paint.FillShape(gtx.Ops, c.Divider, clip.Rect{Min: image.Pt(inset, y), Max: image.Pt(size.X-inset, y+gtx.Metric.Dp(1))}.Op())
		return layout.Dimensions{Size: size}
	}
	size := image.Pt(width, gtx.Metric.Sp(th.TextSize)+gtx.Metric.Dp(10))
	gtx.Constraints = layout.Exact(size)
	row := func(gtx layout.Context, hovered bool) layout.Dimensions {
		fg, muted := c.Text, c.MutedText
		switch {
		case item.Disabled:
			fg = c.MutedText
		case hovered:
			fg, muted = c.AccentText, c.AccentText
			paint.FillShape(gtx.Ops, c.Accent, clip.UniformRRect(image.Rectangle{Max: size}, gtx.Metric.Dp(5)).Op(gtx.Ops))
		}
		if item.Checked {
			s := gtx.Metric.Dp(14)
			trans := op.Offset(image.Pt(gtx.Metric.Dp(6), (size.Y-s)/2)).Push(gtx.Ops)
			gtx := gtx
			gtx.Constraints = layout.Exact(image.Pt(s, s))
			CheckIcon.Layout(gtx, fg)
			trans.Pop()
		}
		label := th.Label(item.Label, 0.95)
		label.Color = fg
		label.MaxLines = 1
		trans := op.Offset(image.Pt(gtx.Metric.Dp(menuCheck), 0)).Push(gtx.Ops)
		dims := Start(gtx, label.Layout)
		trans.Pop()
		if item.Shortcut != "" {
			// Symbols like ⌘ come from a fallback font with other
			// metrics, so line the shortcut up by baseline.
			label := th.Label(item.Shortcut, 0.95)
			label.Color = muted
			short := measure(gtx, label.Layout)
			at := image.Pt(size.X-gtx.Metric.Dp(menuRight)-short.Size.X, (size.Y-dims.Baseline)-(short.Size.Y-short.Baseline))
			trans := op.Offset(at).Push(gtx.Ops)
			gtx := gtx
			gtx.Constraints.Min = image.Point{}
			label.Layout(gtx)
			trans.Pop()
		}
		return layout.Dimensions{Size: size}
	}
	if item.Disabled {
		return row(gtx, false)
	}
	click := &m.items[i]
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		semantic.Button.Add(gtx.Ops)
		return row(gtx, click.Hovered())
	})
}

// measure returns the dimensions w would take, without drawing it.
func measure(gtx layout.Context, w layout.Widget) layout.Dimensions {
	macro := op.Record(gtx.Ops)
	gtx.Constraints.Min = image.Point{}
	dims := w(gtx)
	macro.Stop()
	return dims
}

// Panel draws the rounded, outlined surface of a popup or dialog with
// a soft shadow below it.
func Panel(gtx layout.Context, th *Theme, size image.Point, radius int) {
	c := th.Colors
	rect := image.Rectangle{Max: size}
	shadow := c.Shadow
	for i, spread := range []unit.Dp{12, 6, 2} {
		s := gtx.Metric.Dp(spread)
		shadow.A = c.Shadow.A / uint8(6-i*2)
		r := rect.Inset(-s).Add(image.Pt(0, s/2))
		paint.FillShape(gtx.Ops, shadow, clip.UniformRRect(r, radius+s).Op(gtx.Ops))
	}
	paint.FillShape(gtx.Ops, c.Shadow, clip.UniformRRect(rect.Inset(-1), radius+1).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, c.Background, clip.UniformRRect(rect, radius).Op(gtx.Ops))
}

// Overlay fills the window with the scrim and blocks pointer input to
// everything below it. Lay it out after the content it covers.
func Overlay(gtx layout.Context, th *Theme, tag event.Tag) {
	size := gtx.Constraints.Max
	paint.FillShape(gtx.Ops, th.Colors.Scrim, clip.Rect{Max: size}.Op())
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, tag)
	for {
		if _, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press}); !ok {
			break
		}
	}
}
