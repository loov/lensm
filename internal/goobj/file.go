package goobj

import (
	"regexp"
	"sort"
	"strings"
	"sync"

	"loov.dev/lensm/internal/disasm"
	godisasm "loov.dev/lensm/internal/go/src/disasm"
	"loov.dev/lensm/internal/go/src/objfile"
)

var _ disasm.File = (*File)(nil)
var _ disasm.Func = (*Function)(nil)

// File contains information about the object file.
type File struct {
	objfile *objfile.File
	disasm  *godisasm.Disasm
	funcs   []disasm.Func

	// mu guards cache and serializes Disassemble calls: disassembly
	// lazily populates line-table caches inside disasm, which is not
	// safe for concurrent use.
	mu    sync.Mutex
	cache map[*Function]cacheEntry
}

// cacheEntry also caches failures, so an erroring function isn't
// re-disassembled on every frame.
type cacheEntry struct {
	code *disasm.Code
	err  error
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

	dis, err := godisasm.DisasmForFile(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	file := &File{
		objfile: f,
		disasm:  dis,
		cache:   make(map[*Function]cacheEntry),
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

func (fn *Function) Load(opts disasm.Options) (*disasm.Code, error) {
	return fn.obj.LoadCode(fn, opts)
}

func (file *File) LoadCode(fn *Function, opts disasm.Options) (*disasm.Code, error) {
	file.mu.Lock()
	defer file.mu.Unlock()
	entry, ok := file.cache[fn]
	if !ok {
		entry.code, entry.err = Disassemble(fn.obj.disasm, fn, opts)
		file.cache[fn] = entry
	}
	return entry.code, entry.err
}

var rxCodeDelimiter = regexp.MustCompile(`[ *().]+`)

func sortingName(sym string) string {
	sym = strings.ToLower(sym)
	return rxCodeDelimiter.ReplaceAllString(sym, " ")
}
