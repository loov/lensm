package thumbasm

import (
	"fmt"
	"strconv"
	"strings"
)

// formatter expands an encoding's assembler template with the values the
// decode pseudocode produced.
type formatter struct {
	enc   *encoding
	env   *env
	raw   uint32
	pc    uint64
	width int
	cond  uint8 // current condition (from IT), 14 = always

	target    uint64
	hasTarget bool
}

var condNames = [...]string{"eq", "ne", "hs", "lo", "mi", "pl", "vs", "vc", "hi", "ls", "ge", "lt", "gt", "le", "", "nv"}

var regNames = [...]string{"r0", "r1", "r2", "r3", "r4", "r5", "r6", "r7", "r8", "r9", "r10", "r11", "r12", "sp", "lr", "pc"}

func (f *formatter) format() (string, error) {
	t := f.pickTemplate()
	out, err := f.expand(t)
	if err != nil {
		return "", err
	}
	// A 32-bit encoding whose manual page also lists a ".W" spelling has
	// a 16-bit sibling; write the width qualifier the way assemblers and
	// llvm-objdump do, so it is visible which one was chosen.
	if f.width == 32 && !strings.Contains(t, ".W") && f.enc.hasWide() {
		head, rest, _ := strings.Cut(out, " ")
		out = head + ".w " + rest
	}
	out = strings.Join(strings.Fields(strings.ToLower(out)), " ")
	out = strings.ReplaceAll(out, " ,", ",")
	out = strings.ReplaceAll(out, " ]", "]")
	// A mandatory shift operand of LSL #0 is no shift (CMP Rn, Rm, LSL #0).
	out = strings.TrimSuffix(out, ", lsl #0")
	return out, nil
}

// pickTemplate chooses among the encoding's templates by IT state; the
// XML annotates 16-bit flag-setting forms with "InITBlock()" and
// "Outside IT block".
func (f *formatter) pickTemplate() string {
	inside := func(when string) bool {
		return strings.HasPrefix(when, "InITBlock()") || strings.HasPrefix(when, "Inside IT block")
	}
	outside := func(when string) bool {
		return strings.HasPrefix(when, "Outside IT block")
	}
	state := func(when string) bool {
		return f.env.inIT && inside(when) || !f.env.inIT && outside(when)
	}
	// The XML marks the canonical spelling directly where it matters;
	// an unannotated template is the general form.
	for _, t := range f.enc.tmpl {
		// VLDM/VSTM: the manual prefers the bare mnemonic, the
		// disassemblers VLDMIA/VSTMIA.
		if t.when == "Preferred syntax" && f.enc.mnemonic != "VLDM" && f.enc.mnemonic != "VSTM" {
			return t.text
		}
	}
	for _, t := range f.enc.tmpl {
		if t.when == "" {
			return t.text
		}
	}
	// The 16-bit flag-setting forms have only "InITBlock()" and
	// "Outside IT block" variants (ADD vs ADDS), sometimes with further
	// qualifications about which operands the form can represent.
	for _, t := range f.enc.tmpl {
		if t.when == "InITBlock()" && f.env.inIT || t.when == "Outside IT block" && !f.env.inIT {
			return t.text
		}
	}
	for _, t := range f.enc.tmpl {
		if state(t.when) {
			return t.text
		}
	}
	// Otherwise the variants are assembler-selection hints ("<imm16> can
	// be represented in T1"); prefer the spelling that names this
	// encoding (MOVW over MOV for MOV_i_T3).
	for _, t := range f.enc.tmpl {
		if mnemonicOf(t.text) == f.enc.mnemonic {
			return t.text
		}
	}
	return f.enc.tmpl[0].text
}

func mnemonicOf(t string) string {
	for i := 0; i < len(t); i++ {
		if c := t[i]; !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return t[:i]
		}
	}
	return t
}

