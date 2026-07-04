package main

import (
	"fmt"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/goobj"
	"loov.dev/lensm/internal/wasmobj"
	"loov.dev/lensm/internal/comments"
)

type Session struct {
	Path     string
	File     disasm.File
	Comments *comments.Store
}

func NewSession(path string, commentsPath string) (*Session, error) {
	return NewSessionWithComments(path, commentsPath, nil)
}

func NewSessionWithComments(path string, commentsPath string, store *comments.Store) (*Session, error) {
	file, err := loadDisasmFile(path)
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

func loadDisasmFile(path string) (disasm.File, error) {
	if workInProgressWASM {
		return wasmobj.Load(path)
	}
	return goobj.Load(path)
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
