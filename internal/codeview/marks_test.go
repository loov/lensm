package codeview

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/unit"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/syntax"
)

func TestMarksLane(t *testing.T) {
	code := &disasm.Code{Insts: []disasm.Inst{{Text: "MOVQ AX, 8(SP)", Marks: disasm.MarkSpill}, {Text: "RET"}}}
	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1200, 600)),
	}
	width := func(marks disasm.Mark) float32 {
		ui := Style{UI: &UI{Code: code}, ShowGo: true, ShowNative: true, ShowSource: true, Marks: marks, TextHeight: 12}
		return ui.columns(gtx).marks.Width()
	}
	if width(disasm.AllMarks) == 0 {
		t.Error("no marks lane for a spill")
	}
	if w := width(disasm.AllMarks &^ disasm.MarkSpill); w != 0 {
		t.Errorf("marks lane is %v wide with spills hidden", w)
	}
}

func TestSourceMarksLane(t *testing.T) {
	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1200, 600)),
	}
	state := &UI{Code: &disasm.Code{Insts: []disasm.Inst{{Text: "RET"}}}}
	// Line 1 has one spill, line 2 a reload and an allocation.
	state.hl.lineMarks = map[sourcePos]*lineMarks{
		{"a.go", 1}: {disasm.MarkSpill, markCounts{1, 0, 0, 0, 0}},
		{"a.go", 2}: {disasm.MarkReload | disasm.MarkAlloc, markCounts{0, 1, 0, 1, 0}},
	}
	columns := func(marks disasm.Mark) codeColumns {
		return Style{UI: state, ShowGo: true, ShowNative: true, ShowSource: true, Marks: marks, TextHeight: 12}.columns(gtx)
	}
	c := columns(disasm.AllMarks)
	if c.sourceMarkSlots != disasm.MarkSpill|disasm.MarkReload|disasm.MarkAlloc {
		t.Errorf("slots = %b", c.sourceMarkSlots)
	}
	if want := float32(3 * c.lineHeight); c.sourceMarks.Width() != want {
		t.Errorf("source lane is %v wide, want %v", c.sourceMarks.Width(), want)
	}
	if c.sourceMarks.Max != c.source.Max || c.sourceCol.textLeft+c.sourceCol.textWidth != int(c.source.Max) {
		t.Errorf("source text %+v does not run under lane %+v", c.sourceCol, c.sourceMarks)
	}
	// A line with only an allocation keeps the spill and reload cells.
	if got, want := c.sourceMarksLeft(disasm.MarkAlloc), int(c.sourceMarks.Min)+2*c.lineHeight; got != want {
		t.Errorf("allocation cell starts at %d, want %d", got, want)
	}
	if got := c.sourceMarksLeft(0); got != int(c.source.Max) {
		t.Errorf("unmarked line stops at %d, want %v", got, c.source.Max)
	}
	if c := columns(disasm.MarkBoundsCheck); c.sourceMarks.Width() != 0 {
		t.Errorf("source lane is %v wide with no shown marks", c.sourceMarks.Width())
	}
}

func TestSourceMarksLaneWidensForCounts(t *testing.T) {
	gtx := layout.Context{
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1200, 600)),
	}
	state := &UI{Code: &disasm.Code{Insts: []disasm.Inst{{Text: "RET"}}}}
	// Two spills on one line, one bounds check on another.
	state.hl.lineMarks = map[sourcePos]*lineMarks{
		{"a.go", 1}: {disasm.MarkSpill, markCounts{2, 0, 0, 0, 0}},
		{"a.go", 2}: {disasm.MarkBoundsCheck, markCounts{0, 0, 1, 0, 0}},
	}
	style := Style{UI: state, ShowGo: true, ShowNative: true, ShowSource: true, TextHeight: 12}
	style.Marks = disasm.AllMarks
	c := style.columns(gtx)
	if c.markWidth(disasm.MarkSpill) <= c.lineHeight {
		t.Errorf("spill cell is %d wide with two spills on a line", c.markWidth(disasm.MarkSpill))
	}
	if c.markWidth(disasm.MarkBoundsCheck) != c.lineHeight {
		t.Errorf("bounds cell is %d wide with one per line, want %d", c.markWidth(disasm.MarkBoundsCheck), c.lineHeight)
	}
	if want := float32(c.markWidth(disasm.MarkSpill) + c.lineHeight); c.sourceMarks.Width() != want {
		t.Errorf("source lane is %v wide, want %v", c.sourceMarks.Width(), want)
	}
}

func TestBoundsCheckCountsOnce(t *testing.T) {
	// A branch into the failure block and the failure call, one check.
	code := &disasm.Code{Insts: []disasm.Inst{
		{Text: "BCS 3(PC)", File: "a.go", Line: 1, RefOffset: 2, Marks: disasm.MarkBoundsCheck},
		{Text: "RET", File: "a.go", Line: 1},
		{Text: "CALL runtime.panicBounds(SB)", File: "a.go", Line: 1, Call: "runtime.panicBounds", Marks: disasm.MarkBoundsCheck},
	}}
	var hl highlightCache
	hl.update(code, syntax.Palette{})
	line := hl.lineMarks[sourcePos{"a.go", 1}]
	if line == nil || line.marks != disasm.MarkBoundsCheck || line.counts[2] != 1 {
		t.Errorf("line marks = %+v, want one bounds check", line)
	}
}
