package main

import (
	"fmt"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/goobj"
	"loov.dev/lensm/internal/wasmobj"
)

type LensmSession struct {
	Path     string
	File     disasm.File
	Comments *CommentStore
}

func NewLensmSession(path string, commentsPath string) (*LensmSession, error) {
	return NewLensmSessionWithComments(path, commentsPath, nil)
}

func NewLensmSessionWithComments(path string, commentsPath string, comments *CommentStore) (*LensmSession, error) {
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
	return &LensmSession{
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

func (session *LensmSession) Close() error {
	if session == nil || session.File == nil {
		return nil
	}
	return session.File.Close()
}

func (session *LensmSession) Funcs() []disasm.Func {
	if session == nil || session.File == nil {
		return nil
	}
	return session.File.Funcs()
}

func (session *LensmSession) FindFunc(name string) disasm.Func {
	for _, fn := range session.Funcs() {
		if fn.Name() == name {
			return fn
		}
	}
	return nil
}

func (session *LensmSession) LoadCode(name string, context int) (*disasm.Code, error) {
	fn := session.FindFunc(name)
	if fn == nil {
		return nil, fmt.Errorf("function %q not found", name)
	}
	return fn.Load(disasm.Options{Context: context}), nil
}
