package main

import (
	"loov.dev/lensm/internal/objfile"
)

type Executable struct {
	Objfile *objfile.File
	Disasm  *objfile.Disasm
	Syms    []Symbol
}

func (exe *Executable) Close() error {
	return exe.Objfile.Close()
}

type Symbol struct {
	objfile.Sym
}

func ParseExe(exename string) (*Executable, error) {
	f, err := objfile.Open(exename)
	if err != nil {
		return nil, err
	}

	dis, err := f.Disasm()
	if err != nil {
		f.Close()
		return nil, err
	}

	exe := &Executable{
		Objfile: f,
		Disasm:  dis,
	}
	for _, sym := range dis.Syms() {
		if sym.Code != 'T' && sym.Code != 't' || sym.Addr < dis.TextStart() {
			continue
		}
		exe.Syms = append(exe.Syms, Symbol{Sym: sym})
	}
	return exe, nil
}
