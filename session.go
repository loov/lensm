package main

import (
	"fmt"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/goobj"
	"loov.dev/lensm/internal/wasmobj"
)

type Session struct {
	Path     string
	File     disasm.File
	Comments *CommentStore
}

func NewSession(path string, commentsPath string) (*Session, error) {
	return NewSessionWithComments(path, commentsPath, nil)
}

func NewSessionWithComments(path string, commentsPath string, comments *CommentStore) (*Session, error) {
	file, err := loadDisasmFile(path)
	if err != nil {
		return nil, err
	}
	if comments == nil {
		if commentsPath == "" {
			commentsPath = defaultCommentPath(path)
		}
		var err error
		comments, err = NewCommentStore(commentsPath, path)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
	}
	return &Session{
		Path:     cleanPath(path),
		File:     file,
		Comments: comments,
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
