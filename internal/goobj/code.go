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
	sym.file.attachNotes(code.Source)

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

// farJump is the length in rows above which a jump is drawn as two
// stubs. A longer line rarely shows both its ends, and each one takes
// a column for its whole length: one function had 342 columns.
const farJump = 40

// layoutJumps sets each jump's RefOffset and gives it a column, so jumps
// whose spans overlap get separate lines. Near jumps to one target share
// a column and join into one arrow. Far jumps get a stub at each end,
// and the far jumps to one target share the stub there.
func layoutJumps(code *disasm.Code, pcToIndex map[uint64]int) {
	// A span covers rows in thirds: a jump leaves its row two thirds
	// down and arrives one third down.
	type span struct {
		min, max int
		insts    []*disasm.Inst
		in       bool // the incoming stub of far jumps
	}
	var spans, stubs []*span
	near := map[int]*span{}
	in := map[int]*span{}
	grow := func(spans map[int]*span, target, lo, hi int, ix *disasm.Inst, isIn bool) *span {
		s, ok := spans[target]
		if !ok {
			s = &span{min: lo, max: hi, in: isIn}
			spans[target] = s
		}
		s.min, s.max = min(s.min, lo), max(s.max, hi)
		if ix != nil {
			s.insts = append(s.insts, ix)
		}
		return s
	}
	for i := range code.Insts {
		ix := &code.Insts[i]
		ix.RefFar = false
		if ix.RefPC == 0 {
			continue
		}
		target, ok := pcToIndex[ix.RefPC]
		if !ok {
			continue
		}
		ix.RefOffset = target - i
		from, to := 3*i+2, 3*target+1
		if abs(ix.RefOffset) <= farJump {
			grow(near, target, min(from, to), max(from, to), ix, false)
			continue
		}
		ix.RefFar = true
		if ix.RefOffset > 0 {
			stubs = append(stubs, &span{min: from, max: from + 2, insts: []*disasm.Inst{ix}})
			grow(in, target, to-2, to, &code.Insts[target], true)
		} else {
			stubs = append(stubs, &span{min: from - 2, max: from, insts: []*disasm.Inst{ix}})
			grow(in, target, to, to+2, &code.Insts[target], true)
		}
	}
	for _, s := range near {
		spans = append(spans, s)
	}
	for _, s := range in {
		stubs = append(stubs, s)
	}
	byStart := func(a, b *span) int {
		return cmp.Or(cmp.Compare(a.min, b.min), cmp.Compare(b.max, a.max))
	}
	slices.SortFunc(spans, byStart)
	slices.SortFunc(stubs, byStart)

	// Taking spans by their start and the first free column gives the
	// fewest columns. Any order of the columns works after that, so the
	// ones with the shortest spans go next to the code.
	type column struct {
		end, longest int
		spans        []*span
	}
	var columns []*column
	for _, s := range spans {
		k := slices.IndexFunc(columns, func(c *column) bool { return c.end < s.min })
		if k < 0 {
			k = len(columns)
			columns = append(columns, &column{})
		}
		c := columns[k]
		c.end = s.max
		c.longest = max(c.longest, s.max-s.min)
		c.spans = append(c.spans, s)
	}
	slices.SortStableFunc(columns, func(a, b *column) int { return cmp.Compare(a.longest, b.longest) })

	// Stubs go in the innermost column free at their rows, so their
	// line out of the code stays short. One that fits nowhere gets a
	// new column next to the code.
	for _, s := range stubs {
		k := slices.IndexFunc(columns, func(c *column) bool {
			return !slices.ContainsFunc(c.spans, func(o *span) bool { return o.min <= s.max && s.min <= o.max })
		})
		if k < 0 {
			k = 0
			columns = slices.Insert(columns, 0, &column{})
		}
		columns[k].spans = append(columns[k].spans, s)
	}

	for i := range code.Insts {
		code.Insts[i].RefStack, code.Insts[i].InStack = 0, 0
	}
	for k, c := range columns {
		for _, s := range c.spans {
			for _, ix := range s.insts {
				if s.in {
					ix.InStack = k + 1
				} else {
					ix.RefStack = k + 1
				}
			}
		}
	}
	// Without jumps the code keeps the gap of one column.
	code.MaxJump = max(len(columns), 1)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
