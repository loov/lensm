package main

import (
	"testing"

	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
)

func TestFindMatches(t *testing.T) {
	code := &disasm.Code{
		Insts: []disasm.Inst{
			{PC: 1, Text: "MOVQ AX, BX", NativeText: "mov rbx, rax"},
			{PC: 2, Text: "CALL runtime.panicIndex", NativeText: "call runtime.panicIndex"},
			{PC: 3, Text: "RET", NativeText: "ret"},
		},
		Source: []disasm.Source{{File: "a.go", Blocks: []disasm.SourceBlock{{
			LineRange: disasm.LineRange{From: 10, To: 11},
			Lines:     []string{"x := a[i]", "panic(1)"},
		}}}},
	}
	got := findMatches(code, "PANIC", true, true, true)
	want := []comments.Coord{
		{View: comments.ViewGoAsm, PC: 2},
		{View: comments.ViewSource, File: "a.go", Line: 11},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("matches = %+v, want %+v", got, want)
	}
	// Only the native syntax spells rbx in lower case with the r.
	if got := findMatches(code, "rbx", false, true, false); len(got) != 1 || got[0].View != comments.ViewNativeAsm {
		t.Errorf("native matches = %+v", got)
	}
	if got := findMatches(code, "rbx", true, false, false); len(got) != 0 {
		t.Errorf("hidden native column matched: %+v", got)
	}
}
