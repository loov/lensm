package codeview

import (
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

	for _, tc := range []struct{ asm, src float32 }{
		{-1, 1},  // before the first anchor, one to one
		{0, 2},   // on an anchor
		{2, 3.5}, // halfway between instructions 1 and 3
		{5, 6},   // past the last anchor, one to one
	} {
		if got := followRow(got, anchorAsm, anchorSrc, tc.asm); got != tc.src {
			t.Errorf("asm row %v follows to source row %v, want %v", tc.asm, got, tc.src)
		}
		if back := followRow(got, anchorSrc, anchorAsm, tc.src); back != tc.asm {
			t.Errorf("source row %v follows to asm row %v, want %v", tc.src, back, tc.asm)
		}
	}
}
