package codeview

import (
	"math"
	"testing"

	"loov.dev/lensm/internal/disasm"
)

func TestScrollAnchors(t *testing.T) {
	source := []disasm.Source{{
		File:   "a.go",
		Blocks: []disasm.SourceBlock{{From: 1, To: 4, Lines: []string{"a", "b", "c", "d"}}},
	}}
	// The header is source row 0, so line n is row n. Line 2 has a
	// second run, and line 1 comes after line 4.
	code := &disasm.Code{Source: source, Insts: []disasm.Inst{
		{File: "a.go", Line: 2},
		{File: "a.go", Line: 3},
		{File: "a.go", Line: 2},
		{File: "a.go", Line: 4},
		{File: "a.go", Line: 1},
		{File: "a.go", Line: 4},
	}}
	ui := &UI{Code: code}
	got := ui.scrollAnchors()
	want := []scrollAnchor{{0, 2}, {1, 3}, {3, 4}}
	if len(got) != len(want) {
		t.Fatalf("anchors = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("anchors = %v, want %v", got, want)
		}
	}

	// A narrow average lands on each anchor and keeps one to one
	// past the ends.
	for _, tc := range []struct{ asm, src float32 }{
		{-1, 1}, // before the first anchor
		{0, 2},  // on anchors
		{1, 3},
		{3, 4},
		{6, 7}, // past the last anchor
	} {
		if got := followRow(got, anchorAsm, anchorSrc, tc.asm, 0.3); math.Abs(float64(got-tc.src)) > 0.01 {
			t.Errorf("asm row %v follows to source row %v, want %v", tc.asm, got, tc.src)
		}
		if back := followRow(got, anchorSrc, anchorAsm, tc.src, 0.3); math.Abs(float64(back-tc.asm)) > 0.01 {
			t.Errorf("source row %v follows to asm row %v, want %v", tc.src, back, tc.asm)
		}
	}

	// A wide average moves steadily: each row down in the assembly
	// moves the source down, by no more than the anchors' steepest
	// step, without jumps.
	prev := followRow(got, anchorAsm, anchorSrc, -2, 3)
	for y := float32(-1.9); y < 6; y += 0.1 {
		next := followRow(got, anchorAsm, anchorSrc, y, 3)
		if step := next - prev; step <= 0 || step > 0.2 {
			t.Fatalf("source moved by %v rows at asm row %v", step, y)
		}
		prev = next
	}
}
