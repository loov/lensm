package asmhelp

import (
	"slices"
	"strconv"
	"strings"

	"loov.dev/lensm/internal/asmref"
)

// Perf is the uops.info measurement for one instruction on the reference
// microarchitecture, matched to the instruction's actual operand form
// (register vs memory forms of the same mnemonic differ several-fold).
type Perf struct {
	Arch    string  // microarchitecture the numbers were measured on
	Form    string  // matched operand form, e.g. "ADD (R64, I8)"
	Uops    int     // fused-domain micro-ops
	Latency int     // worst-case dependency-chain cycles; 0 = unmeasured
	TP      float64 // reciprocal throughput, cycles per instruction
	Ports   string  // execution-port usage in uops.info notation
}

// SerialCycles is the instruction's contribution to a serial dependency
// chain: the measured latency, or the reciprocal throughput as a floor when
// latency was not measured.
func (p Perf) SerialCycles() float64 {
	if float64(p.Latency) > p.TP {
		return float64(p.Latency)
	}
	return p.TP
}

// PerfForNative returns the measurement for an x86 instruction in native
// (GNU/AT&T) syntax, picking the operand form that best matches the actual
// operands. ok is false when there is no data or no form fits.
func PerfForNative(arch, canonical, nativeText string) (Perf, bool) {
	if !isX86(arch) {
		return Perf{}, false
	}
	mnemonic, rawOperands := splitAssemblyInstruction(nativeText)
	// AT&T orders operands source-first; the forms are Intel-ordered
	// (destination-first), so match positionally against the reversal.
	operands := make([]operandClass, 0, len(rawOperands))
	for i := len(rawOperands) - 1; i >= 0; i-- {
		operands = append(operands, classifyNativeOperand(rawOperands[i]))
	}
	widthHint := gnuSuffixWidth(mnemonic)

	best := Perf{}
	bestScore := 0
	// The table merges ARM and x86 under one key, and an ARM entry without
	// x86 variants can shadow the spelling we need (SETE), so every
	// candidate mnemonic is scanned rather than the first table hit.
	var seen []string
	for _, m := range []string{canonical, mnemonic} {
		if m == "" {
			continue
		}
		for _, candidate := range mnemonicCandidates(arch, m) {
			if slices.Contains(seen, candidate) {
				continue
			}
			seen = append(seen, candidate)
			ref, ok := asmref.Lookup(candidate)
			if !ok {
				continue
			}
			for _, v := range ref.Variants {
				perf, ok := variantPerf(v, referenceArch)
				if !ok {
					continue
				}
				score := scoreForm(v.Form, operands, widthHint)
				// Ties go to the slower variant, so ambiguity reads conservative.
				if score > bestScore || (score == bestScore && bestScore > 0 && perf.TP > best.TP) {
					bestScore = score
					best = Perf{
						Arch:    referenceArch,
						Form:    v.Form,
						Uops:    perf.Uops,
						Latency: perf.Latency,
						TP:      perf.TP,
						Ports:   perf.Ports,
					}
				}
			}
		}
	}
	return best, bestScore > 0
}

func variantPerf(v asmref.Variant, arch string) (asmref.ArchPerf, bool) {
	for _, p := range v.Perf {
		if p.Arch == arch {
			return p, true
		}
	}
	return asmref.ArchPerf{}, false
}

// operandClass is a coarse classification of one operand for form matching.
type operandClass struct {
	kind  byte   // 'I' imm, 'R' gp reg, 'V' vector reg, 'M' memory, 'K' mask, 'S' segment, 'L' label, 'O' other
	width int    // register width in bits; 0 = unknown/any
	name  string // canonical register name (lowercase), for AL/CL/RAX-style tokens
	value int64  // immediate value; -1 = unknown
}

// scoreForm scores how well a form like "ADD (M64, R32)" fits the classified
// operands. Positive means plausible; higher is a better fit. A count
// mismatch is a penalty rather than a rejection because some forms encode an
// operand in the name instead of the list (LEA_B (R64)).
func scoreForm(form string, operands []operandClass, widthHint int) int {
	var tokens []string
	if _, args, ok := strings.Cut(form, "("); ok {
		for t := range strings.SplitSeq(strings.TrimSuffix(strings.TrimSpace(args), ")"), ",") {
			tokens = append(tokens, strings.TrimSpace(t))
		}
	}
	score := 1 // base, so zero-operand forms can match
	for i := 0; i < max(len(operands), len(tokens)); i++ {
		if i >= len(operands) || i >= len(tokens) {
			score -= 2
			continue
		}
		score += scoreOperand(operands[i], tokens[i], widthHint)
	}
	return score
}

