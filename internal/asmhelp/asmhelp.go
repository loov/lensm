package asmhelp

import (
	"regexp"
	"slices"
	"strings"

	"loov.dev/lensm/internal/asmref"
)

type Help struct {
	Mnemonic    string
	Description string
	Explanation string
	// Ports lists execution-port usage (uops.info notation) for x86 mnemonics,
	// sourced from the generated reference. Empty for other architectures.
	Ports []string
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
	{Prefixes: []string{"MOVZX", "MOVSX", "MOVSXD"}, Description: "Move a value while extending it to a wider size.", Explain: explainMove},
	{Prefixes: []string{"MOV"}, Description: "Move or copy data between registers and memory.", Explain: explainMove},
	{Prefixes: []string{"LEA"}, Description: "Load an effective address without reading memory.", Explain: explainLEA},
	{Prefixes: []string{"ADD", "ADC"}, Description: "Add values; ADC also includes the carry flag.", Explain: explainBinary("+")},
	{Prefixes: []string{"SUB", "SBB"}, Description: "Subtract values; SBB also includes the borrow flag.", Explain: explainBinary("-")},
	{Prefixes: []string{"MUL", "IMUL"}, Description: "Multiply values (IMUL is signed multiplication).", Explain: explainBinary("*")},
	{Prefixes: []string{"MADD"}, Description: "Multiply two values and add a third.", Explain: explainMADD},
	{Prefixes: []string{"MSUB"}, Description: "Multiply two values and subtract the product from a third.", Explain: explainMSUB},
	{Prefixes: []string{"DIV", "IDIV", "SDIV", "UDIV"}, Description: "Divide values (signed or unsigned according to the mnemonic).", Explain: explainBinary("/")},
	{Prefixes: []string{"CQO", "CDQ", "CWD", "CBW"}, Description: "Sign-extend the accumulator for a wider signed operation."},
	{Prefixes: []string{"AND"}, Description: "Compute a bitwise AND.", Explain: explainBinary("&")},
	{Prefixes: []string{"OR", "ORR"}, Description: "Compute a bitwise OR.", Explain: explainBinary("|")},
	{Prefixes: []string{"BIC"}, Description: "Clear selected bits of a value.", Explain: explainBinary("&^")},
	{Prefixes: []string{"XOR", "EOR"}, Description: "Compute a bitwise exclusive OR.", Explain: explainBinary("^")},
	{Prefixes: []string{"SHL", "SAL", "LSL"}, Description: "Shift bits left.", Explain: explainBinary("<<")},
	{Prefixes: []string{"SHR", "LSR"}, Description: "Shift bits right, filling with zeroes.", Explain: explainBinary(">>")},
	{Prefixes: []string{"SAR", "ASR"}, Description: "Arithmetic right shift, preserving the sign.", Explain: explainBinary(">>")},
	{Prefixes: []string{"INC"}, Description: "Increment a value by one.", Explain: explainUnaryDelta("+", "1")},
	{Prefixes: []string{"DEC"}, Description: "Decrement a value by one.", Explain: explainUnaryDelta("-", "1")},
	{Prefixes: []string{"NEG"}, Description: "Negate a signed value.", Explain: explainUnaryPrefix("-")},
	{Prefixes: []string{"NOT", "MVN"}, Description: "Invert every bit in a value.", Explain: explainUnaryPrefix("^")},
	{Prefixes: []string{"CMP"}, Description: "Compare values and update condition flags.", ExplainArch: explainCompare},
	{Prefixes: []string{"CMOV"}, Description: "Conditionally move a value when the selected flags match.", Explain: explainMove},
	{Prefixes: []string{"SET"}, Description: "Set a byte to 0 or 1 according to condition flags."},
	{Prefixes: []string{"CSEL", "CSET"}, Description: "Select or set a value according to condition flags."},
	{Prefixes: []string{"TEST", "TST"}, Description: "Test bits and update condition flags without storing a result.", Explain: explainTest},
	{Prefixes: []string{"FMADD"}, Description: "Fused floating-point multiply-add with one rounding step.", Explain: explainFMADD},
	{Prefixes: []string{"FADD"}, Description: "Add floating-point values.", Explain: explainBinary("+")},
	{Prefixes: []string{"FSUB"}, Description: "Subtract floating-point values.", Explain: explainBinary("-")},
	{Prefixes: []string{"FMUL"}, Description: "Multiply floating-point values.", Explain: explainBinary("*")},
	{Prefixes: []string{"FMOV"}, Description: "Move a floating-point value.", Explain: explainMove},
	{Prefixes: []string{"LDR", "LDUR", "LOAD"}, Description: "Load a value from memory into a register.", Explain: explainLoad},
	{Prefixes: []string{"STR", "STUR", "STORE"}, Description: "Store a register value in memory.", Explain: explainStore},
	{Prefixes: []string{"ADR", "ADRP"}, Description: "Form a PC-relative address."},
	{Prefixes: []string{"LDP"}, Description: "Load a pair of registers from memory."},
	{Prefixes: []string{"STP"}, Description: "Store a pair of registers to memory."},
	{Prefixes: []string{"XCHG", "SWAP"}, Description: "Exchange two values."},
	{Prefixes: []string{"BSWAP", "REV"}, Description: "Reverse the byte order of a value."},
	{Prefixes: []string{"BSF", "BSR"}, Description: "Scan for the position of the first or last set bit."},
	{Prefixes: []string{"CLZ", "CTZ"}, Description: "Count leading or trailing zero bits."},
	{Prefixes: []string{"POPCNT"}, Description: "Count the one bits in a value."},
	{Prefixes: []string{"PUSH"}, Description: "Push a value onto the stack."},
	{Prefixes: []string{"POP"}, Description: "Pop the top stack value."},
	{Prefixes: []string{"CALL", "BL", "BLR", "JAL", "JALR"}, Description: "Call a function and save a return address."},
	{Prefixes: []string{"SYSCALL", "SVC"}, Description: "Enter the operating system or supervisor."},
	{Prefixes: []string{"INT"}, Description: "Raise a software interrupt."},
	{Prefixes: []string{"RET"}, Description: "Return to the caller."},
	{Prefixes: []string{"JMP", "B", "BR"}, Description: "Jump to another instruction."},
	{Prefixes: []string{"JE", "JZ", "BEQ"}, Description: "Jump when values are equal (zero flag set)."},
	{Prefixes: []string{"JNE", "JNZ", "BNE"}, Description: "Jump when values are not equal."},
	{Prefixes: []string{"JG", "JGE", "JL", "JLE", "BGT", "BGE", "BLT", "BLE"}, Description: "Conditional jump after a signed comparison."},
	{Prefixes: []string{"JA", "JAE", "JB", "JBE", "BHI", "BHS", "BLO", "BLS"}, Description: "Conditional jump after an unsigned comparison."},
	{Prefixes: []string{"BMI", "BPL", "BVS", "BVC"}, Description: "Conditional jump testing the sign or overflow flag."},
	{Prefixes: []string{"CBZ"}, Description: "Jump when a register is zero."},
	{Prefixes: []string{"CBNZ"}, Description: "Jump when a register is not zero."},
	{Prefixes: []string{"TBZ"}, Description: "Jump when a selected bit is zero."},
	{Prefixes: []string{"TBNZ"}, Description: "Jump when a selected bit is not zero."},
	{Prefixes: []string{"NOP"}, Description: "Do nothing for one instruction slot."},
	{Prefixes: []string{"HLT", "WFI"}, Description: "Stop or wait for an external event."},
	{Prefixes: []string{"PAUSE", "YIELD"}, Description: "Hint that the processor is in a spin-wait loop."},
	{Prefixes: []string{"MFENCE", "LFENCE", "SFENCE", "DMB", "DSB", "ISB"}, Description: "Order memory or instruction accesses across this barrier."},
}

