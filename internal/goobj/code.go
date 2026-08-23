package goobj

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/objfile"
	"loov.dev/lensm/internal/source"
)

var rxRefAbs = regexp.MustCompile(`\s0x[\da-fA-F]+$`)
var rxRefRel = regexp.MustCompile(`\s-?\d+\(PC\)$`)
var rxCallOrJump = regexp.MustCompile(`^(?:CALL|JMP)\s+(.+?)\(SB\)`)

// entryFile is the file a function belongs to: its entry's, or the first
// position inside it that has one. A line table need not have a row at
// the entry itself, and the file decides which source block sorts first.
func entryFile(bin *objfile.Binary, sym *Func) string {
	// What the debug info says the function was written in, which beats
	// guessing from positions: with inlining the first instruction can
	// belong to a callee in another file.
	if file := bin.FuncFile(sym.fn.Addr); file != "" {
		return file
	}
	for pc := sym.fn.Addr; pc < sym.fn.Addr+sym.fn.Size; pc++ {
		if file, _ := bin.PCToLine(pc); file != "" {
			return file
		}
	}
	return ""
}

// Disassemble disassembles the specified symbol.
func Disassemble(bin *objfile.Binary, sym *Func, opts disasm.Options) (*disasm.Code, error) {
	needRefPCs := map[uint64]struct{}{}

	code := &disasm.Code{
		Name: sym.Name(),
		File: entryFile(bin, sym),
		Arch: bin.Arch,
	}
	decoded, err := bin.Disassemble(sym.fn)
	if err != nil {
		return nil, err
	}
	var instructions []disasm.Inst
	for _, in := range decoded {
		pc, text := in.Addr, in.Text
		file, line := bin.PCToLine(pc)
		// TODO: find a better way to calculate the jump target
		var refPC uint64
		var call string
		if match := rxRefAbs.FindString(text); match != "" {
			if target, err := strconv.ParseInt(match[3:], 16, 64); err == nil {
				refPC = uint64(target)
			}
		} else if match := rxRefRel.FindString(text); match != "" {
			// TODO: this calculation seems incorrect
			if target, err := strconv.ParseInt(match[1:len(match)-4], 10, 64); err == nil {
				refPC = uint64(int64(pc) + target*4)
			} else {
				panic(err)
			}
		} else if match := rxCallOrJump.FindStringSubmatch(text); len(match) > 0 {
			call = match[1]
		}

		if refPC != 0 {
			needRefPCs[refPC] = struct{}{}
		}
		instructions = append(instructions, disasm.Inst{
			PC:         pc,
			Text:       text,
			NativeText: in.GNU,
			Mnemonic:   in.Op,
			File:       file,
			Line:       line,
			Call:       call,
			RefPC:      refPC,
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

	type jumpInterval struct {
		index    int
		ix       *disasm.Inst
		min, max uint64
	}

	var jumps []jumpInterval
	for i := range code.Insts {
		ix := &code.Insts[i]
		if ix.RefPC != 0 {
			target, ok := pcToIndex[ix.RefPC]
			if !ok {
				continue
			}
			ix.RefOffset = target - i

			if ix.PC <= ix.RefPC {
				jumps = append(jumps, jumpInterval{
					index: i,
					ix:    ix,
					min:   ix.PC,
					max:   ix.RefPC,
				})
			} else {
				jumps = append(jumps, jumpInterval{
					index: i,
					ix:    ix,
					min:   ix.RefPC,
					max:   ix.PC,
				})
			}
		}
	}

	sort.Slice(jumps, func(i, k int) bool {
		if jumps[i].min == jumps[k].min {
			return jumps[i].max > jumps[k].max
		}
		return jumps[i].min < jumps[k].min
	})

	var stackLayers []uint64
	insertToStack := func(ix *disasm.Inst, max uint64) {
		found := false
		for k, pc := range stackLayers {
			if pc == 0 {
				stackLayers[k] = max
				ix.RefStack = k
				found = true
				break
			}
		}
		if !found {
			code.MaxJump = len(stackLayers)
			ix.RefStack = len(stackLayers)
			stackLayers = append(stackLayers, max)
		}
	}

	for _, jump := range jumps {
		for i, pc := range stackLayers {
			if pc <= jump.min {
				stackLayers[i] = 0
			}
		}
		insertToStack(jump.ix, jump.max)
	}
	for i := range code.Insts {
		ix := &code.Insts[i]
		ix.RefStack = code.MaxJump - ix.RefStack + 1
	}
	code.MaxJump++

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
	code.Source = source.Load(refs, code.File, opts.Context)

	return code, nil
}
