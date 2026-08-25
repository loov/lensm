// Package wasmobj loads WebAssembly modules as disasm.Files. Loading,
// names, calls and source positions come from objfile; this package
// renders the instructions with their block nesting for the viewer.
package wasmobj

import (
	"sort"
	"strings"
	"sync"

	"github.com/loov/disasm/objfile"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/source"
)

var _ disasm.File = (*File)(nil)
var _ disasm.Func = (*Func)(nil)

// File is a loaded wasm binary: one core module, or every core module
// nested in a component.
type File struct {
	bin   *objfile.Binary
	funcs []disasm.Func
	// mu keeps Close from unmapping the file under a Load in flight.
	mu sync.Mutex
}

func (file *File) Funcs() []disasm.Func { return file.funcs }

func (file *File) Close() error {
	file.mu.Lock()
	defer file.mu.Unlock()
	return file.bin.Close()
}

// Func is one module-defined function.
type Func struct {
	file *File
	fn   *objfile.Func
}

func (fn *Func) Name() string { return fn.fn.Name }

func Load(path string) (*File, error) {
	bin, err := objfile.Open(path)
	if err != nil {
		return nil, err
	}
	file := &File{bin: bin}
	for i := range bin.Funcs {
		file.funcs = append(file.funcs, &Func{file: file, fn: &bin.Funcs[i]})
	}
	sort.SliceStable(file.funcs, func(i, k int) bool {
		return strings.ToLower(file.funcs[i].Name()) < strings.ToLower(file.funcs[k].Name())
	})
	return file, nil
}

func (fn *Func) Load(opts disasm.Options) (*disasm.Code, error) {
	fn.file.mu.Lock()
	defer fn.file.mu.Unlock()
	bin := fn.file.bin
	decoded, err := bin.Disassemble(fn.fn)
	if err != nil {
		return nil, err
	}

	code := &disasm.Code{Name: fn.fn.Name, Arch: bin.Arch}
	refs := source.Refs{}
	// Block nesting is kept as one space per level: Go's function
	// prologue opens one block per resume point, so two would run wide.
	level := 0
	for i, in := range decoded {
		if in.Op == "end" || in.Op == "else" {
			level = max(level-1, 0)
		}
		inst := disasm.Inst{PC: uint64(i), Text: strings.Repeat(" ", level) + in.Text}
		switch in.Op {
		case "block", "loop", "if", "else":
			level++
		}
		inst.File, inst.Line = bin.PCToLine(in.Addr)
		if code.File == "" {
			// The function's own file: the prologue can precede the first
			// line-table row, so take it from the first instruction that
			// has one rather than from the entry.
			code.File = inst.File
		}
		if in.Call {
			if name, base := bin.Lookup(in.Ref); name != "" && base == in.Ref {
				inst.Call = name
			}
		}
		refs.Add(inst.File, inst.Line, i)
		code.Insts = append(code.Insts, inst)
	}
	code.Source = source.Load(refs, code.File, opts.Context)
	return code, nil
}
