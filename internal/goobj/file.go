// Package goobj loads executables, object files and wasm modules as
// disasm.Files. Parsing and decoding come from objfile; this package
// lays out jumps, resolves calls and attaches the source.
package goobj

import (
	"fmt"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"sync"

	"github.com/loov/disasm/objfile"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/source"
)

var _ disasm.File = (*File)(nil)
var _ disasm.Func = (*Func)(nil)

// File contains information about the object file.
type File struct {
	bin   *objfile.Binary
	funcs []disasm.Func

	// mu guards cache and files and serializes access to bin.
	mu    sync.Mutex
	cache map[cacheKey]cacheEntry
	files source.Files
}

// maxCached bounds the code cache, which holds source lines as well.
const maxCached = 256

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
	bin, err := open(path)
	if err != nil {
		return nil, err
	}

	file := &File{
		bin:   bin,
		cache: make(map[cacheKey]cacheEntry),
		files: make(source.Files),
	}

	// Compute each sort key once; in the comparator the regexp ran
	// 2 million times for a binary with 66k functions.
	type keyed struct {
		key string
		fn  *Func
	}
	sorted := make([]keyed, len(bin.Funcs))
	for i := range bin.Funcs {
		fn := &Func{file: file, fn: &bin.Funcs[i]}
		sorted[i] = keyed{sortingName(fn.Name()), fn}
	}
	slices.SortStableFunc(sorted, func(a, b keyed) int { return strings.Compare(a.key, b.key) })
	file.funcs = make([]disasm.Func, len(sorted))
	for i, k := range sorted {
		file.funcs[i] = k.fn
	}

	return file, nil
}

// open parses path, turning a fault from the file shrinking mid-parse
// into an error, see File.disassemble.
func open(path string) (bin *objfile.Binary, err error) {
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	defer func() {
		if r := recover(); r != nil {
			bin, err = nil, fmt.Errorf("reading %s: %v", path, r)
		}
	}()
	return objfile.Open(path)
}

func (fn *Func) Load(opts disasm.Options) (*disasm.Code, error) {
	file := fn.file
	file.mu.Lock()
	defer file.mu.Unlock()
	key := cacheKey{fn: fn, context: opts.Context}
	entry, ok := file.cache[key]
	if !ok {
		entry.code, entry.err = file.disassemble(fn, opts)
		// Clear rather than evict one by one: MCP callers can ask for
		// every function at every context, and the cache only has to
		// carry the GUI's back-and-forth browsing. Use an LRU if
		// clearing ever shows up as reloads.
		if len(file.cache) >= maxCached {
			clear(file.cache)
		}
		file.cache[key] = entry
	}
	return entry.code, entry.err
}

// disassemble turns a fault into an error. The binary is memory mapped,
// so truncating or rewriting it in place (cp onto it, a rebuild in
// -watch mode) makes reads fault with SIGBUS, which would otherwise kill
// the process.
func (file *File) disassemble(fn *Func, opts disasm.Options) (code *disasm.Code, err error) {
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	defer func() {
		if r := recover(); r != nil {
			code, err = nil, fmt.Errorf("disassembling %s: %v", fn.Name(), r)
		}
	}()
	if file.bin.Arch == "wasm" {
		return disassembleWasm(file.bin, fn, opts)
	}
	return disassemble(file.bin, fn, opts)
}

var rxCodeDelimiter = regexp.MustCompile(`[ *().]+`)

func sortingName(sym string) string {
	sym = strings.ToLower(sym)
	return rxCodeDelimiter.ReplaceAllString(sym, " ")
}
