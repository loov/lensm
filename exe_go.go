package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"loov.dev/lensm/internal/go/objfile"
)

var _ Obj = (*GoObj)(nil)
var _ Symbol = (*GoSymbol)(nil)

// GoObj contains information about the object file.
type GoObj struct {
	objfile  *objfile.File
	disasm   *objfile.Disasm
	symbols  []*GoSymbol
	symbols2 []Symbol

	cache map[*GoSymbol]*Code
}

func (exe *GoObj) Symbols() []Symbol { return exe.symbols2 }

// GoSymbol contains information about the executable.
type GoSymbol struct {
	obj *GoObj
	objfile.Sym

	sortName string
}

func (sym *GoSymbol) Name() string { return sym.Sym.Name }

func (exe *GoObj) Close() error {
	return exe.objfile.Close()
}

func LoadExe(path string) (*GoObj, error) {
	f, err := objfile.Open(path)
	if err != nil {
		return nil, err
	}

	dis, err := f.Disasm()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	exe := &GoObj{
		objfile: f,
		disasm:  dis,
		cache:   make(map[*GoSymbol]*Code),
	}

	for _, sym := range dis.Syms() {
		if sym.Code != 'T' && sym.Code != 't' || sym.Addr < dis.TextStart() {
			continue
		}
		sym := &GoSymbol{
			obj:      exe,
			Sym:      sym,
			sortName: sortingName(sym.Name),
		}
		exe.symbols = append(exe.symbols, sym)
	}

	sort.SliceStable(exe.symbols, func(i, k int) bool {
		return exe.symbols[i].sortName < exe.symbols[k].sortName
	})
	for _, sym := range exe.symbols {
		exe.symbols2 = append(exe.symbols2, sym)
	}

	return exe, nil
}

func (sym *GoSymbol) Load(opts Options) *Code {
	return sym.obj.LoadSymbol(sym, opts)
}

func (exe *GoObj) LoadSymbol(sym *GoSymbol, opts Options) *Code {
	code, ok := exe.cache[sym]
	if !ok {
		var err error
		code, err = Disassemble(sym.obj.disasm, sym, opts)
		exe.cache[sym] = code
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
	}

	return code
}

var rxCodeDelimiter = regexp.MustCompile(`[ *().]+`)

func sortingName(sym string) string {
	sym = strings.ToLower(sym)
	return rxCodeDelimiter.ReplaceAllString(sym, " ")
}
