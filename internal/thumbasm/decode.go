// Package thumbasm disassembles T32 (Thumb and Thumb-2) machine code.
//
// The encoding table is generated from ARM's AArch32 ISA XML (see ./gen);
// each encoding carries its decode pseudocode and assembler template, and
// the decoder interprets both, so the output follows the architecture
// manual rather than a hand-maintained operand table.
package thumbasm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Inst is one decoded instruction.
type Inst struct {
	Len int // 2 or 4 bytes
	// Mnemonic is the base mnemonic from the reference, e.g. "ADD" for
	// "adds.w"; useful for looking up documentation.
	Mnemonic string
	// Text is the UAL assembly, e.g. "add r0, sp, #0x10".
	Text string
	// Target is the absolute address the instruction refers to, when it
	// has one: the destination of a branch or call, or the literal an
	// ADR/LDR/PLD addresses.
	Target    uint64
	HasTarget bool
	// Branch reports that Target is control flow (B, Bcc, CBZ, CBNZ, BL,
	// BLX) rather than a literal; Call that it is a call (BL, BLX).
	Branch, Call bool
}

var (
	errShort   = errors.New("truncated instruction")
	errUnknown = errors.New("unknown instruction")
)

// Decoder decodes a linear stream of instructions. It is stateful: the
// IT instruction makes the following up-to-four instructions
// conditional, and their mnemonics depend on that state. Use one Decoder
// per straight-line code region.
type Decoder struct {
	it []uint8 // pending conditions from an IT block, current first
}

var prepare sync.Once

// Decode decodes the instruction at the start of code, which lives at
// address pc. Lengths are 2 or 4; on error the caller should skip 2.
func (d *Decoder) Decode(code []byte, pc uint64) (Inst, error) {
	prepare.Do(compileTables)
	if len(code) < 2 {
		return Inst{}, errShort
	}
	hw1 := uint32(binary.LittleEndian.Uint16(code))
	width := 16
	x := hw1
	if hw1>>11 >= 0b11101 {
		if len(code) < 4 {
			return Inst{}, errShort
		}
		width = 32
		x = hw1<<16 | uint32(binary.LittleEndian.Uint16(code[2:]))
	}

	ev := &env{vars: map[string]value{}}
	var cond uint8 = 14
	if len(d.it) > 0 {
		ev.inIT = true
		ev.lastInIT = len(d.it) == 1
		cond = d.it[0]
	}

	for i := range encodings {
		e := &encodings[i]
		if e.width != width || x&e.mask != e.value {
			continue
		}
		clear(ev.vars)
		for _, f := range e.fields {
			ev.set(f.name, mkBits(uint64(x>>uint(f.lo)), f.n))
		}
		if e.condExpr != nil {
			v, err := ev.eval(e.condExpr)
			if err != nil || !v.truthy() {
				continue
			}
		}
		if e.prog != nil {
			if err := ev.run(e.prog); err != nil {
				continue
			}
		}
		if e.aliasExpr != nil {
			v, err := ev.eval(e.aliasExpr)
			if err != nil || !v.truthy() {
				continue
			}
		}
		f := &formatter{enc: e, env: ev, raw: x, pc: pc, width: width, cond: cond}
		text, err := f.format()
		if err != nil {
			continue
		}
		inst := Inst{
			Len:       width / 8,
			Mnemonic:  e.mnemonic,
			Text:      text,
			Target:    f.target,
			HasTarget: f.hasTarget,
			Branch:    f.hasTarget && (e.mnemonic[0] == 'B' || strings.HasPrefix(e.mnemonic, "CB")),
			Call:      e.mnemonic == "BL" || e.mnemonic == "BLX",
		}
		d.advance(e, ev)
		return inst, nil
	}
	d.advance(nil, ev)
	return Inst{}, errUnknown
}

// advance updates the IT state after decoding one instruction.
func (d *Decoder) advance(e *encoding, ev *env) {
	if e != nil && e.mnemonic == "IT" {
		firstcond, _ := ev.get("firstcond")
		mask, _ := ev.get("mask")
		d.it = itConditions(uint8(firstcond.uint()), uint8(mask.uint()))
		return
	}
	if len(d.it) > 0 {
		d.it = d.it[1:]
	}
}

// itConditions expands IT's firstcond and mask into the condition of
// each instruction in the block.
func itConditions(firstcond, mask uint8) []uint8 {
	conds := []uint8{firstcond}
	lowest := 0
	for lowest < 4 && mask>>uint(lowest)&1 == 0 {
		lowest++
	}
	for i := 3; i > lowest; i-- {
		if mask>>uint(i)&1 == firstcond&1 {
			conds = append(conds, firstcond)
		} else {
			conds = append(conds, firstcond^1)
		}
	}
	return conds
}

// itSuffix returns the T/E letters for an IT instruction.
func itSuffix(firstcond, mask uint8) string {
	var b strings.Builder
	for _, c := range itConditions(firstcond, mask)[1:] {
		if c == firstcond {
			b.WriteByte('t')
		} else {
			b.WriteByte('e')
		}
	}
	return b.String()
}

// compileTables parses the pseudocode and expressions of every encoding
// once, on first use.
func compileTables() {
	for i := range encodings {
		e := &encodings[i]
		var err error
		if e.cond != "" {
			if e.condExpr, err = parseCond(e.cond); err != nil {
				panic(fmt.Sprintf("thumbasm: %s cond: %v", e.name, err))
			}
		}
		if e.decode != "" {
			if e.prog, err = parseProgram(e.decode); err != nil {
				panic(fmt.Sprintf("thumbasm: %s decode: %v", e.name, err))
			}
		}
		if e.alias != "" && e.alias != "Unconditionally" {
			if e.aliasExpr, err = parseExpr(e.alias); err != nil {
				panic(fmt.Sprintf("thumbasm: %s alias: %v", e.name, err))
			}
		}
		e.tableExpr = map[string]*node{}
		for sym, t := range e.tables {
			if t.encodedin == "" {
				continue
			}
			n, err := parseExpr(t.encodedin)
			if err != nil {
				panic(fmt.Sprintf("thumbasm: %s table %s: %v", e.name, sym, err))
			}
			e.tableExpr[sym] = n
		}
		e.encExpr = map[string]*node{}
		for sym, src := range e.enc {
			if src == "" {
				continue
			}
			n, err := parseExpr(src)
			if err != nil {
				panic(fmt.Sprintf("thumbasm: %s %s: %v", e.name, sym, err))
			}
			e.encExpr[sym] = n
		}
	}
}
