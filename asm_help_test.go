package main

import "testing"

func TestAssemblyInstructionExplanations(t *testing.T) {
	tests := []struct {
		instruction string
		want        string
	}{
		{"MOV (R2), R1", "R1 := memory[R2]"},
		{"ADDQ $1, R1", "R1 := R1 + 1"},
		{"LEAQ 0(R1)(R2*4), R3", "R3 := R1 + R2 * 4"},
		{"FMADDS F0, F2, F1, F2", "F2 := F0 * F1 + F2"},
	}
	for _, test := range tests {
		help, ok := AssemblyInstructionHelp(test.instruction)
		if !ok {
			t.Fatalf("no help for %q", test.instruction)
		}
		if help.Explanation != test.want {
			t.Errorf("explanation for %q = %q, want %q", test.instruction, help.Explanation, test.want)
		}
	}
}

func TestAssemblyInstructionReference(t *testing.T) {
	help, ok := AssemblyInstructionHelp("JNE 12(PC)")
	if !ok || help.Description == "" {
		t.Fatalf("JNE help = %#v, %v", help, ok)
	}
	if help.Explanation != "" {
		t.Fatalf("unexpected JNE explanation: %q", help.Explanation)
	}
}
