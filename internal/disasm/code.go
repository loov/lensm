package disasm

// Code combines the disassembly and the source code mapping.
type Code struct {
	// Name is the name of the code block, e.g. function or method name.
	Name string
	// File is where the code is located.
	File string
	// Arch is the GOARCH of the disassembled binary.
	Arch string

	// Insts is the slice of a all instructions in the code.
	Insts []Inst
	// MaxJump is the maximum layers of jumps for the codeblock.
	// This is used to determine how much padding is created for
	// the jump lines.
	MaxJump int

	// Source is the slice of a codeblocks that were used to create the instructions.
	Source []Source
}

// Inst represents a single instruction.
type Inst struct {
	// PC is the program counter, usually offset in the binary.
	PC uint64
	// Text is the textual representation of this instruction.
	Text string
	// NativeText is the native assembler syntax for this instruction.
	NativeText string
	// Mnemonic is the canonical decoder mnemonic (e.g. "LD1" where Text spells
	// it "VLD1"), used for reference lookups. Empty for undecodable bytes.
	Mnemonic string
	// File is the location where this instruction was compiled from.
	File string
	// Line is the line in the file where this instruction was compiled from.
	Line int
	// Indent is the nesting depth, e.g. of wasm blocks. Viewers indent
	// Text by it.
	Indent int

	// RefPC is a reference to another program counter, e.g. a call.
	RefPC uint64
	// RefOffset is a reference to a relative jump.
	RefOffset int
	// RefStack is the depth that the jump line should be drawn at.
	RefStack int

	// Call is a named target that should be present in Funcs.
	// This is used to make the instruction clickable and follow to the
	// called target.
	Call string

	// Marks flags what the instruction costs at run time.
	Marks Mark
}

// Mark is a set of run time costs found in an instruction.
type Mark uint8

const (
	// MarkSpill stores a register to the stack.
	MarkSpill Mark = 1 << iota
	// MarkReload loads a register from the stack.
	MarkReload
	// MarkBoundsCheck branches to or calls a bounds check failure.
	MarkBoundsCheck
	// MarkAlloc calls a heap allocator.
	MarkAlloc
	// MarkWriteBarrier calls a garbage collector write barrier.
	MarkWriteBarrier

	// AllMarks is every mark.
	AllMarks = MarkSpill | MarkReload | MarkBoundsCheck | MarkAlloc | MarkWriteBarrier
)

// Marks lists every mark in display order.
var Marks = []Mark{MarkSpill, MarkReload, MarkBoundsCheck, MarkAlloc, MarkWriteBarrier}

// String is a short label for a single mark.
func (m Mark) String() string {
	switch m {
	case MarkSpill:
		return "spill"
	case MarkReload:
		return "reload"
	case MarkBoundsCheck:
		return "bounds"
	case MarkAlloc:
		return "alloc"
	case MarkWriteBarrier:
		return "wb"
	}
	return "?"
}

// Source represents code from a single file.
type Source struct {
	// File is the file name for the source code.
	File string
	// Blocks is a slice of blocks that were used for compiling the instructions.
	Blocks []SourceBlock
}

// SourceBlock represents a single sequential codeblock that references the instructions.
type SourceBlock struct {
	// LineRange is the range of lines that it references from the file.
	LineRange
	// Lines are textual representation of the source starting from `LineRange.From`.
	Lines []string
	// Related contains a set of ranges in the instructions.
	// e.g. for source code Lines[5] there will be drawn relation shapes to each
	// instructions `for _, r := range Related[5] { draw(Insts[r.From:r.To]) }`
	Related [][]LineRange
}
