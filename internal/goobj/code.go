package goobj

import (
	"strings"

	"github.com/loov/disasm/analysis"
	"github.com/loov/disasm/objfile"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/source"
)

// inFunc reports whether addr lies inside fn.
func inFunc(fn *Func, addr uint64) bool {
	return fn.fn.Addr <= addr && addr-fn.fn.Addr < fn.fn.Size
}

// entryFile is the file a function belongs to: its entry's, or the first
// position inside it that has one. A line table need not have a row at
// the entry itself, and the file decides which source block sorts first.
func entryFile(bin *objfile.Binary, sym *Func, decoded []objfile.Inst) string {
	// What the debug info says the function was written in, which beats
	// guessing from positions: with inlining the first instruction can
	// belong to a callee in another file.
	if file := bin.FuncFile(sym.fn.Addr); file != "" {
		return file
	}
	// Walk the instructions, not every byte of Size: the size comes from
	// the symbol table and a corrupt one can be near 2^64.
	for _, in := range decoded {
		if file, _ := bin.PCToLine(in.Addr); file != "" {
			return file
		}
	}
	return ""
}

// disassemble disassembles a native function.
func disassemble(bin *objfile.Binary, sym *Func, opts disasm.Options) (*disasm.Code, error) {
	needRefPCs := map[uint64]struct{}{}

	decoded, err := bin.Disassemble(sym.fn)
	if err != nil {
		return nil, err
	}
	code := &disasm.Code{
		Name: sym.Name(),
		File: entryFile(bin, sym, decoded),
		Arch: bin.Arch,
	}
	stack := analysis.StackAccesses(bin.Arch, decoded)
	var instructions []disasm.Inst
	for i, in := range decoded {
		pc := in.Addr
		file, line := bin.PCToLine(pc)
		call := callName(bin, sym, in)
		var refPC uint64
		if call == "" && inFunc(sym, in.Ref) && in.Op != "ADRP" && in.Op != "ADR" {
			// A branch inside the function gets an arrow. Ref also holds
			// data addresses, but only ADR and ADRP point into code, and
			// objfile reports ADRP's unmasked pc+imm, often the
			// instruction itself.
			refPC = in.Ref
		}
		marks := instMarks(stack[i], in, call)

		// A jump to itself (for {}) needs no spacer before its target.
		if refPC != 0 && refPC != pc {
			needRefPCs[refPC] = struct{}{}
		}
		instructions = append(instructions, disasm.Inst{
			PC:         pc,
			Text:       in.Text,
			NativeText: in.GNU,
			Mnemonic:   in.Op,
			File:       file,
			Line:       line,
			Call:       call,
			RefPC:      refPC,
			Marks:      marks,
		})
	}

	pcToIndex := map[uint64]int{}
	for _, ix := range instructions {
		if _, ok := needRefPCs[ix.PC]; ok {
			// add empty line
			code.Insts = append(code.Insts, disasm.Inst{})
		}
		pcToIndex[ix.PC] = len(code.Insts)
		code.Insts = append(code.Insts, ix)
	}

	setJumpOffsets(code, pcToIndex)
	markBoundsChecks(code)

	// remove trailing interrupts and padding from funcs
	for len(code.Insts) > 0 &&
		(strings.HasPrefix(code.Insts[len(code.Insts)-1].Text, "INT ") ||
			strings.HasPrefix(code.Insts[len(code.Insts)-1].Text, "BYTE ")) {
		code.Insts = code.Insts[:len(code.Insts)-1]
	}

	// load sources and relate their lines to the instructions
	refs := source.Refs{}
	for i, ix := range code.Insts {
		refs.Add(ix.File, ix.Line, i)
	}
	code.Source = source.Load(sym.file.files, refs, code.File, opts.Context)
	sym.file.attachNotes(code.Source)

	return code, nil
}

// callName names the function in refers to when it calls it, tail
// calls it or takes its address, so that the row links to it.
func callName(bin *objfile.Binary, sym *Func, in objfile.Inst) string {
	if in.Ref == 0 {
		return ""
	}
	if name, base := bin.Lookup(in.Ref); name != "" && base == in.Ref && (in.Call || !inFunc(sym, in.Ref)) {
		return name
	}
	return ""
}

// instMarks are the run time costs of in, which calls call.
func instMarks(stack analysis.StackAccess, in objfile.Inst, call string) disasm.Mark {
	var marks disasm.Mark
	switch {
	case stack.Regs > 0 && stack.Store:
		marks |= disasm.MarkSpill
	case stack.Regs > 0:
		marks |= disasm.MarkReload
	}
	if in.Call && call != "" {
		switch analysis.ClassifyCall(call) {
		case analysis.CallBoundsCheck:
			marks |= disasm.MarkBoundsCheck
		case analysis.CallAlloc:
			marks |= disasm.MarkAlloc
		case analysis.CallWriteBarrier:
			marks |= disasm.MarkWriteBarrier
		}
	}
	return marks
}

// markBoundsChecks marks the branches to a bounds check failure, which
// are the checks themselves. The failure block runs straight into the
// call, so a branch anywhere into it counts.
func markBoundsChecks(code *disasm.Code) {
	for c := range code.Insts {
		if code.Insts[c].Marks&disasm.MarkBoundsCheck == 0 || code.Insts[c].Call == "" {
			continue
		}
		start := c
		for start > 0 {
			prev := &code.Insts[start-1]
			// A spacer, branch or call ends the block.
			if prev.PC == 0 || prev.RefOffset != 0 || prev.Call != "" {
				break
			}
			start--
		}
		for i := range code.Insts {
			ix := &code.Insts[i]
			if target := i + ix.RefOffset; ix.RefOffset != 0 && start <= target && target <= c {
				ix.Marks |= disasm.MarkBoundsCheck
			}
		}
	}
}

// setJumpOffsets sets each jump's RefOffset, the rows to its target,
// and lays the jumps out for a view of disasm.FarJump rows.
func setJumpOffsets(code *disasm.Code, pcToIndex map[uint64]int) {
	for i := range code.Insts {
		ix := &code.Insts[i]
		if ix.RefPC == 0 {
			continue
		}
		if target, ok := pcToIndex[ix.RefPC]; ok {
			ix.RefOffset = target - i
		}
	}
	code.LayoutJumps(disasm.FarJump)
}
