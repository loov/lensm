package main

import (
	"cmp"
	"hash/fnv"
	"image"
	"image/color"
	"slices"
	"strings"

	"gioui.org/f32"
	"gioui.org/io/event"
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

	offset  float32 // vertical scroll, in px
	hover   *flameNode
	pointer f32.Point
	inside  bool
	press   bool
}

// flameBox is a frame placed on screen.
type flameBox struct {
	rect image.Rectangle
	node *flameNode
}

// layoutFrames places the frames of the zoomed tree in size: the
// zoomed frame's callers each full width, then its subtree.
func (f *flameGraph) layoutFrames(gtx layout.Context, size image.Point) []flameBox {
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
	scale := float64(size.X) / float64(max(zoomed.value, 1))
	var place func(n *flameNode, x0 float64, y int)
	place = func(n *flameNode, x0 float64, y int) {
		w := float64(n.value) * scale
		if w < 1 {
			return
		}
		boxes = append(boxes, flameBox{image.Rect(int(x0), y, int(x0+w)-1, y+row-1), n})
		x := x0
		for _, c := range n.children {
			place(c, x, y+row)
			x += float64(c.value) * scale
		}
	}
	place(zoomed, 0, y)
	return boxes
}

// layout draws the flame graph of prof and returns the function
// clicked, if any. A click also zooms to the frame; the frames above
// zoom back out. Frames whose names contain query are marked, and the
// frames of focus, the open function, outlined.
func (f *flameGraph) layout(gtx layout.Context, th *gui.Theme, prof *pprof.Profile, query, focus string) (string, bool) {
	size := gtx.Constraints.Max
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	if f.of != prof {
		f.root, f.of, f.zoom, f.offset, f.hover = flameTree(prof), prof, nil, 0, nil
	}
	boxes := f.layoutFrames(gtx, size)
	at := func(p f32.Point) *flameNode {
		pt := p.Round()
		for i := len(boxes) - 1; i >= 0; i-- {
			if pt.In(boxes[i].rect) {
				return boxes[i].node
			}
		}
		return nil
	}

	clicked := ""
	event.Op(gtx.Ops, f)
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  f,
			Kinds:   pointer.Move | pointer.Press | pointer.Release | pointer.Leave | pointer.Scroll,
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
			f.offset += e.Scroll.Y
		case pointer.Move:
			f.hover, f.pointer, f.inside = at(e.Position), e.Position, true
		case pointer.Leave:
			f.hover, f.inside = nil, false
		case pointer.Press:
			f.press = true
		case pointer.Release:
			if n := at(e.Position); f.press && n != nil {
				f.zoom = n.path()
				f.offset = 0
				if n.parent != nil {
					clicked = n.name
				}
			}
			f.press = false
		}
	}
	// Keep the scroll within the frames.
	height := 0
	for _, b := range boxes {
		height = max(height, b.rect.Max.Y+int(f.offset))
	}
	f.offset = min(max(f.offset, 0), float32(max(height-size.Y, 0)))
	boxes = f.layoutFrames(gtx, size)
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
		case query != "" && strings.Contains(strings.ToLower(n.name), query):
			paint.FillShape(gtx.Ops, c.Marks.Spill, clip.Stroke{Path: clip.UniformRRect(b.rect, radius).Path(gtx.Ops), Width: float32(gtx.Dp(2))}.Op())
		case n == f.hover || n.name == focus:
			paint.FillShape(gtx.Ops, c.Accent, clip.Stroke{Path: clip.UniformRRect(b.rect, radius).Path(gtx.Ops), Width: float32(gtx.Dp(1))}.Op())
		}
		if b.rect.Dx() > gtx.Dp(24) {
			label := th.Label(n.name, 0.8)
			label.TextSize = textSize
			label.MaxLines = 1
			label.Color = c.Text
			inner := b.rect.Inset(gtx.Dp(4))
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
