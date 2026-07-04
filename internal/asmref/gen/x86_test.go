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
	if !strings.HasPrefix(add.Description, "Adds the destination operand") {
		t.Errorf("ADD description = %q", add.Description)
	}
	// Both width variants merge; within each instruction the Intel form
	// precedes its AT&T form.
	wantSyntax := []string{"ADD r/m32, r32", "add %r32, %r/m32", "ADD r/m8, imm8"}
	if strings.Join(add.Syntax, "|") != strings.Join(wantSyntax, "|") {
		t.Errorf("ADD syntax = %#v", add.Syntax)
	}
	if got := add.Operands["r/m32"]; got != "32-bit register, read+written" {
		t.Errorf("ADD operand r/m32 = %q", got)
	}
	if got := add.Operands["imm8"]; got != "8-bit immediate" {
		t.Errorf("ADD operand imm8 = %q", got)
	}

	crc, ok := table["CRC32"]
	if !ok {
		t.Fatal("CRC32 missing")
	}
	if !strings.Contains(crc.Description, "CRC32 value") {
		t.Errorf("CRC32 description = %q", crc.Description)
	}
}
