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
