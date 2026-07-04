package asmhelp

import (
	"strings"
	"testing"
)

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
		// arm64 sized moves and 32-bit variants must keep matching their
		// base rules.
		{"arm64", "MOVD (R1), R0", "R0 := memory[R1]"},
		{"arm64", "MOVWU (R1), R0", "R0 := memory[R1]"},
		{"arm64", "SDIVW R1, R2, R0", "R0 := R2 / R1"},
		{"arm64", "FMOVD F1, F0", "F0 := F1"},
		// MADD R2, R3, R1, R0 computes R0 = R1*R2 + R3.
		{"arm64", "MADD R2, R3, R1, R0", "R0 := R1 * R2 + R3"},
		{"arm64", "MSUB R2, R3, R1, R0", "R0 := R3 - R1 * R2"},
	}
	for _, test := range tests {
		help, ok := ForInstruction(test.arch, test.instruction)
		if !ok {
			t.Fatalf("no help for %q", test.instruction)
		}
		if help.Explanation != test.want {
			t.Errorf("explanation for %q (%s) = %q, want %q", test.instruction, test.arch, help.Explanation, test.want)
		}
	}
}

func TestAssemblyInstructionReference(t *testing.T) {
	help, ok := ForInstruction("amd64", "JNE 12(PC)")
	if !ok || help.Description == "" {
		t.Fatalf("JNE help = %#v, %v", help, ok)
	}
	if help.Explanation != "" {
		t.Fatalf("unexpected JNE explanation: %q", help.Explanation)
	}
}

func TestUnknownGoAssemblyInstructionHasFallbackReference(t *testing.T) {
	help, ok := ForInstruction("amd64", "VPERM2F128 $49, Y1, Y2, Y3")
	if !ok {
		t.Fatal("no fallback help for Go assembly instruction")
	}
	if help.Mnemonic != "VPERM2F128" || help.Description != "Execute the VPERM2F128 instruction." {
		t.Fatalf("fallback help = %#v", help)
	}
}

func TestGeneratedReferenceReplacesGenericFallback(t *testing.T) {
	// ABS and CRC32 are absent from the curated rules, so they used to get the
	// generic "Execute the X instruction." line. The generated asmref table now
	// supplies real reference text, while the bespoke Explanation stays empty
	// (no rule fabricates semantics for them).
	for _, tc := range []struct{ arch, instruction, want string }{
		{"arm64", "ABS V0.8B, V1.8B", "ABS (vector)"},
		{"amd64", "CRC32 AX, BL", "Accumulate CRC32 Value"},
	} {
		help, ok := ForInstruction(tc.arch, tc.instruction)
		if !ok {
			t.Fatalf("no help for %q", tc.instruction)
		}
		if help.Description != tc.want {
			t.Errorf("description for %q = %q, want %q", tc.instruction, help.Description, tc.want)
		}
		if help.Explanation != "" {
			t.Errorf("unexpected explanation for %q: %q", tc.instruction, help.Explanation)
		}
	}
}

func TestUndecodableInstructionHasNoFallback(t *testing.T) {
	// Undecodable bytes render as "?" in the Go column.
	if help, ok := ForInstruction("amd64", "?"); ok {
		t.Fatalf("unexpected fallback for undecodable instruction: %#v", help)
	}
	if help, ok := ForInstruction("amd64", "// pseudo"); ok {
		t.Fatalf("unexpected fallback for non-mnemonic token: %#v", help)
	}
}

func TestUnknownNativeAssemblyInstructionHasNoGoFallback(t *testing.T) {
	if help, ok := ForNative("unknownop %rax"); ok {
		t.Fatalf("unexpected native fallback: %#v", help)
	}
}

func TestNativeAssemblyInstructionHelpUsesNativeRewrite(t *testing.T) {
	help, ok := ForNative("addq $1, %rax")
	if !ok {
		t.Fatal("no native help for ADDQ")
	}
	if help.Explanation != "%rax := %rax + 1" {
		t.Fatalf("native explanation = %q", help.Explanation)
	}
}

