package asmhelp

import "testing"

func TestPerfForNative(t *testing.T) {
	// ADD reg,reg and ADD reg,mem differ several-fold; the operand matcher
	// must pick the right form, not an aggregate.
	regReg, ok := PerfForNative("amd64", "ADD", "add %rax,%rbx")
	if !ok || regReg.Uops != 1 {
		t.Fatalf("add reg,reg = %+v ok=%v, want 1 uop", regReg, ok)
	}
	memDst, ok := PerfForNative("amd64", "ADD", "addq %rax,(%rbx)")
	if !ok || memDst.Uops <= regReg.Uops {
		t.Fatalf("add reg,mem = %+v ok=%v, want more uops than reg,reg", memDst, ok)
	}
	if memDst.Form != "ADD (M64, R64)" {
		t.Errorf("add reg,mem matched form %q, want ADD (M64, R64)", memDst.Form)
	}

	imm, ok := PerfForNative("amd64", "ADD", "addq $0x8,%rsp")
	if !ok || imm.Uops != 1 {
		t.Fatalf("add imm,reg = %+v ok=%v, want 1 uop", imm, ok)
	}

	// LEA forms encode the address mode in the variant name, not the operand
	// list; the count-mismatch penalty must still let them match.
	if _, ok := PerfForNative("amd64", "LEA", "leaq 0x8(%rsp),%rdi"); !ok {
		t.Error("lea did not match any form")
	}

	// Shift-by-CL is much slower than shift-by-immediate; the CL register
	// must be matched by name.
	byCL, ok1 := PerfForNative("amd64", "SHL", "shlq %cl,%rax")
	byImm, ok2 := PerfForNative("amd64", "SHL", "shlq $0x3,%rax")
	if !ok1 || !ok2 || byCL.TP <= byImm.TP {
		t.Errorf("shl cl = %+v (%v), shl imm = %+v (%v); want cl slower", byCL, ok1, byImm, ok2)
	}

	if _, ok := PerfForNative("arm64", "ADD", "add x0, x1, x2"); ok {
		t.Error("arm64 must report no perf data")
	}

	if p, ok := PerfForNative("amd64", "DIVSD", "divsd %xmm1,%xmm0"); !ok || p.SerialCycles() < 4 {
		t.Errorf("divsd = %+v ok=%v, want serial cycles >= throughput 4", p, ok)
	}
}
