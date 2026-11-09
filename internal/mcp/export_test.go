package mcp

import (
	"testing"

	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
)

// TestBuildFunctionCodeDTOStatements checks that statements come in
// their own field, not as native assembly.
func TestBuildFunctionCodeDTOStatements(t *testing.T) {
	code := &disasm.Code{Name: "f", Arch: "wasm", Statements: true, Insts: []disasm.Inst{
		{PC: 1, Text: "local.get 0"},
		{PC: 2, Text: "local.set 1", NativeText: "l1 = p0"},
	}}
	dto := BuildFunctionCodeDTO("m.wasm", code, comments.NewMemory("m.wasm"), ExportOptions{GoAsm: true, NativeAsm: true})
	if len(dto.Asm) != 2 {
		t.Fatalf("got %d rows", len(dto.Asm))
	}
	if got := dto.Asm[1]; got.Statement != "l1 = p0" || got.Native != "" {
		t.Errorf("got statement %q, native %q", got.Statement, got.Native)
	}
}
