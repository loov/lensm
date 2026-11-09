package gui

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

func TestSplitKeepsRatioInSmallRoom(t *testing.T) {
	th := NewTheme(material.NewTheme(), false)
	s := Split{Axis: layout.Horizontal, Ratio: 0.9}
	var first int
	a := func(gtx layout.Context) layout.Dimensions {
		first = gtx.Constraints.Max.X
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	b := func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{Size: gtx.Constraints.Max} }
	gtx := layout.Context{Ops: new(op.Ops), Metric: unit.Metric{PxPerDp: 1}, Constraints: layout.Exact(image.Pt(400, 100))}
	if _, moved := s.Layout(gtx, th, 50, 100, a, b); moved || s.Ratio != 0.9 {
		t.Errorf("moved %v, ratio %v", moved, s.Ratio)
	}
	// The second pane keeps its 100: 400*0.75, less the handle's share.
	if first > 300 {
		t.Errorf("first pane %d wide", first)
	}
}
