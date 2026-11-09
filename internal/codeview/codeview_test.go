package codeview

import (
	"image"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/syntax"
)

func TestCodeUIStyleLayoutWithHelpAndSelection(t *testing.T) {
	base := material.NewTheme()
	base.Shaper = text.NewShaper(text.WithCollection(gui.LoadFonts("")))
	base.TextSize = unit.Sp(12)
	theme := gui.NewTheme(base, false)
	state := &UI{Code: &disasm.Code{
		Name:    "main.example",
		File:    "main.go",
		MaxJump: 1,
		Insts: []disasm.Inst{
			{Text: "MOV (R2), R1", NativeText: "mov (%r2), %r1"},
			{Text: "ADDQ $1, R1", NativeText: "addq $1, %r1"},
		},
		Source: []disasm.Source{{
			File: "main.go",
			Blocks: []disasm.SourceBlock{{
				From: 1, To: 2,
				Lines: []string{"func example() {", "}"},
			}},
		}},
	}}
	state.mousePosition = f32.Pt(100, 22)

	var router input.Router
	var operations op.Ops
	newContext := func() layout.Context {
		operations.Reset()
		return layout.Context{
			Ops:         &operations,
			Source:      router.Source(),
			Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
			Now:         time.Now(),
			Constraints: layout.Exact(image.Pt(1200, 600)),
		}
	}
	copied := ""
	style := Style{
		UI:         state,
		Theme:      theme,
		Syntax:     syntax.PaletteFor(syntax.StyleGoLand, SyntaxColors(theme.Colors)),
		ShowGo:     true,
		ShowNative: true,
		ShowSource: true,
		TextHeight: theme.TextSize,
		CopyText: func(_ layout.Context, text string) {
			copied = text
		},
	}
	gtx := newContext()
	if got := style.Layout(gtx).Size; got != gtx.Constraints.Max {
		t.Fatalf("Layout size = %v, want %v", got, gtx.Constraints.Max)
	}
	router.Frame(gtx.Ops)

	router.Queue(
		pointer.Event{Source: pointer.Mouse, PointerID: 1, Buttons: pointer.ButtonPrimary, Kind: pointer.Press, Position: f32.Pt(100, 20)},
		pointer.Event{Source: pointer.Mouse, PointerID: 1, Buttons: pointer.ButtonPrimary, Kind: pointer.Move, Position: f32.Pt(100, 40)},
		pointer.Event{Source: pointer.Mouse, PointerID: 1, Buttons: pointer.ButtonPrimary, Kind: pointer.Release, Position: f32.Pt(100, 40)},
	)
	gtx = newContext()
	style.Layout(gtx)
	router.Frame(gtx.Ops)
	if from, to, ok := state.Selection.Range(); !ok || from != 0 || to != 1 || state.Selection.View != ViewGoAsm {
		t.Fatalf("drag selection = %#v, range %d..%d", state.Selection, from, to)
	}
	if !gtx.Focused(state) {
		t.Fatal("code view did not retain keyboard focus after drag selection")
	}

	router.Queue(key.Event{Name: key.Name("C"), Modifiers: key.ModShortcut, State: key.Press})
	gtx = newContext()
	style.Layout(gtx)
	router.Frame(gtx.Ops)
	if want := "MOV (R2), R1\nADDQ $1, R1\n"; copied != want {
		t.Fatalf("copied text = %q, want %q", copied, want)
	}

	// A fast drag past the bottom edge must clamp to the last line, not
	// stop at the last in-range drag sample.
	router.Queue(
		pointer.Event{Source: pointer.Mouse, PointerID: 2, Buttons: pointer.ButtonPrimary, Kind: pointer.Press, Position: f32.Pt(100, 20)},
		pointer.Event{Source: pointer.Mouse, PointerID: 2, Buttons: pointer.ButtonPrimary, Kind: pointer.Move, Position: f32.Pt(100, 5000)},
		pointer.Event{Source: pointer.Mouse, PointerID: 2, Buttons: pointer.ButtonPrimary, Kind: pointer.Release, Position: f32.Pt(100, 5000)},
	)
	gtx = newContext()
	style.Layout(gtx)
	router.Frame(gtx.Ops)
	if from, to, ok := state.Selection.Range(); !ok || from != 0 || to != len(state.Code.Insts)-1 {
		t.Fatalf("overshoot drag selection = %#v, range %d..%d", state.Selection, from, to)
	}
}

func TestAlignCounterpart(t *testing.T) {
	// Line 2 compiles to two runs, instructions 1 and 3.
	state := &UI{Code: &disasm.Code{
		Insts: []disasm.Inst{
			{Text: "A", File: "a.go", Line: 1},
			{Text: "B", File: "a.go", Line: 2},
			{Text: "C", File: "a.go", Line: 3},
			{Text: "D", File: "a.go", Line: 2},
		},
		Source: []disasm.Source{{
			File:   "a.go",
			Blocks: []disasm.SourceBlock{{From: 1, To: 3, Lines: []string{"a", "b", "c"}}},
		}},
	}}
	gtx := layout.Context{
		Ops:         new(op.Ops),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Now:         time.Now(),
		Constraints: layout.Exact(image.Pt(1200, 600)),
	}
	style := Style{UI: state, ShowGo: true, ShowSource: true, TextHeight: 12}
	c := style.columns(gtx)
	lh := float32(c.lineHeight)
	finish := func(region *scrollRegion) float32 {
		gtx.Now = gtx.Now.Add(time.Second)
		if scroll, ok := region.Anim.Update(gtx); ok {
			region.Offset = scroll
		}
		return region.Offset
	}

	// The header is source row 0, so line 3 is row 3; instruction C
	// is row 2 in the assembly.
	style.alignCounterpart(gtx, c, f32.Pt(c.asm.Min+1, 2*lh+1))
	if got := finish(&state.src); got != -lh {
		t.Errorf("source offset = %v, want line 3 beside instruction C at %v", got, -lh)
	}

	// Pressing line 2 steps through its runs: B, then D, then B again.
	state.src.Offset = 0
	at := f32.Pt(c.source.Min+1, 2*lh+1)
	for _, want := range []int{1, 3, 1} {
		style.alignCounterpart(gtx, c, at)
		if got := finish(&state.asm); got != 2*lh-float32(want)*lh {
			t.Errorf("assembly offset = %v, want instruction %d beside line 2", got, want)
		}
	}
}
