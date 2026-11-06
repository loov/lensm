// Package asmref holds a generated, flattened instruction reference used for
// hover tooltips. The table is produced by ./gen from CPU ISA XML (ARM's
// official AArch64 release and the uops.info XED-derived dump) and embedded as
// gzip-compressed JSON — see README.md for how to regenerate.
//
// asmref is deliberately just reference text (brief, description, syntax,
// operand meanings) plus x86 performance data. The bespoke Go-pseudocode
// effects live in internal/asmhelp; nothing here overwrites them.
package asmref

import (
	"bytes"
	"compress/gzip"
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
	// Variants holds x86 per-operand-form performance data across all measured
	// microarchitectures. Empty for ARM.
	Variants []Variant `json:"variants,omitempty"`
}

// Variant is one x86 operand form (e.g. "ADD (R32, R32)") and its measured
// performance on each microarchitecture.
type Variant struct {
	Form string     `json:"form"`
	Perf []ArchPerf `json:"perf,omitempty"`
}

// ArchPerf is the uops.info measurement for one microarchitecture.
type ArchPerf struct {
	Arch    string  `json:"arch"`
	Uops    int     `json:"uops,omitempty"`
	Ports   string  `json:"ports,omitempty"` // uops.info notation, e.g. "1*p0156"
	Latency int     `json:"lat,omitempty"`   // worst-case cycles across operand pairs
	TP      float64 `json:"tp,omitempty"`    // throughput (cycles per instruction)
}

// PerfFor returns the per-variant measurements for one microarchitecture.
func (e Entry) PerfFor(arch string) []ArchPerf {
	var out []ArchPerf
	for _, v := range e.Variants {
		for _, p := range v.Perf {
			if p.Arch == arch {
				out = append(out, p)
			}
		}
	}
	return out
}

// Regenerating needs the real ISA dumps under data/ (run data/download.sh
// first). The ARM directory name tracks the pinned release; bump both here and
// in data/download.sh when updating.
//go:generate go run ./gen -arm ../../data/arm64/ISA_A64_xml_A_profile-2025-12 -x86 ../../data/x86/instructions.xml -out table.json.gz

//go:embed table.json.gz
var tableGz []byte

// table decodes the embed on first use. A malformed embed reads as "no
// data" rather than a panic in the UI hover path; the generator is what
// guarantees validity.
var table = sync.OnceValue(func() map[string]Entry {
	m := map[string]Entry{}
	if r, err := gzip.NewReader(bytes.NewReader(tableGz)); err == nil {
		_ = json.NewDecoder(r).Decode(&m)
	}
	return m
})

// Lookup returns the reference entry for a mnemonic (case-insensitive).
func Lookup(mnemonic string) (Entry, bool) {
	e, ok := table()[strings.ToUpper(strings.TrimSpace(mnemonic))]
	return e, ok
}
