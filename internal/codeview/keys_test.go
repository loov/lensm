package codeview

import (
	"image"
	"testing"
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"loov.dev/lensm/internal/disasm"
)

func TestNavigate(t *testing.T) {
	insts := make([]disasm.Inst, 200)
	for i := range insts {
		insts[i].Text = "NOP"
	}
	insts[3].Text = "" // a spacer
	insts[4].RefOffset = 100
	insts[5].Call = "main.f"
	insts[5].Text = "CALL main.f"
	state := &UI{Code: &disasm.Code{Insts: insts}, SelectedAsm: -1}
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Now:         time.Now(),
		Constraints: layout.Exact(image.Pt(1200, 600)),
	}
	opened := ""
	style := Style{UI: state, ShowGo: true, TextHeight: 12,
		TryOpen: func(_ layout.Context, name string) { opened = name }}
	c := style.columns(gtx)
	press := func(name key.Name, want int) {
		t.Helper()
		style.navigate(gtx, c, name, 0)
		if state.SelectedAsm != want {
			t.Fatalf("after %s selected %d, want %d", name, state.SelectedAsm, want)
		}
	}

	press(key.NameDownArrow, 0)
	press(key.NameDownArrow, 1)
	press(key.NameDownArrow, 2)
	press(key.NameDownArrow, 4) // past the spacer
	press(key.NameUpArrow, 2)
	press(key.NameDownArrow, 4)

	press(key.NameRightArrow, 104)
	press(key.NameLeftArrow, 4)
	press(key.NameLeftArrow, 4) // nothing to return to

	press(key.NameDownArrow, 5)
	press(key.NameRightArrow, 5)
	if opened != "main.f" {
		t.Errorf("→ on a call opened %q, want main.f", opened)
	}

	press(key.NameEnd, 199)
	if top := state.asm.Offset + float32(199*c.lineHeight); top < 0 || top+float32(c.lineHeight) > 600 {
		t.Errorf("last row at %v is out of view", top)
	}
	press(key.NameHome, 0)
	if state.asm.Offset != 0 {
		t.Errorf("offset after Home = %v, want 0", state.asm.Offset)
	}
	rows := 600 / c.lineHeight
	press(key.NamePageDown, rows-1)

	// Shift extends a selection from the row it started on.
	press(key.NameHome, 0)
	style.navigate(gtx, c, key.NameDownArrow, key.ModShift)
	style.navigate(gtx, c, key.NameDownArrow, key.ModShift)
	if from, to, ok := state.Selection.Range(); !ok || from != 0 || to != 2 || state.Selection.View != ViewGoAsm {
		t.Errorf("shift selection = %d..%d %v %q", from, to, ok, state.Selection.View)
	}
	press(key.NameDownArrow, 4)
	if state.Selection.Active {
		t.Error("a plain arrow kept the selection")
	}
}

func TestNavigateSource(t *testing.T) {
	insts := []disasm.Inst{
		{Text: "A", File: "a.go", Line: 10},
		{Text: "B", File: "a.go", Line: 12},
	}
	state := &UI{Code: &disasm.Code{
		Insts: insts,
		Source: []disasm.Source{{File: "a.go", Blocks: []disasm.SourceBlock{
			{LineRange: disasm.LineRange{From: 10, To: 13}, Lines: []string{"a", "b", "c"}},
		}}},
	}, SelectedAsm: 1, SelectedView: ViewGoAsm}
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(1200, 600)),
	}
	style := Style{UI: state, ShowGo: true, ShowSource: true, TextHeight: 12}
	c := style.columns(gtx)
	at := func(file string, line int) {
		t.Helper()
		if state.SelectedView != ViewSource || state.SelectedFile != file || state.SelectedLine != line {
			t.Fatalf("selected %q %s:%d, want source %s:%d", state.SelectedView, state.SelectedFile, state.SelectedLine, file, line)
		}
	}

	// Tab goes to the selected instruction's line, past the header row.
	style.navigate(gtx, c, key.NameTab, 0)
	at("a.go", 12)
	style.navigate(gtx, c, key.NameUpArrow, 0)
	at("a.go", 11)
	style.navigate(gtx, c, key.NameHome, 0)
	at("a.go", 10)
	style.navigate(gtx, c, key.NameEnd, 0)
	at("a.go", 12)
	style.navigate(gtx, c, key.NameUpArrow, key.ModShift)
	if from, to, ok := state.Selection.Range(); !ok || state.Selection.View != ViewSource || from != 2 || to != 3 {
		t.Errorf("shift selection in source = %d..%d %v", from, to, ok)
	}
	// And back to the line's first instruction.
	style.navigate(gtx, c, key.NameTab, 0)
	if state.SelectedView != ViewGoAsm || state.SelectedAsm != 0 {
		t.Errorf("after Tab back: %q %d", state.SelectedView, state.SelectedAsm)
	}
}
