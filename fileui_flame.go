package main

import (
	"cmp"
	"hash/fnv"
	"image"
	"image/color"
	"math"
	"slices"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/pprof"
)

// flameNode is a function at one place in the call tree: the samples
// in it and below it through the calls that led there.
type flameNode struct {
	name   string
	parent *flameNode
	// value is the samples of the profile; delta, of a difference, the
	// change, which can be negative while value can't.
	value, delta int64
	children     []*flameNode
	index        map[string]*flameNode
}

// flameTree builds the call tree of prof's stacks, the callers above
// the callees, children in name order as pprof draws them.
func flameTree(prof *pprof.Profile) *flameNode {
	root := &flameNode{name: "all"}
	for _, s := range prof.Stacks {
		n := root
		add := func(n *flameNode) {
			if s.Value > 0 {
				n.value += s.Value
			}
			n.delta += s.Value
		}
		add(n)
		for i := len(s.Funcs) - 1; i >= 0; i-- {
			name := s.Funcs[i]
			if name == "" {
				continue
			}
			child := n.index[name]
			if child == nil {
				child = &flameNode{name: name, parent: n}
				if n.index == nil {
					n.index = map[string]*flameNode{}
				}
				n.index[name] = child
				n.children = append(n.children, child)
			}
			n = child
			add(n)
		}
	}
	var sortTree func(n *flameNode)
	sortTree = func(n *flameNode) {
		slices.SortFunc(n.children, func(a, b *flameNode) int { return cmp.Compare(a.name, b.name) })
		n.index = nil
		for _, c := range n.children {
			sortTree(c)
		}
	}
	sortTree(root)
	return root
}

// path names n and its callers, from the root down.
func (n *flameNode) path() []string {
	var p []string
	for ; n != nil; n = n.parent {
		p = append(p, n.name)
	}
	slices.Reverse(p)
	return p
}

// flameGraph draws the profile's call tree as a flame graph, the
// callers on top, each frame as wide as its share of the samples.
type flameGraph struct {
	root *flameNode
	of   *pprof.Profile
	// zoom is the path of the frame drawn full width.
	zoom []string

	offset float32 // vertical scroll, in px
	// view0 and view1 are the part of the zoomed frame's width shown,
	// as fractions of it; view1 0 means 1, the whole.
	view0, view1 float64

	// up draws the root at the bottom and the callees above it, as a
	// classic flame graph does, instead of the callers on top.
	up bool
	// reveal scrolls the zoomed frame into view on the next layout.
	reveal bool

	// matches are the frames whose names contain query, widest first,
	// and match the one Enter last zoomed to, -1 for none yet.
	matches []*flameNode
	match   int
	query   string
	queryOf *flameNode

	hover   *flameNode
	pointer f32.Point
	inside  bool
	press   bool
	// drag is where the pointer was when last dragged, and moved
	// whether the press moved far enough to pan instead of click.
	drag  f32.Point
	moved bool
	// lastNode and lastClick are the frame and time of the last click,
	// to tell a double click.
	lastNode  *flameNode
	lastClick time.Duration
}

// tree is the call tree of prof, built again when prof changes.
func (f *flameGraph) tree(prof *pprof.Profile) *flameNode {
	if f.of != prof {
		f.root, f.of, f.zoom, f.offset, f.hover = flameTree(prof), prof, nil, 0, nil
		f.view0, f.view1, f.lastNode = 0, 0, nil
	}
	return f.root
}

