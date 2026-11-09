package disasm

// Index is what a scan of every function in a file finds.
type Index struct {
	// Callers maps a function's name to the names of the functions
	// that call it, sorted and each once.
	Callers map[string][]string
	// Stats are per function, in File.Funcs order.
	Stats []FuncStats
}

// FuncStats sums up one function.
type FuncStats struct {
	// Size is the function's machine code in bytes.
	Size uint64
	// Marks counts the instructions with each mark, in Marks order,
	// as CountedMarks counts them.
	Marks [len(Marks)]int
}

// Indexer is a File that can scan all its functions. The scan
// disassembles everything, so run it off the UI goroutine.
type Indexer interface {
	Index() *Index
}

// Index scans the file, or returns nil when it cannot.
func (s *Shared) Index() *Index {
	if ix, ok := s.file.(Indexer); ok {
		return ix.Index()
	}
	return nil
}
