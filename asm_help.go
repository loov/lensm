package main

import (
	"regexp"
	"strings"
)

type AssemblyHelp struct {
	Mnemonic    string
	Description string
	Explanation string
}

type asmInstructionRule struct {
	Prefixes    []string
	Description string
	Explain     func([]string) string
	// ExplainArch is used instead of Explain when the operand meaning
	// depends on the architecture.
	ExplainArch func(arch string, operands []string) string
}

// asmInstructionRules is intentionally data-driven: add a prefix and, when
// useful, a small operand rewrite to extend the built-in reference.
var asmInstructionRules = []asmInstructionRule{
	{Prefixes: []string{"MOV"}, Description: "Move or copy data between registers and memory.", Explain: explainMove},
	{Prefixes: []string{"LEA"}, Description: "Load an effective address without reading memory.", Explain: explainLEA},
	{Prefixes: []string{"ADD", "ADC"}, Description: "Add values; ADC also includes the carry flag.", Explain: explainBinary("+")},
	{Prefixes: []string{"SUB", "SBB"}, Description: "Subtract values; SBB also includes the borrow flag.", Explain: explainBinary("-")},
	{Prefixes: []string{"MUL", "IMUL"}, Description: "Multiply values (IMUL is signed multiplication).", Explain: explainBinary("*")},
	{Prefixes: []string{"DIV", "IDIV"}, Description: "Divide values (IDIV is signed division).", Explain: explainBinary("/")},
	{Prefixes: []string{"AND"}, Description: "Compute a bitwise AND.", Explain: explainBinary("&")},
	{Prefixes: []string{"OR"}, Description: "Compute a bitwise OR.", Explain: explainBinary("|")},
	{Prefixes: []string{"XOR", "EOR"}, Description: "Compute a bitwise exclusive OR.", Explain: explainBinary("^")},
	{Prefixes: []string{"SHL", "SAL", "LSL"}, Description: "Shift bits left.", Explain: explainBinary("<<")},
	{Prefixes: []string{"SHR", "LSR"}, Description: "Shift bits right, filling with zeroes.", Explain: explainBinary(">>")},
	{Prefixes: []string{"SAR", "ASR"}, Description: "Arithmetic right shift, preserving the sign.", Explain: explainBinary(">>")},
	{Prefixes: []string{"INC"}, Description: "Increment a value by one.", Explain: explainUnaryDelta("+", "1")},
	{Prefixes: []string{"DEC"}, Description: "Decrement a value by one.", Explain: explainUnaryDelta("-", "1")},
	{Prefixes: []string{"NEG"}, Description: "Negate a signed value.", Explain: explainUnaryPrefix("-")},
	{Prefixes: []string{"NOT"}, Description: "Invert every bit in a value.", Explain: explainUnaryPrefix("^")},
	{Prefixes: []string{"CMP"}, Description: "Compare values and update condition flags.", ExplainArch: explainCompare},
	{Prefixes: []string{"TEST", "TST"}, Description: "Test bits and update condition flags without storing a result.", Explain: explainTest},
	{Prefixes: []string{"FMADD"}, Description: "Fused floating-point multiply-add with one rounding step.", Explain: explainFMADD},
	{Prefixes: []string{"FADD"}, Description: "Add floating-point values.", Explain: explainBinary("+")},
	{Prefixes: []string{"FSUB"}, Description: "Subtract floating-point values.", Explain: explainBinary("-")},
	{Prefixes: []string{"FMUL"}, Description: "Multiply floating-point values.", Explain: explainBinary("*")},
	{Prefixes: []string{"LDR", "LDUR", "LOAD"}, Description: "Load a value from memory into a register.", Explain: explainLoad},
	{Prefixes: []string{"STR", "STUR", "STORE"}, Description: "Store a register value in memory.", Explain: explainStore},
	{Prefixes: []string{"PUSH"}, Description: "Push a value onto the stack."},
	{Prefixes: []string{"POP"}, Description: "Pop the top stack value."},
	{Prefixes: []string{"CALL", "BL", "JAL"}, Description: "Call a function and save a return address."},
	{Prefixes: []string{"RET"}, Description: "Return to the caller."},
	{Prefixes: []string{"JMP", "B"}, Description: "Jump to another instruction."},
	{Prefixes: []string{"JE", "JZ", "BEQ"}, Description: "Jump when values are equal (zero flag set)."},
	{Prefixes: []string{"JNE", "JNZ", "BNE"}, Description: "Jump when values are not equal."},
	{Prefixes: []string{"JG", "JGE", "JL", "JLE", "BGT", "BGE", "BLT", "BLE"}, Description: "Conditional jump after a signed comparison."},
	{Prefixes: []string{"JA", "JAE", "JB", "JBE", "BHI", "BHS", "BLO", "BLS"}, Description: "Conditional jump after an unsigned comparison."},
	{Prefixes: []string{"CBZ"}, Description: "Jump when a register is zero."},
	{Prefixes: []string{"CBNZ"}, Description: "Jump when a register is not zero."},
	{Prefixes: []string{"NOP"}, Description: "Do nothing for one instruction slot."},
}

func AssemblyInstructionHelp(arch, text string) (AssemblyHelp, bool) {
	mnemonic, operands := splitAssemblyInstruction(text)
	if mnemonic == "" {
		return AssemblyHelp{}, false
	}
	for _, rule := range asmInstructionRules {
		for _, prefix := range rule.Prefixes {
			if mnemonicMatches(mnemonic, prefix) {
				help := AssemblyHelp{Mnemonic: mnemonic, Description: rule.Description}
				switch {
				case rule.ExplainArch != nil:
					help.Explanation = rule.ExplainArch(arch, operands)
				case rule.Explain != nil:
					help.Explanation = rule.Explain(operands)
				}
				return help, true
			}
		}
	}
	return AssemblyHelp{}, false
}

