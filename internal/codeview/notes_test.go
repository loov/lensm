package codeview

import (
	"image"
	"strings"
	"testing"

	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/syntax"
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
	for row := range sourceRows(code, noteLayout{kinds: disasm.NoteInline | disasm.NoteEscape}) {
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
	if n := sourceRowCount(code, noteLayout{}); n != 3 {
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

func TestWrapText(t *testing.T) {
	width := func(n int) func(string) bool {
		return func(s string) bool { return len([]rune(s)) <= n }
	}
	check := func(got, want []string) {
		t.Helper()
		if strings.Join(got, "|") != strings.Join(want, "|") {
			t.Errorf("wrapText = %q, want %q", got, want)
		}
	}
	check(wrapText("x escapes to heap ← y", width(10)), []string{"x escapes", "to heap ←", "y"})
	check(wrapText("abcdefghij", width(4)), []string{"abcd", "efgh", "ij"})
	check(wrapText("short", width(10)), []string{"short"})
	check(wrapText("", width(10)), []string{""})
}

func TestPlaceNotes(t *testing.T) {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1200, 600)),
	}
	short := disasm.Note{Code: "escapes", Col: 7, Message: "x escapes to heap"}
	long := disasm.Note{Code: "escapes", Col: 7, Message: strings.Repeat("word ", 60)}
	code := &disasm.Code{Source: []disasm.Source{{
		File: "a.go",
		Blocks: []disasm.SourceBlock{{
			LineRange: disasm.LineRange{From: 1, To: 2},
			Lines:     []string{"    f(a, x)"},
			Notes:     [][]disasm.Note{{short, long}},
		}},
	}}}
	ui := Style{UI: &UI{Code: code, Notes: disasm.NoteEscape}, Theme: &gui.Theme{Theme: th}, ShowSource: true, TextHeight: 12}
	ui.hl.update(code, syntax.Palette{})
	c := ui.columns(gtx)
	ui.placeNotes(gtx, c)

	// Column 7 is x: the tab counts once but is drawn as four spaces.
	prefix := ui.hl.source[0][0][0][0].Text + "    f(a, "
	notes := code.Source[0].Blocks[0].Notes[0]
	place := ui.notePlaces[&notes[0]]
	if want := glyphWidth(gtx, th, sourceLine{Text: prefix, TextHeight: 12}); place.x != want {
		t.Errorf("note starts at %d, want %d", place.x, want)
	}
	if len(place.rows) != 1 {
		t.Errorf("short note wraps to %q", place.rows)
	}
	wrapped := ui.notePlaces[&notes[1]]
	room := int(c.source.Width()) - wrapped.x - noteGap(c)
	if len(wrapped.rows) < 2 {
		t.Fatalf("long note did not wrap: %q", wrapped.rows)
	}
	for _, row := range wrapped.rows {
		if w := glyphWidth(gtx, th, sourceLine{Text: row, TextHeight: 12 * noteScale, Italic: true}); w > room {
			t.Errorf("row %q is %d wide, more than %d", row, w, room)
		}
	}
	if n := sourceRowCount(code, ui.noteRows()); n != 2+1+len(wrapped.rows) {
		t.Errorf("%d rows, want the header, the line and %d note rows", n, 1+len(wrapped.rows))
	}
}