// NativeAssemblyInstructionHelp returns reference text and a syntax-correct
// effect for the native (GNU) spelling of an instruction. GNU x86/AT&T uses
// source-first operands while ARM-family GNU syntax uses destination-first.
func ForNative(arch, text string) (Help, bool) {
	mnemonic, operands := splitAssemblyInstruction(text)
	lookup := canonicalNativeMnemonic(mnemonic)
	// Arch is irrelevant here: only the description is kept, the
	// explanation is replaced with a native-syntax rewrite below.
	help, ok := knownAssemblyInstructionHelp("", lookup, nil)
	if !ok {
		return Help{}, false
	}
	help.Mnemonic = mnemonic
	help.Explanation = explainNativeInstruction(lookup, operands)
	if help.Explanation == "" {
		help.Explanation = explainNativeEffect(lookup, operands)
	}
	if ref, ok := referenceEntry(mnemonic); ok && isX86(arch) {
		help.Ports = ref.Ports
	}
	return help, true
}

func canonicalNativeMnemonic(mnemonic string) string {
	switch {
	case mnemonic == "ADDS":
		return "ADD"
	case mnemonic == "SUBS":
		return "SUB"
	case mnemonic == "ANDS":
		return "AND"
	case strings.HasPrefix(mnemonic, "LDR"):
		return "LDR"
	case strings.HasPrefix(mnemonic, "LDUR"):
		return "LDUR"
	case strings.HasPrefix(mnemonic, "STR"):
		return "STR"
	case strings.HasPrefix(mnemonic, "STUR"):
		return "STUR"
	case strings.HasPrefix(mnemonic, "CMOV"):
		return "CMOV"
	case strings.HasPrefix(mnemonic, "SET"):
		return "SET"
	case strings.HasPrefix(mnemonic, "MOVZ"), strings.HasPrefix(mnemonic, "MOVSX"), strings.HasPrefix(mnemonic, "MOVSXD"):
		return "MOVZX"
	case mnemonic == "CQTO":
		return "CQO"
	case strings.HasPrefix(mnemonic, "B."):
		condition := strings.TrimPrefix(mnemonic, "B.")
		switch condition {
		case "EQ":
			return "BEQ"
		case "NE":
			return "BNE"
		case "GT":
			return "BGT"
		case "GE":
			return "BGE"
		case "LT":
			return "BLT"
		case "LE":
			return "BLE"
		case "HI":
			return "BHI"
		case "HS", "CS":
			return "BHS"
		case "LO", "CC":
			return "BLO"
		case "LS":
			return "BLS"
		case "MI":
			return "BMI"
		case "PL":
			return "BPL"
		case "VS":
			return "BVS"
		case "VC":
			return "BVC"
		case "AL", "NV":
			// Both encode "always" on modern cores.
			return "B"
		}
	}
	for _, base := range []string{"PUSH", "POP", "CALL", "JMP", "RET", "XCHG", "POPCNT", "BSWAP", "INC", "DEC", "NEG", "NOT"} {
		if mnemonic == base+"B" || mnemonic == base+"W" || mnemonic == base+"L" || mnemonic == base+"Q" {
			return base
		}
	}
	return mnemonic
}

