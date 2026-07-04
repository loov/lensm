// Package asmref holds a generated, flattened instruction reference used for
// hover tooltips. The table is produced by ./gen from CPU ISA XML (ARM's
// official AArch64 release and the uops.info XED-derived dump) and embedded as
// JSON — see README.md for how to regenerate.
//
// asmref is deliberately just reference text (brief, description, syntax,
// operand meanings). The bespoke Go-pseudocode effects live in
// internal/asmhelp; nothing here overwrites them.
package asmref

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

// Entry is the flattened reference for a single mnemonic. It matches the JSON
// schema written by the generator; both gen and runtime share this type.
type Entry struct {
	Brief       string            `json:"brief,omitempty"`
	Description string            `json:"description,omitempty"`
	Syntax      []string          `json:"syntax,omitempty"`
	Operands    map[string]string `json:"operands,omitempty"`
}

//go:generate go run ./gen

//go:embed table.json
var tableJSON []byte

var (
	tableOnce sync.Once
	table     map[string]Entry
)

func load() {
	// A malformed embed should surface as "no data" rather than panic in the
	// UI hover path; the generator is what guarantees validity.
	_ = json.Unmarshal(tableJSON, &table)
	if table == nil {
		table = map[string]Entry{}
	}
}

// Lookup returns the reference entry for a mnemonic (case-insensitive). The
// table is loaded once on first use.
func Lookup(mnemonic string) (Entry, bool) {
	tableOnce.Do(load)
	e, ok := table[strings.ToUpper(strings.TrimSpace(mnemonic))]
	return e, ok
}
