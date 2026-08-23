// Package wasmobj loads WebAssembly modules as disasm.Files. Function
// bodies are rendered as WAT through watgo; there is no source mapping.
package wasmobj

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/eliben/watgo"
	"github.com/eliben/watgo/wasmir"

	"loov.dev/lensm/internal/disasm"
)

var _ disasm.File = (*File)(nil)
var _ disasm.Func = (*Func)(nil)

// File is a loaded wasm module.
type File struct {
	// names maps the function index space (imports first, then defined
	// functions) to display names, for resolving call targets.
	names []string
	funcs []disasm.Func
}

func (file *File) Funcs() []disasm.Func { return file.funcs }
func (file *File) Close() error         { return nil }

// Func is one module-defined function.
type Func struct {
	obj  *File
	name string
	fn   wasmir.Function
}

func (fn *Func) Name() string { return fn.name }

func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	module, err := watgo.DecodeWASM(data)
	if err != nil {
		return nil, fmt.Errorf("%q: %w", path, err)
	}

	file := &File{}
	for _, imp := range module.Imports {
		if imp.Kind == wasmir.ExternalKindFunction {
			file.names = append(file.names, imp.Module+"."+imp.Name)
		}
	}
	for i, fn := range module.Funcs {
		name := fn.Name
		if name == "" {
			name = fmt.Sprintf("func%d", len(file.names))
		}
		file.names = append(file.names, name)
		file.funcs = append(file.funcs, &Func{obj: file, name: name, fn: module.Funcs[i]})
	}
	sort.SliceStable(file.funcs, func(i, k int) bool {
		return strings.ToLower(file.funcs[i].Name()) < strings.ToLower(file.funcs[k].Name())
	})
	return file, nil
}

func (fn *Func) Load(opts disasm.Options) (*disasm.Code, error) {
	// watgo prints whole modules only, so the body goes into a synthetic
	// single-function module under a void signature; the real signature
	// is not needed to render the instructions.
	wat, err := watgo.PrintWAT(&wasmir.Module{
		Types: []wasmir.TypeDef{{Kind: wasmir.TypeDefKindFunc}},
		Funcs: []wasmir.Function{{Locals: fn.fn.Locals, Body: fn.fn.Body}},
	})
	if err != nil {
		return nil, err
	}

	code := &disasm.Code{Name: fn.name, Arch: "wasm"}
	// The body prints one instruction per line between "(func" and its
	// closing ")", in Body order; the final InstrEnd becomes the ")".
	// Block nesting is kept as one space per level: Go's function
	// prologue opens one block per resume point, so two would run wide.
	inBody := false
	for line := range strings.Lines(string(wat)) {
		text := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(text, "(func"):
			inBody = true
			continue
		case !inBody || text == "" || text == ")":
			continue
		}
		i := len(code.Insts)
		if i < len(fn.fn.Body) && fn.fn.Body[i].Kind == wasmir.InstrCall {
			if idx := int(fn.fn.Body[i].FuncIndex); idx < len(fn.obj.names) {
				text = "call " + fn.obj.names[idx]
			}
		}
		level := (len(line) - len(strings.TrimLeft(line, " ")) - 4) / 2
		inst := disasm.Inst{PC: uint64(i), Text: strings.Repeat(" ", max(level, 0)) + text}
		if strings.HasPrefix(text, "call ") {
			inst.Call = strings.TrimPrefix(text, "call ")
		}
		code.Insts = append(code.Insts, inst)
	}
	return code, nil
}
