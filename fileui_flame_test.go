package main

import (
	"image"
	"math"
	"slices"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/pprof"
)

func TestFlameTree(t *testing.T) {
	prof := &pprof.Profile{Stacks: []pprof.Stack{
		{Funcs: []string{"main.b", "main.main"}, Value: 3},
		{Funcs: []string{"main.a", "main.main"}, Value: 2},
		{Funcs: []string{"main.main"}, Value: 1},
		// A frame without a name is skipped, and base samples of a
		// difference count in delta only.
		{Funcs: []string{"main.a", "", "main.main"}, Value: -4},
	}}
	root := flameTree(prof)
	if root.value != 6 || root.delta != 2 || len(root.children) != 1 {
		t.Fatalf("root = %d, delta %d, %d children", root.value, root.delta, len(root.children))
	}
	main := root.children[0]
	var names []string
	for _, c := range main.children {
		names = append(names, c.name)
	}
	if main.name != "main.main" || main.value != 6 || !slices.Equal(names, []string{"main.a", "main.b"}) {
		t.Errorf("main = %s %d, children %q", main.name, main.value, names)
	}
	if a := main.children[0]; a.value != 2 || a.delta != -2 || !slices.Equal(a.path(), []string{"all", "main.main", "main.a"}) {
		t.Errorf("main.a = %d, delta %d, path %q", a.value, a.delta, a.path())
	}
}

// TestFlameClickZooms clicks a frame, which zooms to it, then the
// frame above it, which zooms back out.
func TestFlameClickZooms(t *testing.T) {
	prof := &pprof.Profile{Total: 4, Stacks: []pprof.Stack{
		{Funcs: []string{"main.a", "main.main"}, Value: 3},
		{Funcs: []string{"main.b", "main.main"}, Value: 1},
	}}
	th := gui.NewTheme(material.NewTheme(), false)
	var f flameGraph
	var router input.Router
	var ops op.Ops
	frame := func() (string, bool) {
		ops.Reset()
		gtx := layout.Context{
			Ops:         &ops,
			Source:      router.Source(),
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Now:         time.Now(),
			Constraints: layout.Exact(image.Pt(400, 300)),
		}
		name, ok := f.layout(gtx, th, prof, "", "")
		router.Frame(gtx.Ops)
		return name, ok
	}
	var now time.Duration
	click := func(x, y float32, mods key.Modifiers) (string, bool) {
		now += time.Second
		router.Queue(
			pointer.Event{Source: pointer.Mouse, PointerID: 1, Buttons: pointer.ButtonPrimary, Kind: pointer.Press, Position: f32.Pt(x, y), Time: now},
			pointer.Event{Source: pointer.Mouse, PointerID: 1, Kind: pointer.Release, Position: f32.Pt(x, y), Time: now, Modifiers: mods},
		)
		return frame()
	}
	frame()
	// Rows are 20dp: all, main.main, then main.a over the first 3/4. A
	// click zooms without opening.
	if name, ok := click(100, 50, 0); ok || !slices.Equal(f.zoom, []string{"all", "main.main", "main.a"}) {
		t.Fatalf("click on main.a = %q %v, zoom %q", name, ok, f.zoom)
	}
	// main.a now fills the width, so x=350 is in it, not in main.b.
	frame()
	boxes, _ := f.layoutFrames(layout.Context{Metric: unit.Metric{PxPerDp: 1}}, image.Pt(400, 300))
	if last := boxes[len(boxes)-1]; last.node.name != "main.a" || last.rect.Dx() < 390 {
		t.Fatalf("zoomed frame = %s, %v wide", last.node.name, last.rect.Dx())
	}
	// Command and click opens it.
	if name, ok := click(350, 50, key.ModShortcut); !ok || name != "main.a" {
		t.Errorf("command click = %q %v", name, ok)
	}
	// So does a double click.
	click(350, 30, 0)
	now -= time.Second - 100*time.Millisecond
	if name, ok := click(350, 30, 0); !ok || name != "main.main" {
		t.Errorf("double click = %q %v", name, ok)
	}
	// Escape zooms out a step at a time, from main.main to all.
	router.Queue(key.Event{Name: key.NameEscape, State: key.Press})
	frame()
	if len(f.zoom) != 1 {
		t.Errorf("escape left zoom %q", f.zoom)
	}
	if _, _ = click(200, 5, 0); len(f.zoom) != 1 {
		t.Errorf("click on all zooms to %q", f.zoom)
	}

	// Find counts the frames and Enter zooms to the widest first.
	if found := f.find(prof, "main.b"); len(found) != 1 {
		t.Errorf("found %d frames of main.b", len(found))
	}
	f.next()
	if !slices.Equal(f.zoom, []string{"all", "main.main", "main.b"}) {
		t.Errorf("next zoomed to %q", f.zoom)
	}
	f.zoom = nil

	// Upside down, the root is the bottom row.
	f.up = true
	boxes, _ = f.layoutFrames(layout.Context{Metric: unit.Metric{PxPerDp: 1}}, image.Pt(400, 300))
	if b := boxes[0]; b.node.parent != nil || b.rect.Max.Y != 299 {
		t.Errorf("root upside down at %v", b.rect)
	}
	f.up = false

	// Command and scroll zooms about the pointer: the point a quarter
	// across stays a quarter across.
	router.Queue(pointer.Event{Source: pointer.Mouse, Kind: pointer.Move, Position: f32.Pt(100, 50)})
	frame()
	router.Queue(pointer.Event{Source: pointer.Mouse, Kind: pointer.Scroll, Position: f32.Pt(100, 50), Scroll: f32.Pt(0, -200), Modifiers: key.ModShortcut})
	frame()
	v0, v1 := f.view()
	if w := v1 - v0; math.Abs(w-math.Exp(-1)) > 1e-3 || math.Abs(v0+0.25*w-0.25) > 1e-3 {
		t.Errorf("view after zoom = [%.3f, %.3f)", v0, v1)
	}
	// A drag pans, without zooming to the frame pressed.
	router.Queue(
		pointer.Event{Source: pointer.Mouse, PointerID: 3, Buttons: pointer.ButtonPrimary, Kind: pointer.Press, Position: f32.Pt(200, 50)},
		pointer.Event{Source: pointer.Mouse, PointerID: 3, Buttons: pointer.ButtonPrimary, Kind: pointer.Move, Position: f32.Pt(300, 50)},
		pointer.Event{Source: pointer.Mouse, PointerID: 3, Kind: pointer.Release, Position: f32.Pt(300, 50)},
	)
	if name, ok := frame(); ok || len(f.zoom) > 1 {
		t.Errorf("drag clicked %q, zoom %q", name, f.zoom)
	}
	if p0, _ := f.view(); p0 >= v0 {
		t.Errorf("drag right left the view at %.3f, from %.3f", p0, v0)
	}
}
