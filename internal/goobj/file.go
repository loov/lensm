package goobj

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/go/objfile"
)

var _ disasm.File = (*File)(nil)
var _ disasm.Func = (*Function)(nil)

// File contains information about the object file.
type File struct {
	objfile *objfile.File
	disasm  *objfile.Disasm
	funcs   []disasm.Func

	cache map[*Function]*disasm.Code
}

func (file *File) Funcs() []disasm.Func { return file.funcs }

// Function contains information about the executable.
type Function struct {
	obj *File
	sym objfile.Sym

	sortName string
}

func (fn *Function) Name() string { return fn.sym.Name }

func (file *File) Close() error {
	return file.objfile.Close()
}

func Load(path string) (*File, error) {
	f, err := objfile.Open(path)
	if err != nil {
		return nil, err
	}

	dis, err := f.Disasm()
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	file := &File{
		objfile: f,
		disasm:  dis,
		cache:   make(map[*Function]*disasm.Code),
	}

	for _, sym := range dis.Syms() {
		if sym.Code != 'T' && sym.Code != 't' || sym.Addr < dis.TextStart() {
			continue
		}
		sym := &Function{
			obj:      file,
			sym:      sym,
			sortName: sortingName(sym.Name),
		}
		file.funcs = append(file.funcs, sym)
	}

	sort.SliceStable(file.funcs, func(i, k int) bool {
		return sortingName(file.funcs[i].Name()) < sortingName(file.funcs[k].Name())
	})

	return file, nil
}

func (fn *Function) Load(opts disasm.Options) *disasm.Code {
	return fn.obj.LoadCode(fn, opts)
}

func (file *File) LoadCode(fn *Function, opts disasm.Options) *disasm.Code {
	code, ok := file.cache[fn]
	if !ok {
		var err error
		code, err = Disassemble(fn.obj.disasm, fn, opts)
		file.cache[fn] = code
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
