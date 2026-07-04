package main

import (
	"strings"
	"testing"
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
	// summary attribute becomes the brief; uops.info has no description prose.
	if add.Brief != "Add" {
		t.Errorf("ADD brief = %q", add.Brief)
	}
	// Both variants merge; the string attribute is the syntax form.
	wantSyntax := []string{"ADD (R32, R32)", "ADD (R8, I8)"}
	if strings.Join(add.Syntax, "|") != strings.Join(wantSyntax, "|") {
		t.Errorf("ADD syntax = %#v", add.Syntax)
	}
	// Operands are intentionally not emitted for x86 (derivable, unused, and
	// duplicated); the perf subtrees must not leak in either.
	if len(add.Operands) != 0 {
		t.Errorf("ADD should have no operands, got %#v", add.Operands)
	}

	crc, ok := table["CRC32"]
	if !ok {
		t.Fatal("CRC32 missing")
	}
	if crc.Brief != "Accumulate CRC32 Value" {
		t.Errorf("CRC32 brief = %q", crc.Brief)
	}
}
