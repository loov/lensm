// Package avrasm disassembles AVR machine code (ATmega, ATtiny, ...), in
// the syntax of avr-objdump.
package avrasm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Inst is one decoded instruction.
type Inst struct {
	Len      int // 2 or 4 bytes
	Mnemonic string
	Text     string
	// Target is the byte address of a branch, jump or call.
	Target    uint64
	HasTarget bool
	Call      bool
}

var (
	errShort   = errors.New("truncated instruction")
	errUnknown = errors.New("unknown instruction")
)

// An encoding is a 16-bit pattern of 0/1 and field letters, plus the
// operands to print. Letters: d/r registers, K immediate, k address or
// offset, A I/O address, b bit, s status bit, q displacement.
type encoding struct {
	mnemonic string
	pattern  string
	operands string // comma-separated operand codes, see (*encoding).operand
	long     bool   // a 16-bit address word follows
}

var encodings = []encoding{
	{"nop", "0000000000000000", "", false},
	{"movw", "00000001ddddrrrr", "dw,rw", false},
	{"muls", "00000010ddddrrrr", "d4,r4", false},
	{"mulsu", "000000110ddd0rrr", "d3,r3", false},
	{"fmul", "000000110ddd1rrr", "d3,r3", false},
	{"fmuls", "000000111ddd0rrr", "d3,r3", false},
	{"fmulsu", "000000111ddd1rrr", "d3,r3", false},
	{"cpc", "000001rdddddrrrr", "d5,r5", false},
	{"sbc", "000010rdddddrrrr", "d5,r5", false},
	{"add", "000011rdddddrrrr", "d5,r5", false},
	{"cpse", "000100rdddddrrrr", "d5,r5", false},
	{"cp", "000101rdddddrrrr", "d5,r5", false},
	{"sub", "000110rdddddrrrr", "d5,r5", false},
	{"adc", "000111rdddddrrrr", "d5,r5", false},
	{"and", "001000rdddddrrrr", "d5,r5", false},
	{"eor", "001001rdddddrrrr", "d5,r5", false},
	{"or", "001010rdddddrrrr", "d5,r5", false},
	{"mov", "001011rdddddrrrr", "d5,r5", false},
	{"cpi", "0011KKKKddddKKKK", "d4,K8", false},
	{"sbci", "0100KKKKddddKKKK", "d4,K8", false},
	{"subi", "0101KKKKddddKKKK", "d4,K8", false},
	{"ori", "0110KKKKddddKKKK", "d4,K8", false},
	{"andi", "0111KKKKddddKKKK", "d4,K8", false},
	{"ldd", "10q0qq0ddddd1qqq", "d5,Y+q", false},
	{"ldd", "10q0qq0ddddd0qqq", "d5,Z+q", false},
	{"std", "10q0qq1rrrrr1qqq", "Y+q,r5", false},
	{"std", "10q0qq1rrrrr0qqq", "Z+q,r5", false},
	{"lds", "1001000ddddd0000", "d5,k16", true},
	{"ld", "1001000ddddd0001", "d5,Z+", false},
	{"ld", "1001000ddddd0010", "d5,-Z", false},
	{"lpm", "1001000ddddd0100", "d5,Z", false},
	{"lpm", "1001000ddddd0101", "d5,Z+", false},
	{"elpm", "1001000ddddd0110", "d5,Z", false},
	{"elpm", "1001000ddddd0111", "d5,Z+", false},
	{"ld", "1001000ddddd1001", "d5,Y+", false},
	{"ld", "1001000ddddd1010", "d5,-Y", false},
	{"ld", "1001000ddddd1100", "d5,X", false},
	{"ld", "1001000ddddd1101", "d5,X+", false},
	{"ld", "1001000ddddd1110", "d5,-X", false},
	{"pop", "1001000ddddd1111", "d5", false},
	{"sts", "1001001rrrrr0000", "k16,r5", true},
	{"st", "1001001rrrrr0001", "Z+,r5", false},
	{"st", "1001001rrrrr0010", "-Z,r5", false},
	{"xch", "1001001rrrrr0100", "Z,r5", false},
	{"las", "1001001rrrrr0101", "Z,r5", false},
	{"lac", "1001001rrrrr0110", "Z,r5", false},
	{"lat", "1001001rrrrr0111", "Z,r5", false},
	{"st", "1001001rrrrr1001", "Y+,r5", false},
	{"st", "1001001rrrrr1010", "-Y,r5", false},
	{"st", "1001001rrrrr1100", "X,r5", false},
	{"st", "1001001rrrrr1101", "X+,r5", false},
	{"st", "1001001rrrrr1110", "-X,r5", false},
	{"push", "1001001rrrrr1111", "r5", false},
	{"com", "1001010ddddd0000", "d5", false},
	{"neg", "1001010ddddd0001", "d5", false},
	{"swap", "1001010ddddd0010", "d5", false},
	{"inc", "1001010ddddd0011", "d5", false},
	{"asr", "1001010ddddd0101", "d5", false},
	{"lsr", "1001010ddddd0110", "d5", false},
	{"ror", "1001010ddddd0111", "d5", false},
	{"dec", "1001010ddddd1010", "d5", false},
	{"sec", "1001010000001000", "", false},
	{"sez", "1001010000011000", "", false},
	{"sen", "1001010000101000", "", false},
	{"sev", "1001010000111000", "", false},
	{"ses", "1001010001001000", "", false},
	{"seh", "1001010001011000", "", false},
	{"set", "1001010001101000", "", false},
	{"sei", "1001010001111000", "", false},
	{"clc", "1001010010001000", "", false},
	{"clz", "1001010010011000", "", false},
	{"cln", "1001010010101000", "", false},
	{"clv", "1001010010111000", "", false},
	{"cls", "1001010011001000", "", false},
	{"clh", "1001010011011000", "", false},
	{"clt", "1001010011101000", "", false},
	{"cli", "1001010011111000", "", false},
	{"ijmp", "1001010000001001", "", false},
	{"eijmp", "1001010000011001", "", false},
	{"ret", "1001010100001000", "", false},
	{"icall", "1001010100001001", "", false},
	{"reti", "1001010100011000", "", false},
	{"eicall", "1001010100011001", "", false},
	{"sleep", "1001010110001000", "", false},
	{"break", "1001010110011000", "", false},
	{"wdr", "1001010110101000", "", false},
	{"lpm", "1001010111001000", "", false},
	{"elpm", "1001010111011000", "", false},
	{"spm", "1001010111101000", "", false},
	{"spm", "1001010111111000", "Z+", false},
	{"jmp", "1001010kkkkk110k", "k22", true},
	{"call", "1001010kkkkk111k", "k22", true},
	{"des", "10010100KKKK1011", "K4", false},
	{"adiw", "10010110KKddKKKK", "dp,K6", false},
	{"sbiw", "10010111KKddKKKK", "dp,K6", false},
	{"cbi", "10011000AAAAAbbb", "A5,b", false},
	{"sbic", "10011001AAAAAbbb", "A5,b", false},
	{"sbi", "10011010AAAAAbbb", "A5,b", false},
	{"sbis", "10011011AAAAAbbb", "A5,b", false},
	{"mul", "100111rdddddrrrr", "d5,r5", false},
	{"in", "10110AAdddddAAAA", "d5,A6", false},
	{"out", "10111AArrrrrAAAA", "A6,r5", false},
	{"rjmp", "1100kkkkkkkkkkkk", "k12", false},
	{"rcall", "1101kkkkkkkkkkkk", "k12", false},
	{"ldi", "1110KKKKddddKKKK", "d4,K8", false},
	{"brcs", "111100kkkkkkk000", "k7", false},
	{"breq", "111100kkkkkkk001", "k7", false},
	{"brmi", "111100kkkkkkk010", "k7", false},
	{"brvs", "111100kkkkkkk011", "k7", false},
	{"brlt", "111100kkkkkkk100", "k7", false},
	{"brhs", "111100kkkkkkk101", "k7", false},
	{"brts", "111100kkkkkkk110", "k7", false},
	{"brie", "111100kkkkkkk111", "k7", false},
	{"brcc", "111101kkkkkkk000", "k7", false},
	{"brne", "111101kkkkkkk001", "k7", false},
	{"brpl", "111101kkkkkkk010", "k7", false},
	{"brvc", "111101kkkkkkk011", "k7", false},
	{"brge", "111101kkkkkkk100", "k7", false},
	{"brhc", "111101kkkkkkk101", "k7", false},
	{"brtc", "111101kkkkkkk110", "k7", false},
	{"brid", "111101kkkkkkk111", "k7", false},
	{"bld", "1111100ddddd0bbb", "d5,b", false},
	{"bst", "1111101ddddd0bbb", "d5,b", false},
	{"sbrc", "1111110rrrrr0bbb", "r5,b", false},
	{"sbrs", "1111111rrrrr0bbb", "r5,b", false},
}