func scoreOperand(op operandClass, token string, widthHint int) int {
	const mismatch = -4
	switch {
	case token == "0", token == "1":
		if op.kind == 'I' && op.value == int64(token[0]-'0') {
			return 3
		}
		return mismatch
	case token == "I8", token == "I16", token == "I32", token == "I64":
		if op.kind == 'I' {
			return 2
		}
		return mismatch
	case token == "AL", token == "AX", token == "EAX", token == "RAX", token == "CL", token == "DX":
		if op.kind == 'R' && op.name == strings.ToLower(token) {
			return 4
		}
		return mismatch
	case token == "R", token == "R8h", token == "R8l", token == "R8",
		token == "R16", token == "R32", token == "R64":
		if op.kind != 'R' {
			return mismatch
		}
		width := 0
		switch token {
		case "R8h", "R8l", "R8":
			width = 8
		case "R16":
			width = 16
		case "R32":
			width = 32
		case "R64":
			width = 64
		}
		if width == 0 || width == op.width {
			return 3
		}
		return mismatch
	case token == "XMM", token == "YMM", token == "ZMM":
		if op.kind == 'V' && op.name == strings.ToLower(token) {
			return 3
		}
		return mismatch
	case token == "K":
		if op.kind == 'K' {
			return 3
		}
		return mismatch
	case token == "SEG", token == "FS", token == "GS":
		if op.kind == 'S' {
			return 2
		}
		return mismatch
	case token == "Rel8", token == "Rel32":
		if op.kind == 'L' {
			return 2
		}
		return mismatch
	case strings.HasPrefix(token, "M"), strings.HasPrefix(token, "VSIB"):
		if token == "MM" {
			if op.kind == 'O' && op.name == "mm" {
				return 3
			}
			return mismatch
		}
		// A bare absolute displacement (vmovdqu64 0xc2d38,%zmm1) has no
		// parens and classifies as a label; accept it weakly as memory so
		// branch targets still prefer their Rel forms.
		if op.kind == 'L' {
			return 1
		}
		if op.kind != 'M' {
			return mismatch
		}
		// The GNU mnemonic suffix (addq -> 64) disambiguates M8..M64 forms.
		if w, err := strconv.Atoi(strings.TrimPrefix(token, "M")); err == nil && w == widthHint {
			return 4
		}
		return 3
	default:
		// ST, BND, TMM, CR, DR, ...: match loosely by name.
		if op.kind == 'O' && strings.HasPrefix(op.name, strings.ToLower(token)) {
			return 2
		}
		return mismatch
	}
}

func classifyNativeOperand(op string) operandClass {
	op = strings.TrimSpace(op)
	op = strings.TrimPrefix(op, "*") // AT&T indirect call/jump marker
	switch {
	case strings.HasPrefix(op, "$"):
		value, err := strconv.ParseInt(strings.TrimPrefix(op, "$"), 0, 64)
		if err != nil {
			value = -1
		}
		return operandClass{kind: 'I', value: value}
	case strings.ContainsAny(op, "(:"):
		return operandClass{kind: 'M'}
	case strings.HasPrefix(op, "%"):
		return classifyRegister(strings.ToLower(strings.TrimPrefix(op, "%")))
	default:
		// Bare address or symbol: a jump/call target.
		return operandClass{kind: 'L'}
	}
}

var (
	gpr8  = map[string]bool{"al": true, "bl": true, "cl": true, "dl": true, "ah": true, "bh": true, "ch": true, "dh": true, "sil": true, "dil": true, "spl": true, "bpl": true}
	gpr16 = map[string]bool{"ax": true, "bx": true, "cx": true, "dx": true, "si": true, "di": true, "sp": true, "bp": true}
)

func classifyRegister(name string) operandClass {
	switch {
	case strings.HasPrefix(name, "xmm"):
		return operandClass{kind: 'V', name: "xmm", width: 128}
	case strings.HasPrefix(name, "ymm"):
		return operandClass{kind: 'V', name: "ymm", width: 256}
	case strings.HasPrefix(name, "zmm"):
		return operandClass{kind: 'V', name: "zmm", width: 512}
	case len(name) == 2 && name[0] == 'k' && '0' <= name[1] && name[1] <= '7':
		return operandClass{kind: 'K', name: name}
	case name == "cs" || name == "ds" || name == "es" || name == "fs" || name == "gs" || name == "ss":
		return operandClass{kind: 'S', name: name}
	case gpr8[name]:
		return operandClass{kind: 'R', name: name, width: 8}
	case gpr16[name]:
		return operandClass{kind: 'R', name: name, width: 16}
	case len(name) == 3 && name[0] == 'e' && gpr16[name[1:]]:
		return operandClass{kind: 'R', name: name, width: 32}
	case len(name) == 3 && name[0] == 'r' && gpr16[name[1:]], name == "rip":
		return operandClass{kind: 'R', name: name, width: 64}
	case len(name) >= 2 && name[0] == 'r' && '0' <= name[1] && name[1] <= '9':
		width := 64
		switch name[len(name)-1] {
		case 'd':
			width = 32
		case 'w':
			width = 16
		case 'b':
			width = 8
		}
		return operandClass{kind: 'R', name: name, width: width}
	case strings.HasPrefix(name, "st"):
		return operandClass{kind: 'O', name: "st"}
	case strings.HasPrefix(name, "mm"):
		return operandClass{kind: 'O', name: "mm"}
	default:
		return operandClass{kind: 'O', name: name}
	}
}

// gnuSuffixWidth maps a GNU integer-op size suffix (addq, movl, incw) to its
// operand width in bits; 0 when the mnemonic has no recognizable suffix.
func gnuSuffixWidth(mnemonic string) int {
	if len(mnemonic) < 3 {
		return 0
	}
	switch mnemonic[len(mnemonic)-1] {
	case 'B':
		return 8
	case 'W':
		return 16
	case 'L':
		return 32
	case 'Q':
		return 64
	}
	return 0
}
