package disasm

// File represents an object file, a module or anything that contains functions.
type File interface {
	// Close closes the underlying data.
	Close() error
	// Funcs enumerates all the visualizable code blocks.
	Funcs() []Func
}

// Func represents a function or method that can be independently rendered.
type Func interface {
	// Name is the name of the func.
	Name() string
	// Load loads the source code and disassembles it.
	Load(opt Options) *Code
}

// Options defines configuration for loading the func.
type Options struct {
	// Context is the number of lines that should be additionally included for context.
	// This can often contain function documentation.
	Context int
}