func explainNativeEffect(mnemonic string, operands []string) string {
	operand := func(index int) string {
		if index >= len(operands) {
			return "target"
		}
		return strings.TrimSpace(strings.TrimPrefix(operands[index], "#"))
	}
	switch {
	case mnemonic == "PUSH":
		return "stack := push(stack, " + operand(0) + ")"
	case mnemonic == "POP":
		return operand(0) + " := pop(stack)"
	case mnemonic == "CALL" || mnemonic == "BL" || mnemonic == "BLR" || mnemonic == "JAL" || mnemonic == "JALR":
		return "call " + operand(0) + " and save the return address"
	case mnemonic == "RET":
		return "PC := saved return address"
	case mnemonic == "JMP" || mnemonic == "B" || mnemonic == "BR":
		return "PC := " + operand(0)
	case mnemonic == "JE" || mnemonic == "JZ" || mnemonic == "BEQ":
		return "if the compared values are equal (Z == 1), PC := " + operand(0)
	case mnemonic == "JNE" || mnemonic == "JNZ" || mnemonic == "BNE":
		return "if the compared values are not equal (Z == 0), PC := " + operand(0)
	case mnemonic == "BGT":
		return "if signed greater than (Z == 0 and N == V), PC := " + operand(0)
	case mnemonic == "BGE":
		return "if signed greater than or equal (N == V), PC := " + operand(0)
	case mnemonic == "BLT":
		return "if signed less than (N != V), PC := " + operand(0)
	case mnemonic == "BLE":
		return "if signed less than or equal (Z == 1 or N != V), PC := " + operand(0)
	case mnemonic == "JG":
		return "if signed greater than (ZF == 0 and SF == OF), PC := " + operand(0)
	case mnemonic == "JGE":
		return "if signed greater than or equal (SF == OF), PC := " + operand(0)
	case mnemonic == "JL":
		return "if signed less than (SF != OF), PC := " + operand(0)
	case mnemonic == "JLE":
		return "if signed less than or equal (ZF == 1 or SF != OF), PC := " + operand(0)
	case mnemonic == "BHI":
		return "if unsigned higher than (C == 1 and Z == 0), PC := " + operand(0)
	case mnemonic == "BHS":
		return "if unsigned higher than or equal (C == 1), PC := " + operand(0)
	case mnemonic == "BLO":
		return "if unsigned lower than (C == 0), PC := " + operand(0)
	case mnemonic == "BLS":
		return "if unsigned lower than or equal (C == 0 or Z == 1), PC := " + operand(0)
	case mnemonic == "BMI":
		return "if negative (N == 1), PC := " + operand(0)
	case mnemonic == "BPL":
		return "if positive or zero (N == 0), PC := " + operand(0)
	case mnemonic == "BVS":
		return "if overflow (V == 1), PC := " + operand(0)
	case mnemonic == "BVC":
		return "if no overflow (V == 0), PC := " + operand(0)
	case mnemonic == "JA":
		return "if unsigned above (CF == 0 and ZF == 0), PC := " + operand(0)
	case mnemonic == "JAE":
		return "if unsigned above or equal (CF == 0), PC := " + operand(0)
	case mnemonic == "JB":
		return "if unsigned below (CF == 1), PC := " + operand(0)
	case mnemonic == "JBE":
		return "if unsigned below or equal (CF == 1 or ZF == 1), PC := " + operand(0)
	case mnemonic == "CBZ":
		return "if " + operand(0) + " == 0, PC := " + operand(1)
	case mnemonic == "CBNZ":
		return "if " + operand(0) + " != 0, PC := " + operand(1)
	case mnemonic == "TBZ":
		return "if bit " + operand(1) + " of " + operand(0) + " == 0, PC := " + operand(2)
	case mnemonic == "TBNZ":
		return "if bit " + operand(1) + " of " + operand(0) + " != 0, PC := " + operand(2)
	case mnemonic == "INC":
		return operand(0) + " := " + operand(0) + " + 1"
	case mnemonic == "DEC":
		return operand(0) + " := " + operand(0) + " - 1"
	case mnemonic == "NEG":
		return operand(0) + " := -" + operand(0)
	case mnemonic == "NOT":
		return operand(0) + " := ^" + operand(0)
	case mnemonic == "SET":
		return operand(0) + " := condition ? 1 : 0"
	case mnemonic == "NOP":
		return "state is unchanged"
	case mnemonic == "SYSCALL" || mnemonic == "SVC" || mnemonic == "INT":
		return "transfer control to the operating system"
	case mnemonic == "MFENCE" || mnemonic == "LFENCE" || mnemonic == "SFENCE" || mnemonic == "DMB" || mnemonic == "DSB" || mnemonic == "ISB":
		return "wait for the ordered accesses to complete"
	case mnemonic == "PAUSE" || mnemonic == "YIELD":
		return "temporarily yield processor execution resources"
	case mnemonic == "HLT" || mnemonic == "WFI":
		return "wait until an interrupt or event occurs"
	case mnemonic == "CQO" || mnemonic == "CDQ" || mnemonic == "CWD" || mnemonic == "CBW":
		return "extend the accumulator's sign into the high half"
	case mnemonic == "XCHG" || mnemonic == "SWAP":
		return operand(0) + ", " + operand(1) + " := " + operand(1) + ", " + operand(0)
	}
	return ""
}