type compiled struct {
	mask, value uint16
	fields      map[byte]fieldBits // letter → bit positions, MSB first
}

type fieldBits []uint // bit positions, most significant first

var (
	compileOnce sync.Once
	table       []compiled
)

func compile() {
	table = make([]compiled, len(encodings))
	for i, e := range encodings {
		c := compiled{fields: map[byte]fieldBits{}}
		for j := 0; j < 16; j++ {
			bit := uint(15 - j)
			switch ch := e.pattern[j]; ch {
			case '0':
				c.mask |= 1 << bit
			case '1':
				c.mask |= 1 << bit
				c.value |= 1 << bit
			default:
				c.fields[ch] = append(c.fields[ch], bit)
			}
		}
		table[i] = c
	}
}

func (c *compiled) field(x uint16, letter byte) uint32 {
	var v uint32
	for _, bit := range c.fields[letter] {
		v = v<<1 | uint32(x>>bit&1)
	}
	return v
}

// Decode decodes the instruction at the start of code, located at byte
// address pc.
func Decode(code []byte, pc uint64) (Inst, error) {
	compileOnce.Do(compile)
	if len(code) < 2 {
		return Inst{}, errShort
	}
	x := binary.LittleEndian.Uint16(code)
	// The table is ordered most-specific first for the few overlapping
	// patterns (e.g. sec inside the com/neg/... row of 1001 010x).
	for i := range encodings {
		e, c := &encodings[i], &table[i]
		if x&c.mask != c.value {
			continue
		}
		inst := Inst{Len: 2, Mnemonic: strings.ToUpper(e.mnemonic)}
		var k16 uint32
		if e.long {
			if len(code) < 4 {
				return Inst{}, errShort
			}
			inst.Len = 4
			k16 = uint32(binary.LittleEndian.Uint16(code[2:]))
		}
		var ops []string
		if e.operands != "" {
			for _, op := range strings.Split(e.operands, ",") {
				ops = append(ops, c.operand(op, x, k16, pc, &inst))
			}
		}
		mnemonic := e.mnemonic
		if (mnemonic == "ldd" || mnemonic == "std") && c.field(x, 'q') == 0 {
			mnemonic = mnemonic[:2] // LDD Rd, Y+0 is LD Rd, Y
		}
		inst.Text = mnemonic
		if len(ops) > 0 {
			inst.Text += " " + strings.Join(ops, ", ")
		}
		inst.Call = e.mnemonic == "call" || e.mnemonic == "rcall"
		return inst, nil
	}
	return Inst{}, errUnknown
}