func TestNativeARMAssemblyInstructionExplanation(t *testing.T) {
	help, ok := ForNative("add x0, x1, #8")
	if !ok {
		t.Fatal("no native help for ARM ADD")
	}
	if help.Explanation != "x0 := x1 + 8" {
		t.Fatalf("native ARM explanation = %q", help.Explanation)
	}
}

func TestNativeARMStoreInstructionExplanation(t *testing.T) {
	help, ok := ForNative("str x0, [sp, #16]")
	if !ok {
		t.Fatal("no native help for ARM STR")
	}
	if help.Explanation != "memory[sp + 16] := x0" {
		t.Fatalf("native ARM STR explanation = %q", help.Explanation)
	}
}

func TestNativeARMIndexedMemoryExplanations(t *testing.T) {
	tests := map[string]string{
		"str x30, [sp, #-112]!":     "sp := sp - 112; memory[sp] := x30",
		"ldr x0, [sp], #16":         "x0 := memory[sp]; sp := sp + 16",
		"stp x29, x30, [sp, #-16]!": "sp := sp - 16; memory[sp] := pair(x29, x30)",
	}
	for instruction, want := range tests {
		help, ok := ForNative(instruction)
		if !ok || help.Explanation != want {
			t.Errorf("%q explanation = %q, want %q", instruction, help.Explanation, want)
		}
	}
}

func TestNativeDirectJumpHasHelp(t *testing.T) {
	// x86 GNU syntax spells direct jumps jmpq.
	help, ok := ForNative("jmpq .+0x100")
	if !ok {
		t.Fatal("no native help for jmpq")
	}
	if help.Explanation != "PC := .+0x100" {
		t.Fatalf("jmpq explanation = %q", help.Explanation)
	}
}

func TestNativeARMTwoOperandNeg(t *testing.T) {
	help, ok := ForNative("neg x0, x1")
	if !ok {
		t.Fatal("no native help for neg")
	}
	if help.Explanation != "x0 := -x1" {
		t.Fatalf("neg explanation = %q", help.Explanation)
	}
	help, ok = ForNative("mvn x0, x1")
	if !ok {
		t.Fatal("no native help for mvn")
	}
	if help.Explanation != "x0 := ^x1" {
		t.Fatalf("mvn explanation = %q", help.Explanation)
	}
}

func TestNativeARMUnsignedConditionalBranchIsNotCall(t *testing.T) {
	help, ok := ForNative("b.ls .+0x1bc")
	if !ok {
		t.Fatal("no native help for B.LS")
	}
	if help.Description != "Conditional jump after an unsigned comparison." {
		t.Fatalf("B.LS description = %q", help.Description)
	}
	if help.Explanation != "if unsigned lower than or equal (C == 0 or Z == 1), PC := .+0x1bc" {
		t.Fatalf("B.LS explanation = %q", help.Explanation)
	}
}

func TestAssemblyHelpRuleTableHasNoMnemonicCollisions(t *testing.T) {
	type owner struct {
		description string
		prefix      string
	}
	exact := make(map[string]owner)
	for _, rule := range asmInstructionRules {
		for _, prefix := range rule.Prefixes {
			if previous, exists := exact[prefix]; exists {
				t.Errorf("duplicate mnemonic %s in %s and %s", prefix, previous.prefix, prefix)
			}
			exact[prefix] = owner{description: rule.Description, prefix: prefix}
		}
	}

	for _, rule := range asmInstructionRules {
		for _, prefix := range rule.Prefixes {
			for suffix := range nativeSizeSuffixes(prefix) {
				mnemonic := prefix + suffix
				if exactOwner, isExactMnemonic := exact[mnemonic]; isExactMnemonic {
					if help, ok := ForInstruction("", mnemonic); !ok || help.Description != exactOwner.description {
						t.Errorf("exact %s was captured as a suffix of %s", mnemonic, prefix)
					}
					continue
				}
				help, ok := ForInstruction("", mnemonic)
				if !ok || help.Description != rule.Description {
					t.Errorf("%s resolved to %#v; want rule for %s", mnemonic, help, prefix)
				}
			}
		}
	}
}

