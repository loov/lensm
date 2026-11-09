package goobj

import (
	"strconv"
	"strings"

	"github.com/loov/disasm/objfile"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/source"
)

// disassembleWasm renders a wasm function with its block nesting, its
// branches as jumps, and for a core module the statements it spells.
// Loading, names, calls and source positions come from objfile.
func disassembleWasm(file *File, fn *Func, opts disasm.Options) (*disasm.Code, error) {
	bin := file.bin
	decoded, err := bin.Disassemble(fn.fn)
	if err != nil {
		return nil, err
	}

	code := &disasm.Code{Name: fn.fn.Name, Arch: bin.Arch}
	refs := source.Refs{}
	// The indent stops at maxIndent: past that it no longer helps reading,
	// and thousands of nested blocks would push the text off screen.
	const maxIndent = 32
	// A block opened right inside another adds no level: Go wraps a
	// function in a block per resume point, and LLVM nests blocks for a
	// switch, and indenting each would push the code off the column.
	// steps holds each open block's step, 0 or 1.
	level := 0
	var steps []int
	for i, in := range decoded {
		switch {
		case in.Op == "end" && len(steps) > 0:
			level -= steps[len(steps)-1]
			steps = steps[:len(steps)-1]
		case in.Op == "else" && len(steps) > 0:
			level -= steps[len(steps)-1]
		}
		inst := disasm.Inst{PC: uint64(i), Text: in.Text, Indent: min(level, maxIndent)}
		switch {
		case isWasmOpener(in.Op):
			step := 1
			if i > 0 && isWasmOpener(decoded[i-1].Op) && in.Op != "if" {
				step = 0
			}
			steps = append(steps, step)
			level += step
		case in.Op == "else" && len(steps) > 0:
			level += steps[len(steps)-1]
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
	targeted := wasmBranches(code.Insts)
	if info := file.wasmModule(); info != nil {
		var goArgs map[int64]string
		if info.goWasm {
			goArgs = goArgNames(code.File, fn.Name())
		}
		wasmStatements(info, int(fn.fn.Addr), code.Insts, targeted, goArgs)
		code.Statements = true
	}
	code.Source = source.Load(fn.file.files, refs, code.File, opts.Context)
	return code, nil
}

// wasmBranches points each br and br_if at the row it continues from,
// so the view draws it as a jump. A branch names its target by how many
// blocks out it is; a loop's label is its start and a block's or an
// if's is its end. br_table points at its default target.
//
// It returns the rows of the blocks that branches go to.
func wasmBranches(insts []disasm.Inst) map[int]bool {
	targeted := map[int]bool{}
	type label struct {
		at       int   // the opener's row
		loop     bool  // branches go back to at
		branches []int // rows waiting for the end
	}
	// The function body is the outermost block; its end is the last row.
	stack := []*label{{at: -1}}
	for i := range insts {
		op, arg, _ := strings.Cut(insts[i].Text, " ")
		switch {
		case isWasmOpener(op):
			stack = append(stack, &label{at: i, loop: op == "loop"})
		case op == "end":
			if len(stack) > 1 || i == len(insts)-1 {
				l := stack[len(stack)-1]
				for _, b := range l.branches {
					insts[b].RefOffset = i - b
				}
				l.branches = nil
				if len(stack) > 1 {
					stack = stack[:len(stack)-1]
				}
			}
		case op == "br" || op == "br_if" || op == "br_table":
			fields := strings.Fields(arg)
			if len(fields) == 0 {
				continue
			}
			// The jump goes to br_table's default, the last depth.
			for _, f := range fields {
				if d, err := strconv.Atoi(f); err == nil && d < len(stack) && stack[len(stack)-1-d].at >= 0 {
					targeted[stack[len(stack)-1-d].at] = true
				}
			}
			depth, err := strconv.Atoi(fields[len(fields)-1])
			if err != nil || depth >= len(stack) {
				continue
			}
			l := stack[len(stack)-1-depth]
			if l.loop {
				insts[i].RefOffset = l.at - i
			} else {
				l.branches = append(l.branches, i)
			}
		}
	}
	return targeted
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

// isWasmOpener reports whether a wasm instruction opens a block. watgo
// prints try_table's catch clauses inline and has no legacy try, so
// try_table is the only other opener.
func isWasmOpener(op string) bool {
	switch op {
	case "block", "loop", "if", "try_table":
		return true
	}
	return false
}

// wasmOp is the instruction in a row's text.
func wasmOp(text string) string {
	op, _, _ := strings.Cut(text, " ")
	return op
}
