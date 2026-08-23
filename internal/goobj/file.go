package goobj

import (
	"regexp"
	"sort"
	"strings"
	"sync"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/objfile"
)

var _ disasm.File = (*File)(nil)
var _ disasm.Func = (*Func)(nil)

// File contains information about the object file.
type File struct {
	bin   *objfile.Binary
	funcs []disasm.Func

	// mu guards cache.
	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
}

// cacheKey includes the options: MCP callers choose the source context
// per call, and a cache keyed by function alone would silently serve
// whichever context happened to load first.
type cacheKey struct {
	fn      *Func
	context int
}

// cacheEntry also caches failures, so an erroring function isn't
// re-disassembled on every frame.
type cacheEntry struct {
	code *disasm.Code
	err  error
}

func (file *File) Funcs() []disasm.Func { return file.funcs }

// Function contains information about the executable.
type Func struct {
	obj *File
	fn  *objfile.Func
}

func (fn *Func) Name() string { return fn.fn.Name }

func (file *File) Close() error { return nil }

func Load(path string) (*File, error) {
	bin, err := objfile.Open(path)
	if err != nil {
		return nil, err
	}

	file := &File{
		bin:   bin,
		cache: make(map[cacheKey]cacheEntry),
	}

	for _, fn := range bin.Funcs {
		file.funcs = append(file.funcs, &Func{
			obj: file,
			fn:  fn,
		})
	}

	sort.SliceStable(file.funcs, func(i, k int) bool {
		return sortingName(file.funcs[i].Name()) < sortingName(file.funcs[k].Name())
	})

	return file, nil
}

func (fn *Func) Load(opts disasm.Options) (*disasm.Code, error) {
	return fn.obj.LoadCode(fn, opts)
}

func (file *File) LoadCode(fn *Func, opts disasm.Options) (*disasm.Code, error) {
	file.mu.Lock()
	defer file.mu.Unlock()
	key := cacheKey{fn: fn, context: opts.Context}
	entry, ok := file.cache[key]
	if !ok {
		entry.code, entry.err = Disassemble(fn.obj.bin, fn, opts)
		file.cache[key] = entry
	}
	return entry.code, entry.err
}

var rxCodeDelimiter = regexp.MustCompile(`[ *().]+`)

func sortingName(sym string) string {
	sym = strings.ToLower(sym)
	return rxCodeDelimiter.ReplaceAllString(sym, " ")
}
