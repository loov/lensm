package main

import (
	"sort"

	"loov.dev/lensm/internal/objfile"
)

// Exe contains information about the object file.
type Exe struct {
	Objfile *objfile.File
	Disasm  *objfile.Disasm
	Symbols []*Symbol
}

// Symbol contains information about the executable.
type Symbol struct {
	Exe *Exe
	objfile.Sym
}

func (exe *Exe) Close() error {
	return exe.Objfile.Close()
}

func LoadExe(path string) (*Exe, error) {
	f, err := objfile.Open(path)
	if err != nil {
		return nil, err
	}

	dis, err := f.Disasm()
	if err != nil {
		f.Close()
		return nil, err
	}

	exe := &Exe{
		Objfile: f,
		Disasm:  dis,
	}
	for _, sym := range dis.Syms() {
		if sym.Code != 'T' && sym.Code != 't' || sym.Addr < dis.TextStart() {
			continue
		}
		exe.Symbols = append(exe.Symbols, &Symbol{
			Exe: exe,
			Sym: sym,
		})
	}

	sort.Slice(exe.Symbols, func(i, k int) bool {
		return exe.Symbols[i].Name < exe.Symbols[k].Name
	})

	return exe, nil
}
