// Package goobj loads executables, object files and wasm modules as
// disasm.Files. Parsing and decoding come from objfile; this package
// lays out jumps, resolves calls and attaches the source.
package goobj

import (
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/loov/disasm/objfile"

	"loov.dev/lensm/internal/disasm"
)

var _ disasm.File = (*File)(nil)
var _ disasm.Func = (*Func)(nil)

// File contains information about the object file.
type File struct {
	bin   *objfile.Binary
	funcs []disasm.Func

	// mu guards cache and serializes access to bin.
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

// cacheEntry also caches failures, so browsing back to an erroring
// function doesn't disassemble it again.
type cacheEntry struct {
	code *disasm.Code
	err  error
}

func (file *File) Funcs() []disasm.Func { return file.funcs }

// Func is one function in the file.
type Func struct {
	file *File
	fn   *objfile.Func
}

func (fn *Func) Name() string { return fn.fn.Name }

// Close releases the binary. It waits for a disassembly in flight, since
// Func.Code aliases the file mapping that Close unmaps.
func (file *File) Close() error {
	file.mu.Lock()
	defer file.mu.Unlock()
	return file.bin.Close()
}

// Load opens the binary at path. objfile detects the format.
func Load(path string) (*File, error) {
	bin, err := objfile.Open(path)
	if err != nil {
		return nil, err
	}

	file := &File{
		bin:   bin,
		cache: make(map[cacheKey]cacheEntry),
	}

	for i := range bin.Funcs {
		file.funcs = append(file.funcs, &Func{
			file: file,
			fn:   &bin.Funcs[i],
		})
	}

	sort.SliceStable(file.funcs, func(i, k int) bool {
		return sortingName(file.funcs[i].Name()) < sortingName(file.funcs[k].Name())
	})

	return file, nil
}

func (fn *Func) Load(opts disasm.Options) (*disasm.Code, error) {
	file := fn.file
	file.mu.Lock()
	defer file.mu.Unlock()
	key := cacheKey{fn: fn, context: opts.Context}
	entry, ok := file.cache[key]
	if !ok {
		if file.bin.Arch == "wasm" {
			entry.code, entry.err = disassembleWasm(file.bin, fn, opts)
		} else {
			entry.code, entry.err = disassemble(file.bin, fn, opts)
		}
		file.cache[key] = entry
	}
	return entry.code, entry.err
}

var rxCodeDelimiter = regexp.MustCompile(`[ *().]+`)

func sortingName(sym string) string {
	sym = strings.ToLower(sym)
	return rxCodeDelimiter.ReplaceAllString(sym, " ")
}