func explainNativeInstruction(mnemonic string, operands []string) string {
	if len(operands) == 0 {
		return ""
	}
	// Registers prefixed with % and immediates prefixed with $ identify GNU
	// x86/AT&T syntax, whose source-first operand order agrees with the
	// source-first Plan 9 rewrites (hence the empty arch: the "amd64" CMP
	// rewrite is natural-order, which would be wrong for AT&T).
	if strings.Contains(strings.Join(operands, " "), "%") || strings.HasPrefix(strings.TrimSpace(operands[0]), "$") {
		if help, ok := ForInstruction("", mnemonic+" "+strings.Join(operands, ", ")); ok {
			if help.Explanation != "" {
				return help.Explanation
			}
		}
		return explainNativeX86Effect(mnemonic, operands)
	}

	value := formatNativeValue
	if len(operands) >= 2 {
		destination := value(operands[0])
		switch {
		case mnemonic == "MOVZX", strings.HasPrefix(mnemonic, "MOV"):
			return destination + " := " + value(operands[1])
		case mnemonic == "LDR" || mnemonic == "LDUR" || mnemonic == "LOAD":
			return explainNativeLoad(destination, operands[1:])
		case mnemonic == "STR" || mnemonic == "STUR" || mnemonic == "STORE":
			return explainNativeStore(value(operands[0]), operands[1:])
		case mnemonic == "LDP" && len(operands) >= 3:
			return explainNativePairLoad(value(operands[0]), value(operands[1]), operands[2:])
		case mnemonic == "STP" && len(operands) >= 3:
			return explainNativePairStore(value(operands[0]), value(operands[1]), operands[2:])
		case mnemonic == "LEA", mnemonic == "ADR", mnemonic == "ADRP":
			return destination + " := address(" + value(operands[1]) + ")"
		case mnemonic == "ADD" || mnemonic == "ADC":
			return explainNativeDestinationFirst(destination, operands[1:], "+", value)
		case mnemonic == "SUB" || mnemonic == "SBB":
			return explainNativeDestinationFirst(destination, operands[1:], "-", value)
		case mnemonic == "MUL" || mnemonic == "IMUL" || mnemonic == "FMUL":
			return explainNativeDestinationFirst(destination, operands[1:], "*", value)
		case mnemonic == "MADD" && len(operands) >= 4:
			return destination + " := " + value(operands[1]) + " * " + value(operands[2]) + " + " + value(operands[3])
		case mnemonic == "MSUB" && len(operands) >= 4:
			return destination + " := " + value(operands[3]) + " - " + value(operands[1]) + " * " + value(operands[2])
		case mnemonic == "NEG":
			// arm64 two-operand form: neg x0, x1 is x0 := -x1.
			return destination + " := -" + value(operands[1])
		case mnemonic == "NOT" || mnemonic == "MVN":
			return destination + " := ^" + value(operands[1])
		case mnemonic == "DIV" || mnemonic == "IDIV" || mnemonic == "SDIV" || mnemonic == "UDIV":
			return explainNativeDestinationFirst(destination, operands[1:], "/", value)
		case mnemonic == "AND":
			return explainNativeDestinationFirst(destination, operands[1:], "&", value)
		case mnemonic == "BIC":
			return explainNativeDestinationFirst(destination, operands[1:], "&^", value)
		case mnemonic == "OR" || mnemonic == "ORR":
			return explainNativeDestinationFirst(destination, operands[1:], "|", value)
		case mnemonic == "XOR" || mnemonic == "EOR":
			return explainNativeDestinationFirst(destination, operands[1:], "^", value)
		case mnemonic == "SHL" || mnemonic == "SAL" || mnemonic == "LSL":
			return explainNativeDestinationFirst(destination, operands[1:], "<<", value)
		case mnemonic == "SHR" || mnemonic == "LSR" || mnemonic == "SAR" || mnemonic == "ASR":
			return explainNativeDestinationFirst(destination, operands[1:], ">>", value)
		case mnemonic == "FADD":
			return explainNativeDestinationFirst(destination, operands[1:], "+", value)
		case mnemonic == "FSUB":
			return explainNativeDestinationFirst(destination, operands[1:], "-", value)
		case mnemonic == "FMADD" && len(operands) >= 4:
			return destination + " := " + value(operands[1]) + " * " + value(operands[2]) + " + " + value(operands[3])
		case mnemonic == "CMP":
			return "flags := compare(" + value(operands[0]) + ", " + value(operands[1]) + ")"
		case mnemonic == "TEST" || mnemonic == "TST":
			return "flags := " + value(operands[0]) + " & " + value(operands[1])
		case mnemonic == "CSEL" && len(operands) >= 4:
			return destination + " := condition(" + value(operands[3]) + ") ? " + value(operands[1]) + " : " + value(operands[2])
		case mnemonic == "CSET":
			return destination + " := condition(" + value(operands[1]) + ") ? 1 : 0"
		case mnemonic == "POPCNT":
			return destination + " := countOneBits(" + value(operands[1]) + ")"
		case mnemonic == "BSWAP" || mnemonic == "REV":
			return destination + " := reverseBytes(" + value(operands[1]) + ")"
		case mnemonic == "BSF" || mnemonic == "BSR" || mnemonic == "CLZ" || mnemonic == "CTZ":
			return destination + " := significantBitPosition(" + value(operands[1]) + ")"
		}
	}
	return ""
}

