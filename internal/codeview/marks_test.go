package codeview

import (
	"image"
	"testing"

	"gioui.org/layout"
	"gioui.org/unit"

	"loov.dev/lensm/internal/disasm"
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
	state.hl.lineMarks = map[sourcePos]disasm.Mark{
		{"a.go", 1}: disasm.MarkSpill,
		{"a.go", 2}: disasm.MarkReload | disasm.MarkAlloc,
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
	if c.sourceMarks.Max != c.source.Max || c.sourceCol.textLeft+c.sourceCol.textWidth > int(c.sourceMarks.Min) {
		t.Errorf("source text %+v overlaps lane %+v", c.sourceCol, c.sourceMarks)
	}
	if c := columns(disasm.MarkBoundsCheck); c.sourceMarks.Width() != 0 {
		t.Errorf("source lane is %v wide with no shown marks", c.sourceMarks.Width())
	}
}
