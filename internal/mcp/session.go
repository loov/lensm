package mcp

import (
	"fmt"

	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/goobj"
)

type Session struct {
	Path     string
	File     disasm.File
	Comments *comments.Store
}

func NewSessionWithComments(path string, commentsPath string, store *comments.Store) (*Session, error) {
	file, err := goobj.Load(path)
	if err != nil {
		return nil, err
	}
	if store == nil {
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

func (s *Session) Close() error { return s.File.Close() }

func (s *Session) Funcs() []disasm.Func { return s.File.Funcs() }

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