func explainNativeX86Effect(mnemonic string, operands []string) string {
	operand := func(index int) string {
		if index >= len(operands) {
			return "target"
		}
		return formatNativeValue(operands[index])
	}
	destination := operand(len(operands) - 1)
	switch mnemonic {
	case "POPCNT":
		return destination + " := countOneBits(" + operand(0) + ")"
	case "BSWAP":
		return destination + " := reverseBytes(" + destination + ")"
	case "XCHG":
		return operand(0) + ", " + operand(1) + " := " + operand(1) + ", " + operand(0)
	case "SET":
		return destination + " := condition ? 1 : 0"
	case "CMOV":
		return "if condition, " + destination + " := " + operand(0)
	}
	return explainNativeEffect(mnemonic, operands)
}

func formatNativeValue(operand string) string {
	operand = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(operand), "#"))
	if strings.HasPrefix(operand, "[") {
		return formatNativeMemory(operand)
	}
	return operand
}

func formatNativeMemory(operand string) string {
	_, address, _ := parseNativeMemory(operand)
	return "memory[" + address + "]"
}

func explainNativeLoad(destination string, addressOperands []string) string {
	base, address, preIndex := parseNativeMemory(addressOperands[0])
	if preIndex {
		return base + " := " + address + "; " + destination + " := memory[" + base + "]"
	}
	effect := destination + " := memory[" + address + "]"
	if len(addressOperands) > 1 {
		effect += "; " + base + " := " + addNativeOffset(base, addressOperands[1])
	}
	return effect
}