// expand walks template text: <sym> markers become operand values and
// {...} groups are optional parts decided by the operand they contain.
func (f *formatter) expand(t string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(t); {
		switch t[i] {
		case '#':
			// "#<option>" (DBG) is a plain number, unlike a barrier's
			// named option.
			if strings.HasPrefix(t[i:], "#<option>") {
				v, _ := f.env.get("option")
				b.WriteString("#" + fmtImm(int64(v.uint())))
				i += len("#<option>")
				continue
			}
			b.WriteByte(t[i])
			i++
		case '<':
			j := strings.IndexByte(t[i:], '>')
			if j < 0 {
				return "", fmt.Errorf("unterminated symbol in %q", t)
			}
			s, err := f.symbol(t[i : i+j+1])
			if err != nil {
				return "", err
			}
			b.WriteString(s)
			i += j + 1
		case '{':
			j := matchBrace(t, i)
			if j < 0 {
				return "", fmt.Errorf("unbalanced brace in %q", t)
			}
			s, err := f.group(t[i+1:j], t[j+1:])
			if err != nil {
				return "", err
			}
			b.WriteString(s)
			i = j + 1
		default:
			b.WriteByte(t[i])
			i++
		}
	}
	return b.String(), nil
}

func matchBrace(t string, open int) int {
	depth := 0
	for i := open; i < len(t); i++ {
		switch t[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// group decides whether an optional {...} part is printed. rest is the
// template after the group, used to compare an elidable register with
// the operand that follows it.
func (f *formatter) group(inner, rest string) (string, error) {
	switch inner {
	case "<c>":
		return f.symbol("<c>")
	case "<q>", "+", "IA", "'0010', '0011', '0100', '0111'":
		return "", nil
	case "#":
		return "#", nil
	case "+/-":
		if v, ok := f.env.get("add"); ok && !v.truthy() {
			return "-", nil
		}
		return "", nil
	case "!":
		if v, ok := f.env.get("wback"); ok && v.truthy() {
			return "!", nil
		}
		return "", nil
	case "<option>":
		return f.symbol("<option>")
	}
	// Optional data-type suffixes ({.32}, {.<size>}, {.<dt>}) are
	// assembler sugar; print one only when the pseudocode defines the
	// element size it names, as for the VMOV element moves.
	if strings.HasPrefix(inner, ".") {
		if !strings.Contains(inner, "<") {
			return "", nil
		}
		out, err := f.expand(inner)
		if err != nil {
			return "", nil
		}
		return out, nil
	}
	if strings.HasPrefix(inner, "<x>") {
		firstcond, _ := f.env.get("firstcond")
		mask, _ := f.env.get("mask")
		return itSuffix(uint8(firstcond.uint()), uint8(mask.uint())), nil
	}
	// A destination that is necessarily the same register as the
	// following operand ({<Rdn>, }<Rdn>, {SP, }SP) is written once, as
	// the usual disassemblers do; {<Rd>, }<Rn> always prints both.
	if reg, trailing := strings.CutSuffix(inner, ", "); trailing && isRegister(reg) {
		if strings.HasPrefix(rest, reg) {
			return "", nil
		}
		return f.expand(inner)
	}
	// Optional immediates and shifts vanish when zero.
	if strings.Contains(inner, "<shift>") || strings.Contains(inner, "<amount>") {
		if f.shiftAmount() == 0 {
			return "", nil
		}
		return f.expand(inner)
	}
	if strings.Contains(inner, "<imm") {
		sym := inner[strings.Index(inner, "<imm"):]
		sym = sym[:strings.IndexByte(sym, '>')+1]
		v, err := f.immediate(sym)
		if err != nil {
			return "", err
		}
		if v == 0 {
			return "", nil
		}
		return f.expand(inner)
	}
	if strings.Contains(inner, "<opc2>") {
		if v, _ := f.env.get("opc2"); v.uint() == 0 {
			return "", nil
		}
	}
	return f.expand(inner)
}

func isRegister(sym string) bool {
	return sym == "SP" || strings.HasPrefix(sym, "<R") && strings.HasSuffix(sym, ">")
}

// firstRegister returns the first register symbol in template text.
func firstRegister(t string) string {
	for i := 0; i < len(t); i++ {
		if t[i] == '<' {
			j := strings.IndexByte(t[i:], '>')
			if j < 0 {
				return ""
			}
			if sym := t[i : i+j+1]; isRegister(sym) {
				return sym
			}
			i += j
		} else if strings.HasPrefix(t[i:], "SP") {
			return "SP"
		}
	}
	return ""
}

// aslRegister maps a register symbol to the integer the decode
// pseudocode binds for it. The pseudocode is authoritative where the
// explanation's encodedin is incomplete (ADD_SP_r_T1 says "Rdm" but
// decodes DM:Rdm).
var aslRegister = map[string]string{
	"<Rd>": "d", "<Rdn>": "d", "<Rdm>": "d", "<Rn>": "n", "<Rm>": "m", "<Rt>": "t", "<Rt2>": "t2",
	"<Ra>": "a", "<Rs>": "s", "<RdLo>": "dLo", "<RdHi>": "dHi",
}

func (f *formatter) regValue(sym string) (uint64, error) {
	if sym == "SP" {
		return 13, nil
	}
	if name := aslRegister[sym]; name != "" {
		if v, ok := f.env.get(name); ok && v.k == kInt {
			return uint64(v.i), nil
		}
	}
	n := f.enc.encExpr[sym]
	if n == nil {
		return 0, fmt.Errorf("%s: no encoding for %s", f.enc.name, sym)
	}
	v, err := f.env.eval(n)
	if err != nil {
		return 0, err
	}
	return v.uint(), nil
}

func (f *formatter) shiftAmount() int64 {
	if v, ok := f.env.get("shift_n"); ok {
		return v.i
	}
	if v, ok := f.env.get("rotation"); ok {
		return v.i
	}
	if v, ok := f.env.get("imm2"); ok {
		return int64(v.uint())
	}
	return 0
}

// immediate resolves an <imm*>/<const> symbol to its value, preferring
// the pseudocode's decoded imm32 over the raw field.
func (f *formatter) immediate(sym string) (int64, error) {
	for _, name := range []string{"imm32", "imm16", "saturate_to", "shift_n"} {
		if v, ok := f.env.get(name); ok {
			if name == "shift_n" && sym != "<imm>" {
				continue
			}
			return int64(v.uint()), nil
		}
	}
	if v, ok := f.env.get("imm"); ok && v.k == kBits && v.n == 12 && sym == "<const>" {
		r, _ := t32ExpandImmC(uint32(v.uint()), false)
		return int64(r), nil
	}
	if n := f.enc.encExpr[sym]; n != nil {
		v, err := f.env.eval(n)
		if err != nil {
			return 0, err
		}
		return int64(v.uint()), nil
	}
	return 0, fmt.Errorf("%s: cannot resolve %s", f.enc.name, sym)
}

func fmtImm(v int64) string {
	if v < 0 {
		return "-" + fmtImm(-v)
	}
	if v < 10 {
		return strconv.FormatInt(v, 10)
	}
	return "0x" + strconv.FormatInt(v, 16)
}

var barrierOptions = map[uint64]string{15: "sy", 14: "st", 13: "ld", 11: "ish", 10: "ishst", 9: "ishld", 7: "nsh", 6: "nshst", 5: "nshld", 3: "osh", 2: "oshst", 1: "oshld"}

func (f *formatter) symbol(sym string) (string, error) {
	switch sym {
	case "<c>":
		// Conditional branches carry their own cond field; everything
		// else is conditional only inside an IT block.
		if v, ok := f.env.get("cond"); ok {
			return condNames[v.uint()&15], nil
		}
		if f.env.inIT {
			return condNames[f.cond&15], nil
		}
		return "", nil
	case "<q>":
		return "", nil
	case "<cond>", "<firstcond>":
		if v, ok := f.env.get("firstcond"); ok {
			return condNames[v.uint()&15], nil
		}
		v, _ := f.env.get("cond")
		return condNames[v.uint()&15], nil
	case "<label>":
		return f.label()
	case "<shift>":
		v, _ := f.env.get("shift_t")
		return strings.ToLower(strings.TrimPrefix(v.s, "SRType_")), nil
	case "<amount>":
		return fmtImm(f.shiftAmount()), nil
	case "<lsb>":
		v, _ := f.env.get("lsbit")
		return fmtImm(v.i), nil
	case "<width>":
		if v, ok := f.env.get("widthminus1"); ok {
			return fmtImm(v.i + 1), nil
		}
		msb, _ := f.env.get("msbit")
		lsb, _ := f.env.get("lsbit")
		return fmtImm(msb.i - lsb.i + 1), nil
	case "<registers>":
		v, ok := f.env.get("registers")
		if !ok {
			// Alias encodings (PUSH/POP) carry no decode pseudocode;
			// the list is the register_list field plus the P/M bits.
			v, _ = f.env.get("register_list")
			bits := v.uint()
			if m, ok := f.env.get("M"); ok && m.uint() != 0 {
				bits |= 1 << 14
			}
			if p, ok := f.env.get("P"); ok && p.uint() != 0 {
				bits |= 1 << 15
			}
			return regList(bits), nil
		}
		return regList(v.uint()), nil
	case "<single_register_list>":
		r, err := f.regValue("<Rt>")
		if err != nil {
			r, err = f.regValue(sym)
		}
		return "{" + regNames[r&15] + "}", err
	case "<option>":
		v, _ := f.env.get("option")
		if name, ok := barrierOptions[v.uint()]; ok {
			return name, nil
		}
		return "#" + fmtImm(int64(v.uint())), nil
	case "<spec_reg>":
		// VMRS/VMSR name FPSCR and friends in their value table; MRS/MSR
		// are decoded the M-profile way below.
		if t, ok := f.enc.tables[sym]; ok && strings.HasPrefix(f.enc.mnemonic, "V") {
			return f.fromTable(sym, t)
		}
		return f.specReg(), nil
	case "<coproc>":
		v, _ := f.env.get("cp")
		return "p" + strconv.FormatInt(v.i, 10), nil
	case "<CRn>", "<CRm>":
		v, _ := f.env.get(sym[1 : len(sym)-1])
		return "c" + strconv.FormatUint(v.uint(), 10), nil
	case "<iflags>":
		var b strings.Builder
		for _, flag := range "AIF" {
			if v, _ := f.env.get(string(flag)); v.uint() != 0 {
				b.WriteRune(flag)
			}
		}
		return b.String(), nil
	case "<endian_specifier>":
		if v, _ := f.env.get("E"); v.uint() != 0 {
			return "be", nil
		}
		return "le", nil
	}
	if isRegister(sym) {
		r, err := f.regValue(sym)
		if err != nil {
			return "", err
		}
		if r == 15 && f.enc.mnemonic == "VMRS" {
			return "apsr_nzcv", nil // VMRS APSR_nzcv, FPSCR
		}
		return regNames[r&15], nil
	}
	if out, ok, err := f.fpSymbol(sym); ok || err != nil {
		return out, err
	}
	if t, ok := f.enc.tables[sym]; ok {
		return f.fromTable(sym, t)
	}
	switch sym {
	case "<size>", "<dt>":
		// Element size of the VMOV element moves, from the pseudocode.
		esize, ok := f.env.get("esize")
		if !ok {
			return "", fmt.Errorf("%s: no esize for %s", f.enc.name, sym)
		}
		if sym == "<dt>" && esize.i < 32 {
			if u, _ := f.env.get("U"); u.uint() != 0 {
				return fmt.Sprintf("u%d", esize.i), nil
			}
			return fmt.Sprintf("s%d", esize.i), nil
		}
		return strconv.FormatInt(esize.i, 10), nil
	}
	if strings.HasPrefix(sym, "<imm") || sym == "<const>" {
		v, err := f.immediate(sym)
		if err != nil {
			return "", err
		}
		return fmtImm(v), nil
	}
	// Anything else (opc1, opc2, mode, banked_reg, ...) prints as its
	// encoded number.
	if n := f.enc.encExpr[sym]; n != nil {
		v, err := f.env.eval(n)
		if err != nil {
			return "", err
		}
		return strconv.FormatUint(v.uint(), 10), nil
	}
	if v, ok := f.env.get(sym[1 : len(sym)-1]); ok {
		return strconv.FormatUint(v.uint(), 10), nil
	}
	return "", fmt.Errorf("%s: unknown symbol %s", f.enc.name, sym)
}

// label resolves a PC-relative target. The PC of a T32 instruction is
// its address plus 4; ADR, BLX and literal loads align it down to 4.
func (f *formatter) label() (string, error) {
	imm, ok := f.env.get("imm32")
	if !ok {
		return "", fmt.Errorf("%s: label without imm32", f.enc.name)
	}
	base := f.pc + 4
	switch f.enc.mnemonic {
	case "ADR", "BLX", "LDR", "LDRB", "LDRH", "LDRSB", "LDRSH", "LDRD", "PLD", "PLI", "VLDR", "SUB", "ADD":
		base &^= 3
	}
	off := imm.sint()
	if add, ok := f.env.get("add"); ok && !add.truthy() {
		off = -off
	}
	f.target = uint64(uint32(int64(base) + off))
	f.hasTarget = true
	return "0x" + strconv.FormatUint(f.target, 16), nil
}

func regList(bits uint64) string {
	var names []string
	for i := 0; i < 16; i++ {
		if bits>>uint(i)&1 != 0 {
			names = append(names, regNames[i])
		}
	}
	return "{" + strings.Join(names, ", ") + "}"
}

// specReg names the system register of MRS/MSR. A-profile encodes only
// APSR/CPSR/SPSR; M-profile (the common T32-only target) uses the SYSm
// byte in the same bit positions, so that is decoded from the raw word.
func (f *formatter) specReg() string {
	sysm := f.raw & 0xff
	names := map[uint32]string{0: "apsr", 1: "iapsr", 2: "eapsr", 3: "xpsr", 5: "ipsr", 6: "epsr", 7: "iepsr",
		8: "msp", 9: "psp", 10: "msplim", 11: "psplim", 16: "primask", 17: "basepri", 18: "basepri_max", 19: "faultmask", 20: "control",
		0x88: "msp_ns", 0x89: "psp_ns", 0x8a: "msplim_ns", 0x8b: "psplim_ns", 0x90: "primask_ns", 0x91: "basepri_ns", 0x93: "faultmask_ns", 0x94: "control_ns", 0x98: "sp_ns"}
	name, ok := names[sysm]
	if !ok {
		if v, _ := f.env.get("R"); v.uint() != 0 {
			return "spsr"
		}
		return "cpsr"
	}
	if f.enc.mnemonic == "MSR" && sysm>>3 == 0 {
		switch f.raw >> 10 & 3 {
		case 1:
			name += "_g"
		case 2:
			name += "_nzcvq"
		case 3:
			name += "_nzcvqg"
		}
	}
	return name
}

// fromTable spells a symbol from its explanation's value table.
func (f *formatter) fromTable(sym string, t valueTable) (string, error) {
	n := f.enc.tableExpr[sym]
	if n == nil {
		return "", fmt.Errorf("%s: table for %s has no fields", f.enc.name, sym)
	}
	v, err := f.env.eval(n)
	if err != nil {
		return "", err
	}
	for _, row := range t.rows {
		if len(row.bits) != v.n {
			continue
		}
		if equal(value{k: kBits, n: len(row.bits), s: row.bits, i: int64(bitsValue(row.bits))}, v) {
			if strings.Contains(row.text, "RESERVED") || strings.Contains(row.text, "UNPREDICTABLE") {
				return "", errReject
			}
			return row.text, nil
		}
	}
	return "", fmt.Errorf("%s: no table row for %s = %0*b", f.enc.name, sym, v.n, v.uint())
}

// fpSymbol formats the floating-point operand symbols: S/D/Q registers,
// register ranges, the VFP immediate and fixed-point fraction bits.
func (f *formatter) fpSymbol(sym string) (string, bool, error) {
	switch sym {
	case "<sreglist>", "<dreglist>", "<list>":
		d, _ := f.env.get("d")
		regs, _ := f.env.get("regs")
		prefix := "s"
		if sym == "<dreglist>" {
			prefix = "d"
		}
		if sym == "<list>" {
			// VLDM/VSTM: the pseudocode says which register file; the
			// Advanced SIMD structure loads' lists are not handled.
			single, ok := f.env.get("single_regs")
			if !ok {
				return "", true, fmt.Errorf("%s: unsupported <list>", f.enc.name)
			}
			if !single.truthy() {
				prefix = "d"
			}
		}
		// More registers than exist is UNPREDICTABLE; show what fits.
		regs.i = min(regs.i, 32-d.i)
		if regs.i <= 1 {
			return fmt.Sprintf("{%s%d}", prefix, d.i), true, nil
		}
		return fmt.Sprintf("{%s%d-%s%d}", prefix, d.i, prefix, d.i+regs.i-1), true, nil
	case "<fbits>":
		v, ok := f.env.get("frac_bits")
		if !ok {
			return "", true, fmt.Errorf("%s: no frac_bits", f.enc.name)
		}
		return fmtImm(v.i), true, nil
	case "<imm>":
		if v, ok := f.env.get("imm"); ok && v.k == kFloat {
			return fmtFloat(v.f), true, nil
		}
		return "", false, nil
	case "<Sm1>":
		r, err := f.regValue("<Sm>")
		return fmt.Sprintf("s%d", r+1), true, err
	}
	if len(sym) < 4 || (sym[1] != 'S' && sym[1] != 'D' && sym[1] != 'Q') || sym[2] < 'a' || sym[2] > 'z' {
		return "", false, nil
	}
	// <Sd>, <Dm>, <Qn>, <Dd[x]>, ...: the pseudocode binds d/n/m to the
	// register number; the encodedin expression is the fallback.
	base := strings.TrimSuffix(sym, "[x]>")
	if base == sym {
		base = strings.TrimSuffix(sym, ">")
	}
	var r uint64
	if v, ok := f.env.get(strings.ToLower(base[2:3])); ok && v.k == kInt && len(base) == 3 {
		r = uint64(v.i)
	} else if n := f.enc.encExpr[sym]; n != nil {
		v, err := f.env.eval(n)
		if err != nil {
			return "", true, err
		}
		r = v.uint()
	} else {
		return "", true, fmt.Errorf("%s: no encoding for %s", f.enc.name, sym)
	}
	var name string
	switch sym[1] {
	case 'S':
		name = fmt.Sprintf("s%d", r)
	case 'D':
		name = fmt.Sprintf("d%d", r)
	case 'Q':
		name = fmt.Sprintf("q%d", r>>1)
	}
	if strings.HasSuffix(sym, "[x]>") {
		idx, ok := f.env.get("index")
		if !ok {
			return "", true, fmt.Errorf("%s: no index for %s", f.enc.name, sym)
		}
		name += fmt.Sprintf("[%d]", idx.i)
	}
	return name, true, nil
}

func fmtFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}