func (c *compiled) operand(op string, x uint16, k16 uint32, pc uint64, inst *Inst) string {
	reg := func(n uint32) string { return "r" + strconv.FormatUint(uint64(n), 10) }
	signed := func(v uint32, bits uint) int64 {
		if v&(1<<(bits-1)) != 0 {
			return int64(v) - 1<<bits
		}
		return int64(v)
	}
	target := func(addr uint64) string {
		inst.Target, inst.HasTarget = addr, true
		return "0x" + strconv.FormatUint(addr, 16)
	}
	switch op {
	case "d5":
		return reg(c.field(x, 'd'))
	case "r5":
		return reg(c.field(x, 'r'))
	case "d4":
		return reg(16 + c.field(x, 'd'))
	case "r4":
		return reg(16 + c.field(x, 'r'))
	case "d3":
		return reg(16 + c.field(x, 'd'))
	case "r3":
		return reg(16 + c.field(x, 'r'))
	case "dw":
		return reg(2 * c.field(x, 'd'))
	case "rw":
		return reg(2 * c.field(x, 'r'))
	case "dp":
		return reg(24 + 2*c.field(x, 'd'))
	case "K8", "K6":
		return fmt.Sprintf("0x%02x", c.field(x, 'K'))
	case "K4":
		return strconv.FormatUint(uint64(c.field(x, 'K')), 10)
	case "A6", "A5":
		return fmt.Sprintf("0x%02x", c.field(x, 'A'))
	case "b":
		return strconv.FormatUint(uint64(c.field(x, 'b')), 10)
	case "k16":
		return fmt.Sprintf("0x%04x", k16)
	case "k22":
		return target(uint64(c.field(x, 'k')<<16|k16) * 2)
	case "k12":
		return target(uint64(int64(pc) + 2 + 2*signed(c.field(x, 'k'), 12)))
	case "k7":
		return target(uint64(int64(pc) + 2 + 2*signed(c.field(x, 'k'), 7)))
	case "Y+q", "Z+q":
		q := c.field(x, 'q')
		if q == 0 {
			return op[:1]
		}
		return fmt.Sprintf("%s+%d", op[:1], q)
	}
	return op // X, X+, -X, Y, Z, Z+, ...
}
