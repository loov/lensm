package mcp

import (
	"fmt"

	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
)

type Session struct {
	Path     string
	File     disasm.File
	Comments *comments.Store
}

// LoadFile opens a binary for disassembly. The caller injects an
// implementation so sessions stay independent of the object-file formats.
type LoadFile func(path string) (disasm.File, error)

func NewSessionWithComments(load LoadFile, path string, commentsPath string, store *comments.Store) (*Session, error) {
	file, err := load(path)
	if err != nil {
		return nil, err
	}
	if store == nil {
		if commentsPath == "" {
			commentsPath = comments.DefaultPath(path)
		}
		var err error
		store, err = comments.Open(commentsPath, path)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	return &Session{
		Path:     comments.CleanPath(path),
		File:     file,
		Comments: store,
	}, nil
}

func (s *Session) Close() error {
	if s == nil || s.File == nil {
		return nil
	}
	return s.File.Close()
}

func (s *Session) Funcs() []disasm.Func {
	if s == nil || s.File == nil {
		return nil
	}
	return s.File.Funcs()
}

func (s *Session) FindFunc(name string) disasm.Func {
	for _, fn := range s.Funcs() {
		if fn.Name() == name {
			return fn
		}
	}
	return nil
}

func (s *Session) LoadCode(name string, context int) (*disasm.Code, error) {
	fn := s.FindFunc(name)
	if fn == nil {
		return nil, fmt.Errorf("function %q not found", name)
	}
	return fn.Load(disasm.Options{Context: context})
}
