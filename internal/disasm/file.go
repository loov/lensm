package disasm

// File represents an object file, a module or anything that contains functions.
type File interface {
	// Close closes the underlying data.
	Close() error
	// Funcs enumerates all the visualizable code blocks.
	Funcs() []Func
}

// FindFunc returns the first function in file called name, or nil.
// Names need not be unique, a Go ABI wrapper can share its function's,
// so tabs, history and comments that refer to a name get the first.
func FindFunc(file File, name string) Func {
	for _, fn := range file.Funcs() {
		if fn.Name() == name {
			return fn
		}
	}
	return nil
}

// Func represents a function or method that can be independently rendered.
type Func interface {
	// Name is the name of the func.
	Name() string
	// Load loads the source code and disassembles it.
	Load(opt Options) (*Code, error)
}

// Options defines configuration for loading the func.
type Options struct {
	// Context is the number of lines that should be additionally included for context.
	// This can often contain function documentation.
	Context int
}
