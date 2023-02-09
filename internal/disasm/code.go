package disasm

type Code struct {
	Name string
	File string

	Insts   []Inst
	MaxJump int

	Source []Source
}

// Inst represents a single instruction.
type Inst struct {
	PC   uint64
	Text string
	File string
	Line int

	RefPC     uint64
	RefOffset int
	RefStack  int

	Call string
}

type Source struct {
	File   string
	Blocks []SourceBlock
}

type SourceBlock struct {
	LineRange
	Lines   []string
	Related [][]LineRange // for each line, a range index in Code.Insts
}
