package goobj

import (
	"strings"

	"github.com/loov/disasm/objfile"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/source"
)

// disassembleWasm renders a wasm function with its block nesting.
// Loading, names, calls and source positions come from objfile.
func disassembleWasm(bin *objfile.Binary, fn *Func, opts disasm.Options) (*disasm.Code, error) {
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