func mnemonicMatches(mnemonic, prefix string) bool {
	if mnemonic == prefix {
		return true
	}
	if !strings.HasPrefix(mnemonic, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(mnemonic, prefix)
	return suffix == "B" || suffix == "W" || suffix == "L" || suffix == "Q" || suffix == "S" || suffix == "D"
}

func splitAssemblyInstruction(text string) (string, []string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	if index := strings.Index(text, "\t"); index >= 0 {
		text = text[:index]
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", nil
	}
	mnemonic := strings.ToUpper(strings.TrimSpace(fields[0]))
	rest := strings.TrimSpace(strings.TrimPrefix(text, fields[0]))
	if rest == "" {
		return mnemonic, nil
	}
	parts := strings.Split(rest, ",")
	operands := make([]string, 0, len(parts))
	for _, part := range parts {
		operands = append(operands, strings.TrimSpace(part))
	}
	return mnemonic, operands
}

func explainMove(operands []string) string {
	if len(operands) < 2 {
		return ""
	}
	return formatDestination(operands[len(operands)-1]) + " := " + formatValue(operands[0])
}

func explainLEA(operands []string) string {
	if len(operands) < 2 {
		return ""
	}
	return formatDestination(operands[len(operands)-1]) + " := " + formatAddress(operands[0])
}

func explainBinary(operator string) func([]string) string {
	return func(operands []string) string {
		if len(operands) < 2 {
			return ""
		}
		destination := formatDestination(operands[len(operands)-1])
		if len(operands) == 2 {
			return destination + " := " + destination + " " + operator + " " + formatValue(operands[0])
		}
		// Three-operand Go assembly computes dst := second op first:
		// SUB R1, R5, R3 is R3 = R5 - R1, LSL $3, R5, R3 is R3 = R5 << 3.
		return destination + " := " + formatValue(operands[len(operands)-2]) + " " + operator + " " + formatValue(operands[0])
	}
}

func explainUnaryDelta(operator, amount string) func([]string) string {
	return func(operands []string) string {
		if len(operands) == 0 {
			return ""
		}
		destination := formatDestination(operands[len(operands)-1])
		return destination + " := " + destination + " " + operator + " " + amount
	}
}

func explainUnaryPrefix(operator string) func([]string) string {
	return func(operands []string) string {
		if len(operands) == 0 {
			return ""
		}
		destination := formatDestination(operands[len(operands)-1])
		return destination + " := " + operator + destination
	}
}

func explainCompare(arch string, operands []string) string {
	if len(operands) < 2 {
		return ""
	}
	// Go assembly for x86 deliberately keeps CMP operands in natural
	// left-to-right order (CMPQ AX, BX sets flags from AX-BX; see
	// go.dev/issue/60920), while arm64 and the other ports use
	// source-first order (CMP R1, R4 sets flags from R4-R1).
	if arch == "386" || arch == "amd64" {
		return "flags := compare(" + formatValue(operands[0]) + ", " + formatValue(operands[1]) + ")"
	}
	return "flags := compare(" + formatValue(operands[1]) + ", " + formatValue(operands[0]) + ")"
}

func explainTest(operands []string) string {
	if len(operands) < 2 {
		return ""
	}
	return "flags := " + formatValue(operands[1]) + " & " + formatValue(operands[0])
}

func explainFMADD(operands []string) string {
	if len(operands) < 4 {
		return ""
	}
	destination := formatDestination(operands[3])
	return destination + " := " + formatValue(operands[0]) + " * " + formatValue(operands[2]) + " + " + formatValue(operands[1])
}

func explainLoad(operands []string) string {
	if len(operands) < 2 {
		return ""
	}
	return formatDestination(operands[len(operands)-1]) + " := " + formatValue(operands[0])
}

func explainStore(operands []string) string {
	if len(operands) < 2 {
		return ""
	}
	return "memory[" + formatAddress(operands[len(operands)-1]) + "] := " + formatValue(operands[0])
}

var plan9Address = regexp.MustCompile(`^([^()]*)\(([^)]+)\)(?:\(([^)]+)\))?$`)

func formatValue(operand string) string {
	operand = strings.TrimSpace(operand)
	if strings.HasPrefix(operand, "$") {
		return strings.TrimPrefix(operand, "$")
	}
	if plan9Address.MatchString(operand) {
		return "memory[" + formatAddress(operand) + "]"
	}
	return operand
}

func formatDestination(operand string) string {
	operand = strings.TrimSpace(operand)
	if plan9Address.MatchString(operand) {
		return "memory[" + formatAddress(operand) + "]"
	}
	return operand
}

func formatAddress(operand string) string {
	operand = strings.TrimSpace(strings.TrimPrefix(operand, "$"))
	match := plan9Address.FindStringSubmatch(operand)
	if len(match) == 0 {
		return operand
	}
	displacement := strings.TrimSpace(match[1])
	base := strings.TrimSpace(match[2])
	index := strings.TrimSpace(match[3])

	var terms []string
	if base != "" && base != "SB" {
		terms = append(terms, base)
	}
	if index != "" {
		index = strings.ReplaceAll(index, "*", " * ")
		terms = append(terms, index)
	}
	if displacement != "" && displacement != "0" {
		terms = append(terms, displacement)
	}
	if len(terms) == 0 {
		return displacement
	}
	return strings.Join(terms, " + ")
}
