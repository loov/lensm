package thumbasm

import "strings"

// Regenerating needs ARM's AArch32 ISA XML under data/arm32 (run
// data/download.sh first); the directory name tracks the pinned release.
//go:generate go run ./gen -xml ../../data/arm32/ISA_AArch32_xml_A_profile-2025-12 -classes general,fpsimd -out tables_gen.go

// encoding is one T32 instruction encoding from ARM's ISA XML, as emitted
// by ./gen. The table is data; decode.go interprets it.
type encoding struct {
	name     string // e.g. "ADD_i_T3"
	mnemonic string // base mnemonic, e.g. "ADD"
	width    int    // 16 or 32
	// mask/value are the fixed bits; 16-bit encodings use the low half.
	mask, value uint32
	// sbMask/sbValue are the "should be" bits, (0)/(1) in the manual's
	// diagrams. Other values are UNPREDICTABLE; the decoder ignores them
	// but the fuzz test sets them.
	sbMask, sbValue uint32
	fields          []field
	// cond selects this encoding among those sharing a bit pattern: the
	// XML bitdiffs plus any field constraints (e.g. "Rn != 1101").
	cond string
	// tmpl are the assembler templates; when is the XML comment, either
	// "" or one of "InITBlock()" / "Outside IT block".
	tmpl []tmpl
	// decode is the ASL decode pseudocode, evaluated by eval.go.
	decode string
	// enc maps a template symbol ("<Rd>") to the ASL expression that
	// encodes it ("Rd", "(D :: Rd)").
	enc map[string]string
	// tables are the explanations' value tables: how a symbol such as
	// <dt> or <spec_reg> is spelled for each value of its fields.
	tables map[string]valueTable
	// alias is the alias condition for alias encodings, evaluated after
	// decode; "" for base encodings, "Unconditionally" for plain aliases.
	alias   string
	isAlias bool
	// simd marks Advanced SIMD (NEON) encodings, absent from T32-only
	// cores; decoded on a best-effort basis.
	simd bool

	// parsed forms, filled by compileTables
	prog      *program
	condExpr  *node
	aliasExpr *node
	encExpr   map[string]*node
	tableExpr map[string]*node
}

type field struct {
	name  string
	lo, n int
}

type tmpl struct {
	when, text string
}

type valueTable struct {
	encodedin string
	rows      []valueRow
}

type valueRow struct {
	bits, text string // bits may contain x wildcards
}

// hasWide reports whether any template of the encoding carries a .W
// qualifier, i.e. a narrower encoding of the instruction exists.
func (e *encoding) hasWide() bool {
	// llvm-objdump also qualifies the flag-setting comparisons, whose
	// 16-bit forms take only registers.
	switch e.mnemonic {
	case "TST", "TEQ", "CMN":
		return true
	case "MUL":
		return false
	}
	for _, t := range e.tmpl {
		if strings.Contains(t.text, ".W") {
			return true
		}
	}
	return false
}