// find marks the frames of prof whose names contain query, which is
// lower case, and returns them.
func (f *flameGraph) find(prof *pprof.Profile, query string) []*flameNode {
	root := f.tree(prof)
	if query == f.query && root == f.queryOf {
		return f.matches
	}
	f.query, f.queryOf, f.matches, f.match = query, root, nil, -1
	if query == "" {
		return nil
	}
	var walk func(n *flameNode)
	walk = func(n *flameNode) {
		if n.parent != nil && strings.Contains(strings.ToLower(n.name), query) {
			f.matches = append(f.matches, n)
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
	slices.SortStableFunc(f.matches, func(a, b *flameNode) int { return cmp.Compare(b.value, a.value) })
	return f.matches
}

// next zooms to the next frame found.
func (f *flameGraph) next() {
	if len(f.matches) == 0 {
		return
	}
	f.match = (f.match + 1) % len(f.matches)
	f.zoomTo(f.matches[f.match])
	f.reveal = true
}

// zoomTo draws n full width.
func (f *flameGraph) zoomTo(n *flameNode) {
	f.zoom = n.path()
	f.view0, f.view1 = 0, 0
}

// zoomOut undoes a scroll zoom, or else zooms to the zoomed frame's
// caller.
func (f *flameGraph) zoomOut() {
	switch {
	case f.view1 > f.view0:
		f.view0, f.view1 = 0, 0
	case len(f.zoom) > 1:
		f.zoom = f.zoom[:len(f.zoom)-1]
		f.reveal = true
	}
}

// view is the shown part of the zoomed frame, as fractions.
func (f *flameGraph) view() (float64, float64) {
	if f.view1 <= f.view0 {
		return 0, 1
	}
	return f.view0, f.view1
}

// setView shows [v0, v0+w) of the zoomed frame, kept inside it.
func (f *flameGraph) setView(v0, w float64) {
	w = min(max(w, 1e-4), 1)
	v0 = min(max(v0, 0), 1-w)
	f.view0, f.view1 = v0, v0+w
}

// flameBox is a frame placed on screen.
type flameBox struct {
	rect image.Rectangle
	node *flameNode
}

// layoutFrames places the frames of the zoomed tree in size: the
// zoomed frame's callers each full width, then its subtree. It returns
// them and the height of them all, scrolled or not.
func (f *flameGraph) layoutFrames(gtx layout.Context, size image.Point) ([]flameBox, int) {
	row := gtx.Dp(20)
	zoomed := f.root
	for _, name := range f.zoom[min(len(f.zoom), 1):] {
		var next *flameNode
		for _, c := range zoomed.children {
			if c.name == name {
				next = c
			}
		}
		if next == nil {
			break
		}
		zoomed = next
	}
	var boxes []flameBox
	y := -int(f.offset)
	var ancestors []*flameNode
	for n := zoomed.parent; n != nil; n = n.parent {
		ancestors = append(ancestors, n)
	}
	slices.Reverse(ancestors)
	for _, n := range ancestors {
		boxes = append(boxes, flameBox{image.Rect(0, y, size.X, y+row-1), n})
		y += row
	}
	v0, v1 := f.view()
	scale := float64(size.X) / float64(max(zoomed.value, 1)) / (v1 - v0)
	var place func(n *flameNode, x0 float64, y int)
	place = func(n *flameNode, x0 float64, y int) {
		w := float64(n.value) * scale
		if w < 1 || x0 > float64(size.X) || x0+w < 0 {
			return
		}
		boxes = append(boxes, flameBox{image.Rect(int(x0), y, int(x0+w)-1, y+row-1), n})
		x := x0
		for _, c := range n.children {
			place(c, x, y+row)
			x += float64(c.value) * scale
		}
	}
	place(zoomed, -v0*float64(zoomed.value)*scale, y)
	height := 0
	for i, b := range boxes {
		height = max(height, b.rect.Max.Y+1+int(f.offset))
		if f.up {
			r := b.rect
			boxes[i].rect = image.Rect(r.Min.X, size.Y-r.Max.Y-1, r.Max.X, size.Y-r.Min.Y-1)
		}
	}
	return boxes, height
}

// layout draws the flame graph of prof and returns the function
// clicked, if any. A click also zooms to the frame; the frames above
// zoom back out. Frames whose names contain query are marked, and the
// frames of focus, the open function, outlined.
func (f *flameGraph) layout(gtx layout.Context, th *gui.Theme, prof *pprof.Profile, query, focus string) (string, bool) {
	size := gtx.Constraints.Max
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	f.tree(prof)
	boxes, _ := f.layoutFrames(gtx, size)
	at := func(p f32.Point) *flameNode {
		pt := p.Round()
		for i := len(boxes) - 1; i >= 0; i-- {
			if pt.In(boxes[i].rect) {
				return boxes[i].node
			}
		}
		return nil
	}

	// Scrolling and dragging move the frames the other way up.
	dir := float32(1)
	if f.up {
		dir = -1
	}
	clicked := ""
	event.Op(gtx.Ops, f)
	for {
		ev, ok := gtx.Event(
			key.FocusFilter{Target: f},
			key.Filter{Focus: f, Name: key.NameEscape},
			key.Filter{Focus: f, Name: key.NameDeleteBackward},
			key.Filter{Focus: f, Name: key.NameHome},
		)
		if !ok {
			break
		}
		if e, ok := ev.(key.Event); ok && e.State == key.Press {
			if e.Name == key.NameHome {
				f.zoom, f.view0, f.view1, f.offset = nil, 0, 0, 0
			} else {
				f.zoomOut()
			}
		}
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  f,
			Kinds:   pointer.Move | pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Leave | pointer.Scroll,
			ScrollX: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20},
			ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20},
		})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		switch e.Kind {
		case pointer.Scroll:
			v0, v1 := f.view()
			w := v1 - v0
			if e.Modifiers.Contain(key.ModShortcut) {
				// Zoom about the pointer.
				at := float64(e.Position.X) / float64(size.X)
				nw := w * math.Exp(float64(e.Scroll.Y)/200)
				f.setView(v0+at*w-at*min(max(nw, 1e-4), 1), nw)
			} else {
				f.setView(v0+float64(e.Scroll.X)/float64(size.X)*w, w)
				f.offset += dir * e.Scroll.Y
			}
			boxes, _ = f.layoutFrames(gtx, size)
		case pointer.Move:
			f.hover, f.pointer, f.inside = at(e.Position), e.Position, true
		case pointer.Leave:
			f.hover, f.inside = nil, false
		case pointer.Press:
			f.press, f.moved, f.drag = true, false, e.Position
		case pointer.Drag:
			// Drag to pan, once past a click's slop.
			if d := e.Position.Sub(f.drag); f.moved || d.X*d.X+d.Y*d.Y > float32(gtx.Dp(4)*gtx.Dp(4)) {
				f.moved = true
				v0, v1 := f.view()
				f.setView(v0-float64(d.X)/float64(size.X)*(v1-v0), v1-v0)
				f.offset -= dir * d.Y
				f.drag = e.Position
				boxes, _ = f.layoutFrames(gtx, size)
			}
		case pointer.Release:
			// A click zooms to the frame, which keeps its row, so it
			// stays under the pointer. A double click or one with
			// Command or Ctrl held also opens its function.
			if n := at(e.Position); f.press && !f.moved && n != nil {
				double := n == f.lastNode && e.Time-f.lastClick < 400*time.Millisecond
				f.lastNode, f.lastClick = n, e.Time
				f.zoomTo(n)
				// Escape and Home then zoom out.
				gtx.Execute(key.FocusCmd{Tag: f})
				if n.parent != nil && (double || e.Modifiers.Contain(key.ModShortcut)) {
					clicked = n.name
					f.lastNode = nil
				}
			}
			f.press = false
		case pointer.Cancel:
			f.press = false
		}
	}
	if f.reveal {
		// Put the zoomed frame a third of the way into the pane.
		f.reveal = false
		f.offset = float32(max(len(f.zoom)-1, 0)*gtx.Dp(20) - size.Y/3)
	}
	// Keep the scroll within the frames.
	_, height := f.layoutFrames(gtx, size)
	f.offset = min(max(f.offset, 0), float32(max(height-size.Y, 0)))
	boxes, _ = f.layoutFrames(gtx, size)
	if f.hover != nil {
		pointer.CursorPointer.Add(gtx.Ops)
	}

	c := th.Colors
	textSize := unit.Sp(float32(th.TextSize) * 0.8)
	for _, b := range boxes {
		n := b.node
		col := flameColor(n.name)
		if prof.Diff {
			// A difference is red where it grew and green where it shrank.
			col = c.Marks.Alloc
			share := float32(abs(n.delta)) / float32(max(n.value, 1))
			switch {
			case n.delta > 0:
				col = c.Marks.BoundsCheck
			case n.delta < 0:
				col = c.Marks.Good
			}
			col.A = uint8(0x20 + 0xa0*min(share, 1))
		}
		radius := gtx.Dp(2)
		paint.FillShape(gtx.Ops, c.Background, clip.UniformRRect(b.rect, radius).Op(gtx.Ops))
		paint.FillShape(gtx.Ops, col, clip.UniformRRect(b.rect, radius).Op(gtx.Ops))
		switch {
		case query != "" && n.parent != nil && strings.Contains(strings.ToLower(n.name), query):
			paint.FillShape(gtx.Ops, c.Marks.Spill, clip.Stroke{Path: clip.UniformRRect(b.rect, radius).Path(gtx.Ops), Width: float32(gtx.Dp(2))}.Op())
		case n == f.hover || n.name == focus:
			paint.FillShape(gtx.Ops, c.Accent, clip.Stroke{Path: clip.UniformRRect(b.rect, radius).Path(gtx.Ops), Width: float32(gtx.Dp(1))}.Op())
		}
		if b.rect.Dx() > gtx.Dp(24) {
			label := th.Label(n.name, 0.8)
			label.TextSize = textSize
			label.MaxLines = 1
			label.Color = c.Text
			// Keep the name in view when the frame runs off an edge.
			inner := b.rect.Intersect(image.Rect(0, b.rect.Min.Y, size.X, b.rect.Max.Y)).Inset(gtx.Dp(4))
			gtx := gtx
			gtx.Constraints = layout.Exact(image.Pt(max(inner.Dx(), 0), b.rect.Dy()))
			stack := clip.Rect(b.rect).Push(gtx.Ops)
			trans := op.Offset(image.Pt(inner.Min.X, b.rect.Min.Y+gtx.Dp(2))).Push(gtx.Ops)
			label.Layout(gtx)
			trans.Pop()
			stack.Pop()
		}
	}

	if f.hover != nil && f.inside {
		n := f.hover
		tip := []string{n.name, valueLabel(prof, n.value)}
		if prof.Diff {
			tip = append(tip, "change "+signed(prof, n.delta, valueText(prof, n.delta)))
		}
		if n.parent != nil && n.parent.value > 0 {
			tip = append(tip, percentText(100*float64(n.value)/float64(n.parent.value))+" of "+n.parent.name)
		}
		drawTip(gtx, th, f.pointer, tip)
	}
	return clicked, clicked != ""
}

// flameColor is a warm colour that stays the same for a package, so the
// frames of one package look alike.
func flameColor(name string) color.NRGBA {
	pkg, _ := gui.SplitPackage(name)
	h := fnv.New32a()
	h.Write([]byte(strings.TrimSuffix(pkg, ".")))
	v := h.Sum32()
	return color.NRGBA{R: 0xe8 + uint8(v%24), G: 0x90 + uint8(v>>8%80), B: 0x50 + uint8(v>>16%50), A: 0xc0}
}
