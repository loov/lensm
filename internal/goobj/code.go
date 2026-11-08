package goobj

import (
	"cmp"
	"slices"
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
		var refPC uint64
		var call string
		if name, base := bin.Lookup(in.Ref); in.Ref != 0 && name != "" && base == in.Ref &&
			(in.Call || !inFunc(sym, in.Ref)) {
			// A call, a tail call or a function address: link to it.
			call = name
		} else if inFunc(sym, in.Ref) && in.Op != "ADRP" && in.Op != "ADR" {
			// A branch inside the function gets an arrow. Ref also holds
			// data addresses, but only ADR and ADRP point into code, and
			// objfile reports ADRP's unmasked pc+imm, often the
			// instruction itself.
			refPC = in.Ref
		}

		var marks disasm.Mark
		switch {
		case stack[i].Regs > 0 && stack[i].Store:
			marks |= disasm.MarkSpill
		case stack[i].Regs > 0:
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

	layoutJumps(code, pcToIndex)
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

	return code, nil
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

// layoutJumps sets each jump's RefOffset and gives it a column, so jumps
// whose spans overlap get separate lines. Jumps that start earlier, and
// the longer one on a tie, take the outer columns.
func layoutJumps(code *disasm.Code, pcToIndex map[uint64]int) {
	type jump struct {
		ix       *disasm.Inst
		min, max uint64
	}
	var jumps []jump
	for i := range code.Insts {
		ix := &code.Insts[i]
		if ix.RefPC == 0 {
			continue
		}
		target, ok := pcToIndex[ix.RefPC]
		if !ok {
			continue
		}
		ix.RefOffset = target - i
		jumps = append(jumps, jump{ix, min(ix.PC, ix.RefPC), max(ix.PC, ix.RefPC)})
	}
	slices.SortFunc(jumps, func(a, b jump) int {
		return cmp.Or(cmp.Compare(a.min, b.min), cmp.Compare(b.max, a.max))
	})

	// columns holds the end of the jump in each column, 0 when free.
	var columns []uint64
	for _, j := range jumps {
		for k, end := range columns {
			if end <= j.min {
				columns[k] = 0
			}
		}
		k := slices.Index(columns, 0)
		if k < 0 {
			k = len(columns)
			code.MaxJump = k
			columns = append(columns, 0)
		}
		columns[k] = j.max
		j.ix.RefStack = k
	}
	// Number the columns from the code outwards.
	for i := range code.Insts {
		ix := &code.Insts[i]
		ix.RefStack = code.MaxJump - ix.RefStack + 1
	}
	code.MaxJump++
}
