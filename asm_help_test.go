package main

import "testing"

func TestAssemblyInstructionExplanations(t *testing.T) {
	tests := []struct {
		arch        string
		instruction string
		want        string
	}{
		{"arm64", "MOV (R2), R1", "R1 := memory[R2]"},
		{"amd64", "ADDQ $1, R1", "R1 := R1 + 1"},
		{"amd64", "LEAQ 0(R1)(R2*4), R3", "R3 := R1 + R2 * 4"},
		{"arm64", "FMADDS F0, F2, F1, F2", "F2 := F0 * F1 + F2"},
		// x86 Go assembly keeps CMP operands in natural order: flags are
		// computed from AX - 0x10 (go.dev/issue/60920).
		{"amd64", "CMPQ AX, $0x10", "flags := compare(AX, 0x10)"},
		// arm64 Go assembly is source-first: CMP R1, R4 is R4 - R1.
		{"arm64", "CMP R1, R4", "flags := compare(R4, R1)"},
		// Three-operand arm64 forms compute dst := second op first.
		{"arm64", "SUB R1, R5, R3", "R3 := R5 - R1"},
		{"arm64", "LSL $3, R5, R3", "R3 := R5 << 3"},
	}
	for _, test := range tests {
		help, ok := AssemblyInstructionHelp(test.arch, test.instruction)
		if !ok {
			t.Fatalf("no help for %q", test.instruction)
		}
		if help.Explanation != test.want {
			t.Errorf("explanation for %q (%s) = %q, want %q", test.instruction, test.arch, help.Explanation, test.want)
		}
	}
}

func TestAssemblyInstructionReference(t *testing.T) {
	help, ok := AssemblyInstructionHelp("amd64", "JNE 12(PC)")
	if !ok || help.Description == "" {
		t.Fatalf("JNE help = %#v, %v", help, ok)
	}
	if help.Explanation != "" {
		t.Fatalf("unexpected JNE explanation: %q", help.Explanation)
	}
}
