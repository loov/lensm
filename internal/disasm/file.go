package disasm

// File represents an object file, a module or anything that contains functions.
type File interface {
	Close() error
	Funcs() []Func
}

type Func interface {
	Name() string
	Load(opt Options) *Code
}

type Options struct {
	Context int
}
