package disasm

import "testing"

func TestLayoutJumps(t *testing.T) {
	code := &Code{Insts: make([]Inst, 100)}
	jump := func(from, to int) { code.Insts[from].RefOffset = to - from }
	// Two near jumps to one loop header.
	jump(10, 2)
	jump(12, 2)
	// Two far jumps to one exit.
	jump(5, 90)
	jump(8, 90)
	code.LayoutJumps(40)

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

	// A taller view draws the long jumps whole.
	code.LayoutJumps(100)
	if ix[5].RefFar || ix[90].InStack != 0 {
		t.Errorf("with far 100: RefFar %v, InStack %d", ix[5].RefFar, ix[90].InStack)
	}
	if ix[5].RefStack != ix[8].RefStack {
		t.Errorf("jumps to one exit got columns %d and %d, want one", ix[5].RefStack, ix[8].RefStack)
	}
}

func TestLayoutJumpsTrimmedTarget(t *testing.T) {
	code := &Code{Insts: make([]Inst, 10)}
	code.Insts[2].RefOffset = 20
	code.Insts[3].RefOffset = -5
	code.LayoutJumps(40)
	if code.Insts[2].RefOffset != 0 || code.Insts[3].RefOffset != 0 {
		t.Errorf("jumps out of the code kept offsets %d and %d", code.Insts[2].RefOffset, code.Insts[3].RefOffset)
	}
}