func explainNativeStore(source string, addressOperands []string) string {
	base, address, preIndex := parseNativeMemory(addressOperands[0])
	if preIndex {
		return base + " := " + address + "; memory[" + base + "] := " + source
	}
	effect := "memory[" + address + "] := " + source
	if len(addressOperands) > 1 {
		effect += "; " + base + " := " + addNativeOffset(base, addressOperands[1])
	}
	return effect
}

func explainNativePairLoad(first, second string, addressOperands []string) string {
	base, address, preIndex := parseNativeMemory(addressOperands[0])
	load := first + ", " + second + " := pair(memory[" + address + "])"
	if preIndex {
		return base + " := " + address + "; " + first + ", " + second + " := pair(memory[" + base + "])"
	}
	if len(addressOperands) > 1 {
		load += "; " + base + " := " + addNativeOffset(base, addressOperands[1])
	}
	return load
}

func explainNativePairStore(first, second string, addressOperands []string) string {
	base, address, preIndex := parseNativeMemory(addressOperands[0])
	store := "memory[" + address + "] := pair(" + first + ", " + second + ")"
	if preIndex {
		return base + " := " + address + "; memory[" + base + "] := pair(" + first + ", " + second + ")"
	}
	if len(addressOperands) > 1 {
		store += "; " + base + " := " + addNativeOffset(base, addressOperands[1])
	}
	return store
}

func parseNativeMemory(operand string) (base, address string, preIndex bool) {
	operand = strings.TrimSpace(operand)
	preIndex = strings.HasSuffix(operand, "!")
	inside := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(operand, "["), "!"), "]"))
	parts := splitAssemblyOperands(inside)
	base = strings.TrimSpace(parts[0])
	address = base
	if len(parts) > 1 {
		address = addNativeOffset(base, strings.Join(parts[1:], ", "))
	}
	return base, address, preIndex
}

