package goobj

import (
	"testing"

	"loov.dev/lensm/internal/disasm"
)

func TestLayoutJumps(t *testing.T) {
	code := &disasm.Code{Insts: make([]disasm.Inst, 100)}
	pcToIndex := map[uint64]int{}
	for i := range code.Insts {
		code.Insts[i].PC = uint64(4 * (i + 1))
		pcToIndex[code.Insts[i].PC] = i
	}
	jump := func(from, to int) { code.Insts[from].RefPC = code.Insts[to].PC }
	// Two near jumps to one loop header.
	jump(10, 2)
	jump(12, 2)
	// Two far jumps to one exit.
	jump(5, 90)
	jump(8, 90)
	layoutJumps(code, pcToIndex)

	ix := code.Insts
	if ix[10].RefStack != ix[12].RefStack || ix[10].RefStack == 0 {
		t.Errorf("near jumps to one target got columns %d and %d, want one", ix[10].RefStack, ix[12].RefStack)
	}
	if ix[10].RefFar || !ix[5].RefFar || !ix[8].RefFar {
		t.Errorf("far flags: near %v, far %v %v", ix[10].RefFar, ix[5].RefFar, ix[8].RefFar)
	}
	if ix[90].InStack == 0 || ix[5].RefStack == 0 {
		t.Errorf("far stubs got columns %d and %d", ix[5].RefStack, ix[90].InStack)
	}
	// The stubs fit next to the code beside the loop line.
	if code.MaxJump != 2 {
		t.Errorf("MaxJump = %d, want 2", code.MaxJump)
	}
	if ix[5].RefStack != 1 {
		t.Errorf("stub at row 5 in column %d, want the innermost", ix[5].RefStack)
	}
}
