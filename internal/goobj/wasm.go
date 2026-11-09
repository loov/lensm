package goobj

import (
	"strconv"
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
	// The indent stops at maxIndent: past that it no longer helps reading,
	// and thousands of nested blocks would push the text off screen.
	const maxIndent = 32
	level := 0
	for i, in := range decoded {
		if in.Op == "end" || in.Op == "else" {
			level = max(level-1, 0)
		}
		inst := disasm.Inst{PC: uint64(i), Text: in.Text, Indent: min(level, maxIndent)}
		switch in.Op {
		// watgo prints try_table's catch clauses inline and has no
		// legacy try, so try_table is the only other opener.
		case "block", "loop", "if", "try_table", "else":
			level++
		}
		inst.File, inst.Line = bin.PCToLine(in.Addr)
		if code.File == "" {
			// The function's own file: the prologue can precede the first
			// line-table row, so take it from the first instruction that
			// has one rather than from the entry.
			code.File = inst.File
		}
		inst.Call = wasmCall(in)
		refs.Add(inst.File, inst.Line, i)
		code.Insts = append(code.Insts, inst)
	}
	code.Source = source.Load(fn.file.files, refs, code.File, opts.Context)
	return code, nil
}

// wasmCall names the callee of a call. objfile names it in Text from
// the calling module. Lookup can't stand in: in a component it
// resolves the index in the first module, whichever module the call
// is in. An unnamed callee stays a bare index and gets no link.
func wasmCall(in objfile.Inst) string {
	if !in.Call {
		return ""
	}
	name, ok := strings.CutPrefix(in.Text, "call ")
	if _, err := strconv.ParseUint(name, 10, 64); ok && err != nil {
		return name
	}
	return ""
}