func addNativeOffset(base, offset string) string {
	offset = strings.TrimSpace(strings.ReplaceAll(offset, "#", ""))
	if after, ok := strings.CutPrefix(offset, "-"); ok {
		return base + " - " + after
	}
	return base + " + " + offset
}

func explainNativeDestinationFirst(destination string, sources []string, operator string, value func(string) string) string {
	if len(sources) == 1 {
		return destination + " := " + destination + " " + operator + " " + value(sources[0])
	}
	return destination + " := " + value(sources[0]) + " " + operator + " " + value(sources[1])
}

func ForInstruction(arch, text string) (Help, bool) {
	mnemonic, operands := splitAssemblyInstruction(text)
	if mnemonic == "" {
		return Help{}, false
	}
	ref, hasRef := referenceEntry(mnemonic)

	help, ok := knownAssemblyInstructionHelp(arch, mnemonic, operands)
	switch {
	case ok:
		// Curated rules own the bespoke Explanation semantics; keep them.
	case hasRef:
		// For mnemonics the rules don't cover, fall back to the generated
		// reference so the tooltip shows real ARM/x86 text.
		help, ok = Help{Mnemonic: mnemonic, Description: referenceBrief(ref)}, true
	case plausibleMnemonic(mnemonic):
		help = Help{Mnemonic: mnemonic, Description: "Execute the " + mnemonic + " instruction."}
	default:
		return Help{}, false
	}
	// Ports are x86-only in the reference; the table merges ARM and x86 under
	// one mnemonic key, so gate by arch to avoid showing x86 ports for arm64.
	if hasRef && isX86(arch) {
		help.Ports = ref.Ports
	}
	return help, true
}

func isX86(arch string) bool {
	return arch == "386" || arch == "amd64"
}

// referenceEntry looks up the generated reference for a mnemonic, tolerating
// Plan 9 (Go assembler) size suffixes: the table is keyed by base mnemonic
// (ADD, CRC32) while the disassembly shows ADDQ, MOVD, CRC32Q, etc.
func referenceEntry(mnemonic string) (asmref.Entry, bool) {
	if e, ok := asmref.Lookup(mnemonic); ok {
		return e, true
	}
	if base := trimGoAsmSuffix(mnemonic); base != mnemonic {
		if e, ok := asmref.Lookup(base); ok {
			return e, true
		}
	}
	return asmref.Entry{}, false
}

// trimGoAsmSuffix removes a single trailing Go-assembler operand-size suffix.
// Longer suffixes (BU/HU/WU) are tried before single letters. Only used after
// an exact lookup misses, so stripping a genuine trailing letter is harmless.
func trimGoAsmSuffix(mnemonic string) string {
	for _, suffix := range []string{"BU", "HU", "WU", "B", "W", "L", "Q", "D", "H", "S"} {
		if len(mnemonic) > len(suffix) && strings.HasSuffix(mnemonic, suffix) {
			return mnemonic[:len(mnemonic)-len(suffix)]
		}
	}
	return mnemonic
}

// referenceBrief returns a short human description for an entry, preferring the
// brief title over the full first paragraph.
func referenceBrief(ref asmref.Entry) string {
	if ref.Brief != "" {
		return ref.Brief
	}
	return ref.Description
}

// plausibleMnemonic reports whether a token looks like an instruction
// mnemonic. Undecodable bytes render as "?", which must not get an
// authoritative-sounding "Execute the ? instruction." fallback.
func plausibleMnemonic(mnemonic string) bool {
	if r := mnemonic[0]; r < 'A' || r > 'Z' {
		return false
	}
	for _, r := range mnemonic {
		switch {
		case 'A' <= r && r <= 'Z', '0' <= r && r <= '9', r == '.', r == '_':
		default:
			return false
		}
	}
	return true
}

