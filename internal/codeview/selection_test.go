package codeview

import (
	"testing"

	"loov.dev/lensm/internal/disasm"
)

func TestTextSelectionAssembly(t *testing.T) {
	code := &disasm.Code{Insts: []disasm.Inst{
		{Text: "MOV (R2), R1", NativeText: "mov (%r2), %r1"},
		{Text: "ADDQ $1, R1", NativeText: "addq $1, %r1"},
		{Text: "RET", NativeText: "ret"},
	}}
	selection := TextSelection{View: ViewGoAsm, Anchor: 1, Head: 0, Active: true}
	if got, want := selection.Text(code), "MOV (R2), R1\nADDQ $1, R1\n"; got != want {
		t.Fatalf("Text() = %q, want %q", got, want)
	}
	selection.View = ViewNativeAsm
	if got, want := selection.Text(code), "MOV (%R2), %R1\nADDQ $1, %R1\n"; got != want {
		t.Fatalf("native Text() = %q, want %q", got, want)
	}
}

func TestTextSelectionSource(t *testing.T) {
	code := &disasm.Code{Source: []disasm.Source{{
		File:   "main.go",
		Blocks: []disasm.SourceBlock{{Lines: []string{"func main() {", "}"}}},
	}}}
	selection := TextSelection{View: ViewSource, Anchor: 0, Head: 2, Active: true}
	if got, want := selection.Text(code), "// main.go\nfunc main() {\n}\n"; got != want {
		t.Fatalf("Text() = %q, want %q", got, want)
	}
}

func TestSourceRowAtYRejectsSpaceAboveContent(t *testing.T) {
	code := &disasm.Code{Source: []disasm.Source{{File: "main.go"}}}
	if got := sourceRowAtY(code, 10, 20, 9); got != -1 {
		t.Fatalf("sourceRowAtY() = %d, want -1", got)
	}
	if got := sourceRowAtY(code, 10, 20, 10); got != 0 {
		t.Fatalf("sourceRowAtY() = %d, want 0", got)
	}
}
