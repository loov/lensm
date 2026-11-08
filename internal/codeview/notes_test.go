package codeview

import (
	"testing"

	"loov.dev/lensm/internal/disasm"
)

func TestNoteRows(t *testing.T) {
	code := &disasm.Code{Source: []disasm.Source{{
		File: "a.go",
		Blocks: []disasm.SourceBlock{{
			LineRange: disasm.LineRange{From: 1, To: 3},
			Lines:     []string{"func f() {", "    x := &T{}"},
			Notes: [][]disasm.Note{
				{{Code: "cannotInlineFunction", Message: "marked go:noinline"}},
				{{Code: "escapes", Message: "&T{} escapes to heap"}, {Code: "nilcheck"}},
			},
		}},
	}}}
	var got []string
	for row := range sourceRows(code, disasm.NoteInline|disasm.NoteEscape) {
		got = append(got, row.text)
	}
	want := []string{
		"// a.go",
		"func f() {",
		"// cannot inline: marked go:noinline",
		"    x := &T{}",
		"    // &T{} escapes to heap",
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
	if n := sourceRowCount(code, 0); n != 3 {
		t.Errorf("%d rows with notes hidden, want 3", n)
	}
}

func TestNoteColumn(t *testing.T) {
	// "\t\ttotal += a[i]" drawn with each tab as four spaces.
	line := "        total += a[i]"
	if got := noteColumn(line, 3); got != 8 {
		t.Errorf("column of total = %d, want 8", got)
	}
	if got := noteColumn(line, 12); got != 17 {
		t.Errorf("column of a = %d, want 17", got)
	}
	if got := noteColumn("func f()", 6); got != 5 {
		t.Errorf("column of f = %d, want 5", got)
	}
}