func knownAssemblyInstructionHelp(arch, mnemonic string, operands []string) (Help, bool) {
	// Resolve exact mnemonics first. Otherwise BLS can be mistaken for BL with
	// an S size suffix, and similar prefix collisions produce wrong semantics.
	for _, rule := range asmInstructionRules {
		if slices.Contains(rule.Prefixes, mnemonic) {
			return assemblyHelpFromRule(arch, mnemonic, operands, rule), true
		}
	}
	for _, rule := range asmInstructionRules {
		for _, prefix := range rule.Prefixes {
			if mnemonicMatches(mnemonic, prefix) {
				return assemblyHelpFromRule(arch, mnemonic, operands, rule), true
			}
		}
	}
	return Help{}, false
}

func assemblyHelpFromRule(arch, mnemonic string, operands []string, rule asmInstructionRule) Help {
	help := Help{Mnemonic: mnemonic, Description: rule.Description}
	switch {
	case rule.ExplainArch != nil:
		help.Explanation = rule.ExplainArch(arch, operands)
	case rule.Explain != nil:
		help.Explanation = rule.Explain(operands)
	}
	return help
}

func mnemonicMatches(mnemonic, prefix string) bool {
	if mnemonic == prefix {
		return true
	}
	if !strings.HasPrefix(mnemonic, prefix) {
		return false
	}
	suffix := strings.TrimPrefix(mnemonic, prefix)
	return nativeSizeSuffixes(prefix)[suffix]
}

// Suffix sets are package-level: nativeSizeSuffixes runs in the per-frame
// hover lookup and must not allocate.
var (
	x86IntegerSuffixes    = map[string]bool{"B": true, "W": true, "L": true, "Q": true}
	floatOrVectorSuffixes = map[string]bool{"S": true, "D": true}
	// arm64 Go assembly sizes moves with D/H/W/B plus zero-extending
	// BU/HU/WU: MOVD, MOVWU, ... The x86 sizes stay valid alongside.
	movSuffixes = map[string]bool{
		"B": true, "W": true, "L": true, "Q": true,
		"D": true, "H": true, "BU": true, "HU": true, "WU": true,
	}
	// arm64 32-bit register variants: SDIVW, MADDW, LSLW, ...
	arm64WordSuffixes = map[string]bool{"W": true}
)

func nativeSizeSuffixes(mnemonic string) map[string]bool {
	switch mnemonic {
	case "MOV":
		return movSuffixes
	case "FMOV":
		return floatOrVectorSuffixes
	case "LEA", "ADD", "ADC", "SUB", "SBB", "MUL", "IMUL", "DIV", "IDIV",
		"AND", "OR", "XOR", "SHL", "SAL", "SHR", "SAR", "INC", "DEC", "NEG", "NOT",
		"CMP", "TEST", "PUSH", "POP", "CALL", "RET", "XCHG", "BSWAP", "POPCNT":
		return x86IntegerSuffixes
	case "SDIV", "UDIV", "MADD", "MSUB", "ORR", "EOR", "LSL", "LSR", "ASR", "BIC", "MVN":
		return arm64WordSuffixes
	case "FADD", "FSUB", "FMUL", "FMADD":
		return floatOrVectorSuffixes
	default:
		return nil
	}
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
	parts := splitAssemblyOperands(rest)
	operands := make([]string, 0, len(parts))
	for _, part := range parts {
		operands = append(operands, strings.TrimSpace(part))
	}
	return mnemonic, operands
}

func splitAssemblyOperands(text string) []string {
	var parts []string
	start, depth := 0, 0
	for index, char := range text {
		switch char {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, text[start:index])
				start = index + 1
			}
		}
	}
	return append(parts, text[start:])
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

// Go arm64 assembly orders MADD/MSUB as "Rm, Ra, Rn, Rd":
// MADD R2, R3, R1, R0 computes R0 = R1*R2 + R3.
func explainMADD(operands []string) string {
	if len(operands) < 4 {
		return ""
	}
	destination := formatDestination(operands[3])
	return destination + " := " + formatValue(operands[2]) + " * " + formatValue(operands[0]) + " + " + formatValue(operands[1])
}

func explainMSUB(operands []string) string {
	if len(operands) < 4 {
		return ""
	}
	destination := formatDestination(operands[3])
	return destination + " := " + formatValue(operands[1]) + " - " + formatValue(operands[2]) + " * " + formatValue(operands[0])
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
	if after, ok := strings.CutPrefix(operand, "$"); ok {
		return after
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
