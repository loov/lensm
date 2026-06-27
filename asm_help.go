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
}

// asmInstructionRules is intentionally data-driven: add a prefix and, when
// useful, a small operand rewrite to extend the built-in reference.
var asmInstructionRules = []asmInstructionRule{
	{[]string{"MOV"}, "Move or copy data between registers and memory.", explainMove},
	{[]string{"LEA"}, "Load an effective address without reading memory.", explainLEA},
	{[]string{"ADD", "ADC"}, "Add values; ADC also includes the carry flag.", explainBinary("+")},
	{[]string{"SUB", "SBB"}, "Subtract values; SBB also includes the borrow flag.", explainBinary("-")},
	{[]string{"MUL", "IMUL"}, "Multiply values (IMUL is signed multiplication).", explainBinary("*")},
	{[]string{"DIV", "IDIV"}, "Divide values (IDIV is signed division).", explainBinary("/")},
	{[]string{"AND"}, "Compute a bitwise AND.", explainBinary("&")},
	{[]string{"OR"}, "Compute a bitwise OR.", explainBinary("|")},
	{[]string{"XOR", "EOR"}, "Compute a bitwise exclusive OR.", explainBinary("^")},
	{[]string{"SHL", "SAL", "LSL"}, "Shift bits left.", explainBinary("<<")},
	{[]string{"SHR", "LSR"}, "Shift bits right, filling with zeroes.", explainBinary(">>")},
	{[]string{"SAR", "ASR"}, "Arithmetic right shift, preserving the sign.", explainBinary(">>")},
	{[]string{"INC"}, "Increment a value by one.", explainUnaryDelta("+", "1")},
	{[]string{"DEC"}, "Decrement a value by one.", explainUnaryDelta("-", "1")},
	{[]string{"NEG"}, "Negate a signed value.", explainUnaryPrefix("-")},
	{[]string{"NOT"}, "Invert every bit in a value.", explainUnaryPrefix("^")},
	{[]string{"CMP"}, "Compare values and update condition flags.", explainCompare},
	{[]string{"TEST", "TST"}, "Test bits and update condition flags without storing a result.", explainTest},
	{[]string{"FMADD"}, "Fused floating-point multiply-add with one rounding step.", explainFMADD},
	{[]string{"FADD"}, "Add floating-point values.", explainBinary("+")},
	{[]string{"FSUB"}, "Subtract floating-point values.", explainBinary("-")},
	{[]string{"FMUL"}, "Multiply floating-point values.", explainBinary("*")},
	{[]string{"LDR", "LDUR", "LOAD"}, "Load a value from memory into a register.", explainLoad},
	{[]string{"STR", "STUR", "STORE"}, "Store a register value in memory.", explainStore},
	{[]string{"PUSH"}, "Push a value onto the stack.", nil},
	{[]string{"POP"}, "Pop the top stack value.", nil},
	{[]string{"CALL", "BL", "JAL"}, "Call a function and save a return address.", nil},
	{[]string{"RET"}, "Return to the caller.", nil},
	{[]string{"JMP", "B"}, "Jump to another instruction.", nil},
	{[]string{"JE", "JZ", "BEQ"}, "Jump when values are equal (zero flag set).", nil},
	{[]string{"JNE", "JNZ", "BNE"}, "Jump when values are not equal.", nil},
	{[]string{"JG", "JGE", "JL", "JLE", "BGT", "BGE", "BLT", "BLE"}, "Conditional jump after a signed comparison.", nil},
	{[]string{"JA", "JAE", "JB", "JBE", "BHI", "BHS", "BLO", "BLS"}, "Conditional jump after an unsigned comparison.", nil},
	{[]string{"CBZ"}, "Jump when a register is zero.", nil},
	{[]string{"CBNZ"}, "Jump when a register is not zero.", nil},
	{[]string{"NOP"}, "Do nothing for one instruction slot.", nil},
}

func AssemblyInstructionHelp(text string) (AssemblyHelp, bool) {
	mnemonic, operands := splitAssemblyInstruction(text)
	if mnemonic == "" {
		return AssemblyHelp{}, false
	}
	for _, rule := range asmInstructionRules {
		for _, prefix := range rule.Prefixes {
			if mnemonicMatches(mnemonic, prefix) {
				help := AssemblyHelp{Mnemonic: mnemonic, Description: rule.Description}
				if rule.Explain != nil {
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
		return destination + " := " + formatValue(operands[0]) + " " + operator + " " + formatValue(operands[1])
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

func explainCompare(operands []string) string {
	if len(operands) < 2 {
		return ""
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
