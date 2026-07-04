package main

import (
	"strings"
	"testing"
)

func TestParseARMDir(t *testing.T) {
	b := NewBuilder()
	if err := ParseARMDir(b, "testdata/arm"); err != nil {
		t.Fatal(err)
	}
	table := b.Table()

	// sysreg_ and AArch64- files must be skipped entirely.
	if _, ok := table["SHOULDNOTAPPEAR"]; ok {
		t.Fatal("skipped file leaked into the table")
	}

	add, ok := table["ADD"]
	if !ok {
		t.Fatal("ADD missing")
	}
	if add.Brief != "ADD (immediate)" {
		t.Errorf("ADD brief = %q", add.Brief)
	}
	// Description is the first <authored> paragraph only; the second must be dropped.
	if !strings.HasPrefix(add.Description, "Add (immediate) adds a register value") {
		t.Errorf("ADD description = %q", add.Description)
	}
	if strings.Contains(add.Description, "second paragraph") {
		t.Errorf("ADD description leaked a later paragraph: %q", add.Description)
	}
	if len(add.Syntax) != 1 || add.Syntax[0] != "ADD <Wd>, <Wn>, #<imm>" {
		t.Errorf("ADD syntax = %#v", add.Syntax)
	}
	if got := add.Operands["<Wd>"]; !strings.Contains(got, "destination register") {
		t.Errorf("ADD operand <Wd> = %q", got)
	}
	if _, ok := add.Operands["<imm>"]; !ok {
		t.Errorf("ADD operand <imm> missing: %#v", add.Operands)
	}

	// Two LDR encodings in one file must merge into distinct syntaxes.
	ldr := table["LDR"]
	if len(ldr.Syntax) != 2 {
		t.Errorf("LDR should have 2 syntaxes, got %#v", ldr.Syntax)
	}
}
