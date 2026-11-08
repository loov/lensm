package codeview

import (
	"image"
	"testing"

	"gioui.org/layout"

	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
)

func TestReveal(t *testing.T) {
	insts := make([]disasm.Inst, 100)
	for i := range insts {
		insts[i].PC = uint64(0x1000 + 4*i)
	}
	lines := make([]string, 100)
	ui := &UI{Code: &disasm.Code{
		Insts:  insts,
		Source: []disasm.Source{{File: "main.go", Blocks: []disasm.SourceBlock{{From: 10, Lines: lines}}}},
	}}
	gtx := layout.Context{Constraints: layout.Exact(image.Pt(800, 200))}

	ui.Reveal(comments.Coord{View: comments.ViewNativeAsm, PC: 0x1000 + 4*50})
	ui.applyReveal(gtx, 10)
	if ui.SelectedAsm != 50 || ui.SelectedView != ViewNativeAsm || ui.asm.Offset != 100-500-5 {
		t.Errorf("asm reveal: selected %d %q, offset %v", ui.SelectedAsm, ui.SelectedView, ui.asm.Offset)
	}

	// Row 0 is the file header, so line 10 is row 1. Near the top the
	// offset clamps to 0.
	ui.Reveal(comments.Coord{View: comments.ViewSource, File: "main.go", Line: 10})
	ui.applyReveal(gtx, 10)
	if ui.SelectedView != ViewSource || ui.SelectedLine != 10 || ui.src.Offset != 0 {
		t.Errorf("source reveal: %q line %d, offset %v", ui.SelectedView, ui.SelectedLine, ui.src.Offset)
	}
	ui.Reveal(comments.Coord{View: comments.ViewSource, File: "main.go", Line: 70})
	ui.applyReveal(gtx, 10)
	if ui.src.Offset != 100-610-5 {
		t.Errorf("source reveal line 70: offset %v", ui.src.Offset)
	}
}