func TestAllNativeARMConditionBranchesResolveAsBranches(t *testing.T) {
	conditions := map[string]string{
		"eq": "if the compared values are equal (Z == 1), PC := .+4",
		"ne": "if the compared values are not equal (Z == 0), PC := .+4",
		"gt": "if signed greater than (Z == 0 and N == V), PC := .+4",
		"ge": "if signed greater than or equal (N == V), PC := .+4",
		"lt": "if signed less than (N != V), PC := .+4",
		"le": "if signed less than or equal (Z == 1 or N != V), PC := .+4",
		"hi": "if unsigned higher than (C == 1 and Z == 0), PC := .+4",
		"hs": "if unsigned higher than or equal (C == 1), PC := .+4",
		"cs": "if unsigned higher than or equal (C == 1), PC := .+4",
		"lo": "if unsigned lower than (C == 0), PC := .+4",
		"cc": "if unsigned lower than (C == 0), PC := .+4",
		"ls": "if unsigned lower than or equal (C == 0 or Z == 1), PC := .+4",
	}
	for condition, want := range conditions {
		instruction := "b." + condition + " .+4"
		help, ok := ForNative(instruction)
		if !ok {
			t.Errorf("no help for %s", instruction)
			continue
		}
		if strings.Contains(help.Description, "Call a function") {
			t.Errorf("%s incorrectly resolved as call: %#v", instruction, help)
		}
		if help.Explanation != want {
			t.Errorf("%s effect = %q, want %q", instruction, help.Explanation, want)
		}
	}
}

func TestAssemblyInstructionReferenceCoverage(t *testing.T) {
	for _, instruction := range []string{
		"movzbl (%rax), %eax", "cmovne %rax, %rbx", "sete %al",
		"cqto", "syscall", "mfence", "adrp x0, 0x1000", "stp x0, x1, [sp]",
	} {
		if help, ok := ForNative(instruction); !ok || help.Description == "" || help.Explanation == "" {
			t.Errorf("no native help for %q", instruction)
		}
	}
}

func TestNativeInstructionFamiliesHaveConcreteExplanations(t *testing.T) {
	instructions := []string{
		"movq %rax, %rbx", "movzbl (%rax), %ebx", "leaq 8(%rax), %rbx",
		"addq $1, %rax", "sub x0, x1, x2", "madd x0, x1, x2, x3",
		"msub x0, x1, x2, x3", "udiv x0, x1, x2", "and x0, x1, x2",
		"orr x0, x1, x2", "eor x0, x1, x2", "lsl x0, x1, #3",
		"cmp x0, x1", "tst x0, x1", "fadd d0, d1, d2",
		"fsub d0, d1, d2", "fmul d0, d1, d2", "fmadd d0, d1, d2, d3",
		"ldrb w0, [x1, #4]", "strh w0, [sp, #8]", "ldp x0, x1, [sp]",
		"stp x0, x1, [sp, #-16]!", "adrp x0, 0x1000", "csel x0, x1, x2, eq",
		"cset x0, ne", "rev x0, x1", "clz x0, x1", "popcnt %rax, %rbx",
		"pushq %rax", "popq %rax", "callq 0x1000", "ret", "jmp 0x1000",
		"b.eq 0x1000", "cbz x0, 0x1000", "tbnz x0, #2, 0x1000",
		"nop", "syscall", "dmb ish", "yield", "xchgq %rax, %rbx",
	}
	for _, instruction := range instructions {
		help, ok := ForNative(instruction)
		if !ok || help.Explanation == "" {
			t.Errorf("native %q help has no concrete explanation: %#v", instruction, help)
		}
		if strings.HasPrefix(help.Explanation, "execute ") {
			t.Errorf("native %q uses generic explanation %q", instruction, help.Explanation)
		}
	}
}
