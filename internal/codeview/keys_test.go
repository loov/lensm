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
		style.navigate(gtx, c, name)
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
}
