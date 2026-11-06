package disasm

import "sync/atomic"

// Shared is a File held by several owners, such as the GUI and the MCP
// server. Each Acquire needs a matching Close; the last Close closes the
// underlying file.
type Shared struct {
	file File
	refs atomic.Int32
}

// Share wraps file with one reference held by the caller.
func Share(file File) *Shared {
	s := &Shared{file: file}
	s.refs.Store(1)
	return s
}

// Acquire adds a reference and returns s.
func (s *Shared) Acquire() *Shared {
	s.refs.Add(1)
	return s
}

func (s *Shared) Funcs() []Func { return s.file.Funcs() }

func (s *Shared) Close() error {
	if s.refs.Add(-1) == 0 {
		return s.file.Close()
	}
	return nil
}
