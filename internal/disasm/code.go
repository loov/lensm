package disasm

// Code combines the disassembly and the source code mapping.
type Code struct {
	// Name is the name of the code block, e.g. function or method name.
	Name string
	// File is where the code is located.
	File string
	// Arch is the GOARCH of the disassembled binary.
	Arch string
	// Statements marks the instructions' NativeText as source-like
	// statements instead of native assembly, as wasm has: each nests
	// by Indent as Text does, and a row that only feeds the next has
	// none.
	Statements bool

	// Insts is the slice of a all instructions in the code.
	Insts []Inst
	// MaxJump is the maximum layers of jumps for the codeblock.
	// This is used to determine how much padding is created for
	// the jump lines.
	MaxJump int
	// jumpsFar is the far argument of the last LayoutJumps, 0 before.
	jumpsFar int

	// Source is the slice of a codeblocks that were used to create the instructions.
	Source []Source
}

// Inst represents a single instruction.
type Inst struct {
	// PC is the program counter, usually offset in the binary.
	PC uint64
	// Text is the textual representation of this instruction.
	Text string
	// NativeText is the native assembler syntax for this instruction,
	// or with Code.Statements the statement it completes.
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
	// RefStack is the column, counted from the code outwards, that the
	// jump line, or a far jump's outgoing stub, is drawn in.
	RefStack int
	// RefFar marks a jump too long to draw whole. It gets a short stub
	// here and another at its target, in the target's InStack column.
	RefFar bool
	// InStack is the column of the stub that far jumps to this
	// instruction arrive through, 0 when none do.
	InStack int

	// Call is a named target that should be present in Funcs.
	// This is used to make the instruction clickable and follow to the
	// called target.
	Call string

	// Marks flags what the instruction costs at run time.
	Marks Mark

	// Stack is what a stack machine, such as wasm, holds before the
	// instruction runs, the top first.
	Stack []string
}

// CountedMarks are the marks of ix that count towards totals. A bounds
// check marks its branches and its failure call, but counts once, at
// the call.
func (ix *Inst) CountedMarks() Mark {
	if ix.Call == "" {
		return ix.Marks &^ MarkBoundsCheck
	}
	return ix.Marks
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
var Marks = [...]Mark{MarkSpill, MarkReload, MarkBoundsCheck, MarkAlloc, MarkWriteBarrier}

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
	// Notes are the compiler's notes for each line, parallel to Lines.
	// Only a binary that lensm built has them.
	Notes [][]Note
	// Related contains a set of ranges in the instructions.
	// e.g. for source code Lines[5] there will be drawn relation shapes to each
	// instructions `for _, r := range Related[5] { draw(Insts[r.From:r.To]) }`
	Related [][]LineRange
}
