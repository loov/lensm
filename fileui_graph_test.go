package main

import (
	"testing"

	"gioui.org/f32"
)

func TestSegmentDistance(t *testing.T) {
	a, b := f32.Pt(0, 0), f32.Pt(10, 0)
	for _, tc := range []struct {
		p    f32.Point
		want float32
	}{
		{f32.Pt(5, 3), 3},   // beside the middle
		{f32.Pt(-4, 3), 5},  // past the start
		{f32.Pt(13, -4), 5}, // past the end
	} {
		if got := segmentDistance(tc.p, a, b); got != tc.want {
			t.Errorf("distance of %v = %v, want %v", tc.p, got, tc.want)
		}
	}
	if got := segmentDistance(f32.Pt(3, 4), a, a); got != 5 {
		t.Errorf("distance to a point = %v, want 5", got)
	}
}
