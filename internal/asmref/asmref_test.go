package asmref

import "testing"

func TestLookup(t *testing.T) {
	// The embedded table is generated from gen/testdata; ADD is always present.
	e, ok := Lookup("add")
	if !ok {
		t.Fatal("ADD not found (is table.json generated?)")
	}
	if e.Brief == "" || len(e.Syntax) == 0 {
		t.Fatalf("ADD entry looks empty: %#v", e)
	}
	if _, ok := Lookup("NOTAREALINSTRUCTION"); ok {
		t.Fatal("unexpected hit for bogus mnemonic")
	}
}
