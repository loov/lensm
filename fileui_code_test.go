package main

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
)

func TestCodeUIStyleLayoutWithHelpAndSelection(t *testing.T) {
	theme := material.NewTheme()
	theme.Shaper = text.NewShaper(text.WithCollection(LoadFonts("")))
	theme.TextSize = unit.Sp(12)
	colors := ApplyTheme(theme, false)
	state := &CodeUI{Code: &disasm.Code{
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
				LineRange: disasm.LineRange{From: 1, To: 2},
				Lines:     []string{"func example() {", "}"},
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
	style := CodeUIStyle{
		CodeUI:     state,
		Theme:      theme,
		Colors:     colors,
		Syntax:     SyntaxPaletteFor(SyntaxStyleGoLand, colors),
		ShowNative: true,
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
	if from, to, ok := state.Selection.Range(); !ok || from != 0 || to != 1 || state.Selection.View != CodeViewGoAsm {
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
}
