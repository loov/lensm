package main

import (
	"strings"
	"testing"

	"loov.dev/lensm/internal/asmref"
)

func TestParseX86File(t *testing.T) {
	b := NewBuilder()
	if err := ParseX86File(b, "testdata/x86/instructions.xml"); err != nil {
		t.Fatal(err)
	}
	table := b.Table()

	add, ok := table["ADD"]
	if !ok {
		t.Fatal("ADD missing")
	}
	if add.Brief != "Add" {
		t.Errorf("ADD brief = %q", add.Brief)
	}
	// Both operand forms merge as syntax and as perf variants.
	wantSyntax := []string{"ADD (R32, R32)", "ADD (M32, R32)"}
	if strings.Join(add.Syntax, "|") != strings.Join(wantSyntax, "|") {
		t.Errorf("ADD syntax = %#v", add.Syntax)
	}
	if len(add.Variants) != 2 {
		t.Fatalf("ADD should have 2 variants, got %d", len(add.Variants))
	}

	// First form carries all measured microarchitectures (IACA nodes ignored).
	regReg := findVariant(add, "ADD (R32, R32)")
	if regReg == nil {
		t.Fatal("ADD (R32, R32) variant missing")
	}
	if got := archNames(regReg.Perf); strings.Join(got, ",") != "ADL-P,ZEN5" {
		t.Errorf("ADD (R32, R32) arches = %v", got)
	}
	adl := findPerf(regReg.Perf, "ADL-P")
	if adl == nil || adl.Uops != 1 || adl.Ports != "1*p0156" || adl.Latency != 1 || adl.TP != 0.25 {
		t.Errorf("ADD (R32, R32) ADL-P perf = %#v", adl)
	}

	// The memory form has higher latency and load ports.
	memReg := findVariant(add, "ADD (M32, R32)")
	adlMem := findPerf(memReg.Perf, "ADL-P")
	if adlMem == nil || adlMem.Latency != 6 || adlMem.Uops != 2 {
		t.Errorf("ADD (M32, R32) ADL-P perf = %#v", adlMem)
	}

	crc, ok := table["CRC32"]
	if !ok {
		t.Fatal("CRC32 missing")
	}
	if crc.Brief != "Accumulate CRC32 Value" {
		t.Errorf("CRC32 brief = %q", crc.Brief)
	}
	if p := crc.PerfFor("ADL-P"); len(p) != 1 || p[0].Latency != 3 {
		t.Errorf("CRC32 ADL-P perf = %#v", p)
	}
}

func findVariant(e asmref.Entry, form string) *asmref.Variant {
	for i := range e.Variants {
		if e.Variants[i].Form == form {
			return &e.Variants[i]
		}
	}
	return nil
}

func findPerf(perf []asmref.ArchPerf, arch string) *asmref.ArchPerf {
	for i := range perf {
		if perf[i].Arch == arch {
			return &perf[i]
		}
	}
	return nil
}

func archNames(perf []asmref.ArchPerf) []string {
	var out []string
	for _, p := range perf {
		out = append(out, p.Arch)
	}
	return out
}
