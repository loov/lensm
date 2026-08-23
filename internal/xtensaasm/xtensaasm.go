// Package xtensaasm disassembles Xtensa LX machine code (ESP32, ESP8266),
// in the syntax of xtensa-objdump: the core ISA with the code density,
// windowed register, loop, multiply/divide, boolean and single-precision
// floating-point options.
package xtensaasm

import (
	"errors"
	"strconv"
	"strings"
)

// Inst is one decoded instruction.
type Inst struct {
	Len      int // 2 or 3 bytes
	Mnemonic string
	Text     string
	// Target is the address a branch, jump, call or L32R refers to.
	Target    uint64
	HasTarget bool
	Branch    bool // Target is control flow rather than a literal
	Call      bool
}

var (
	errShort   = errors.New("truncated instruction")
	errUnknown = errors.New("unknown instruction")
)

// encoding selects an instruction by its opcode fields; -1 means any
// value. Narrow encodings use op0 8..13 and have no op1/op2. tpat
// constrains the t nibble bitwise ("0xxx") where a field straddles it.
type encoding struct {
	mn                      string
	op0, op1, op2, r, s, t int8
	tpat                    string
	ops                     string // comma-separated operand codes
}

const any = -1

var encodings = []encoding{
	// op0=0, op1=0 RST0; op2=0 ST0, r selects.
	{"ill", 0, 0, 0, 0, 0, 0, "", ""},
	{"ret", 0, 0, 0, 0, 0, 8, "", ""},
	{"retw", 0, 0, 0, 0, 0, 9, "", ""},
	{"jx", 0, 0, 0, 0, any, 10, "", "as"},
	{"callx0", 0, 0, 0, 0, any, 12, "", "as"},
	{"callx4", 0, 0, 0, 0, any, 13, "", "as"},
	{"callx8", 0, 0, 0, 0, any, 14, "", "as"},
	{"callx12", 0, 0, 0, 0, any, 15, "", "as"},
	{"movsp", 0, 0, 0, 1, any, any, "", "at,as"},
	{"isync", 0, 0, 0, 2, 0, 0, "", ""},
	{"rsync", 0, 0, 0, 2, 0, 1, "", ""},
	{"esync", 0, 0, 0, 2, 0, 2, "", ""},
	{"dsync", 0, 0, 0, 2, 0, 3, "", ""},
	{"excw", 0, 0, 0, 2, 0, 8, "", ""},
	{"memw", 0, 0, 0, 2, 0, 12, "", ""},
	{"extw", 0, 0, 0, 2, 0, 13, "", ""},
	{"nop", 0, 0, 0, 2, 0, 15, "", ""},
	{"rfe", 0, 0, 0, 3, 0, 0, "", ""},
	{"rfue", 0, 0, 0, 3, 1, 0, "", ""},
	{"rfde", 0, 0, 0, 3, 2, 0, "", ""},
	{"rfwo", 0, 0, 0, 3, 4, 0, "", ""},
	{"rfwu", 0, 0, 0, 3, 5, 0, "", ""},
	{"rfi", 0, 0, 0, 3, any, 1, "", "s"},
	{"rfme", 0, 0, 0, 3, 0, 2, "", ""},
	{"break", 0, 0, 0, 4, any, any, "", "s,t"},
	{"syscall", 0, 0, 0, 5, 0, 0, "", ""},
	{"simcall", 0, 0, 0, 5, 1, 0, "", ""},
	{"rsil", 0, 0, 0, 6, any, any, "", "at,s"},
	{"waiti", 0, 0, 0, 7, any, 0, "", "s"},
	{"any4", 0, 0, 0, 8, any, any, "", "bt,bs4"},
	{"all4", 0, 0, 0, 9, any, any, "", "bt,bs4"},
	{"any8", 0, 0, 0, 10, any, any, "", "bt,bs8"},
	{"all8", 0, 0, 0, 11, any, any, "", "bt,bs8"},
	{"and", 0, 0, 1, any, any, any, "", "ar,as,at"},
	{"or", 0, 0, 2, any, any, any, "", "ar,as,at"},
	{"xor", 0, 0, 3, any, any, any, "", "ar,as,at"},
	// op2=4 ST1, r selects.
	{"ssr", 0, 0, 4, 0, any, 0, "", "as"},
	{"ssl", 0, 0, 4, 1, any, 0, "", "as"},
	{"ssa8l", 0, 0, 4, 2, any, 0, "", "as"},
	{"ssa8b", 0, 0, 4, 3, any, 0, "", "as"},
	{"ssai", 0, 0, 4, 4, any, any, "", "ssai"},
	{"rer", 0, 0, 4, 6, any, any, "", "at,as"},
	{"wer", 0, 0, 4, 7, any, any, "", "at,as"},
	{"rotw", 0, 0, 4, 8, 0, any, "", "rotw"},
	{"nsa", 0, 0, 4, 14, any, any, "", "at,as"},
	{"nsau", 0, 0, 4, 15, any, any, "", "at,as"},
	// op2=5 TLB.
	{"hwwitlba", 0, 0, 5, 1, any, any, "", ""},
	{"ritlb0", 0, 0, 5, 3, any, any, "", "at,as"},
	{"hwwdtlba", 0, 0, 5, 9, any, any, "", ""},
	{"iitlb", 0, 0, 5, 4, any, 0, "", "as"},
	{"pitlb", 0, 0, 5, 5, any, any, "", "at,as"},
	{"witlb", 0, 0, 5, 6, any, any, "", "at,as"},
	{"ritlb1", 0, 0, 5, 7, any, any, "", "at,as"},
	{"rdtlb0", 0, 0, 5, 11, any, any, "", "at,as"},
	{"idtlb", 0, 0, 5, 12, any, 0, "", "as"},
	{"pdtlb", 0, 0, 5, 13, any, any, "", "at,as"},
	{"wdtlb", 0, 0, 5, 14, any, any, "", "at,as"},
	{"rdtlb1", 0, 0, 5, 15, any, any, "", "at,as"},
	// op2=6 RT0, s selects.
	{"neg", 0, 0, 6, any, 0, any, "", "ar,at"},
	{"abs", 0, 0, 6, any, 1, any, "", "ar,at"},
	{"add", 0, 0, 8, any, any, any, "", "ar,as,at"},
	{"addx2", 0, 0, 9, any, any, any, "", "ar,as,at"},
	{"addx4", 0, 0, 10, any, any, any, "", "ar,as,at"},
	{"addx8", 0, 0, 11, any, any, any, "", "ar,as,at"},
	{"sub", 0, 0, 12, any, any, any, "", "ar,as,at"},
	{"subx2", 0, 0, 13, any, any, any, "", "ar,as,at"},
	{"subx4", 0, 0, 14, any, any, any, "", "ar,as,at"},
	{"subx8", 0, 0, 15, any, any, any, "", "ar,as,at"},
	// op1=1 RST1.
	{"slli", 0, 1, 0, any, any, any, "", "ar,as,slli"},
	{"slli", 0, 1, 1, any, any, any, "", "ar,as,slli"},
	{"srai", 0, 1, 2, any, any, any, "", "ar,at,srai"},
	{"srai", 0, 1, 3, any, any, any, "", "ar,at,srai"},
	{"srli", 0, 1, 4, any, any, any, "", "ar,at,s"},
	{"xsr", 0, 1, 6, any, any, any, "", "sr"},
	{"src", 0, 1, 8, any, any, any, "", "ar,as,at"},
	{"srl", 0, 1, 9, any, 0, any, "", "ar,at"},
	{"sll", 0, 1, 10, any, any, 0, "", "ar,as"},
	{"sra", 0, 1, 11, any, 0, any, "", "ar,at"},
	{"mul16u", 0, 1, 12, any, any, any, "", "ar,as,at"},
	{"mul16s", 0, 1, 13, any, any, any, "", "ar,as,at"},
	{"lict", 0, 1, 15, 0, any, any, "", "at,as"},
	{"sict", 0, 1, 15, 1, any, any, "", "at,as"},
	{"licw", 0, 1, 15, 2, any, any, "", "at,as"},
	{"sicw", 0, 1, 15, 3, any, any, "", "at,as"},
	{"ldct", 0, 1, 15, 8, any, any, "", "at,as"},
	{"sdct", 0, 1, 15, 9, any, any, "", "at,as"},
	{"rfdo", 0, 1, 15, 14, 0, 0, "", ""},
	{"rfdd", 0, 1, 15, 14, 0, 1, "", ""},
	// op1=2 RST2.
	{"andb", 0, 2, 0, any, any, any, "", "br,bs,bt"},
	{"andbc", 0, 2, 1, any, any, any, "", "br,bs,bt"},
	{"orb", 0, 2, 2, any, any, any, "", "br,bs,bt"},
	{"orbc", 0, 2, 3, any, any, any, "", "br,bs,bt"},
	{"xorb", 0, 2, 4, any, any, any, "", "br,bs,bt"},
	{"mull", 0, 2, 8, any, any, any, "", "ar,as,at"},
	{"muluh", 0, 2, 10, any, any, any, "", "ar,as,at"},
	{"mulsh", 0, 2, 11, any, any, any, "", "ar,as,at"},
	{"quou", 0, 2, 12, any, any, any, "", "ar,as,at"},
	{"quos", 0, 2, 13, any, any, any, "", "ar,as,at"},
	{"remu", 0, 2, 14, any, any, any, "", "ar,as,at"},
	{"rems", 0, 2, 15, any, any, any, "", "ar,as,at"},
	// op1=3 RST3.
	{"rsr", 0, 3, 0, any, any, any, "", "sr"},
	{"wsr", 0, 3, 1, any, any, any, "", "sr"},
	{"sext", 0, 3, 2, any, any, any, "", "ar,as,t7"},
	{"clamps", 0, 3, 3, any, any, any, "", "ar,as,t7"},
	{"min", 0, 3, 4, any, any, any, "", "ar,as,at"},
	{"max", 0, 3, 5, any, any, any, "", "ar,as,at"},
	{"minu", 0, 3, 6, any, any, any, "", "ar,as,at"},
	{"maxu", 0, 3, 7, any, any, any, "", "ar,as,at"},
	{"moveqz", 0, 3, 8, any, any, any, "", "ar,as,at"},
	{"movnez", 0, 3, 9, any, any, any, "", "ar,as,at"},
	{"movltz", 0, 3, 10, any, any, any, "", "ar,as,at"},
	{"movgez", 0, 3, 11, any, any, any, "", "ar,as,at"},
	{"movf", 0, 3, 12, any, any, any, "", "ar,as,bt"},
	{"movt", 0, 3, 13, any, any, any, "", "ar,as,bt"},
	{"rur", 0, 3, 14, any, any, any, "", "ur"},
	{"wur", 0, 3, 15, any, any, any, "", "ur"},
	// op1=4,5 EXTUI.
	{"extui", 0, 4, any, any, any, any, "", "ar,at,extsh,extmask"},
	{"extui", 0, 5, any, any, any, any, "", "ar,at,extsh,extmask"},
	// op1=8 LSCX, 9 LSC4.
	{"lsx", 0, 8, 0, any, any, any, "", "fr,as,at"},
	{"lsxu", 0, 8, 1, any, any, any, "", "fr,as,at"},
	{"ssx", 0, 8, 4, any, any, any, "", "fr,as,at"},
	{"ssxu", 0, 8, 5, any, any, any, "", "fr,as,at"},
	{"l32e", 0, 9, 0, any, any, any, "", "at,as,imm4e"},
	{"s32e", 0, 9, 4, any, any, any, "", "at,as,imm4e"},
	// op1=10 FP0, 11 FP1.
	{"add.s", 0, 10, 0, any, any, any, "", "fr,fs,ft"},
	{"sub.s", 0, 10, 1, any, any, any, "", "fr,fs,ft"},
	{"mul.s", 0, 10, 2, any, any, any, "", "fr,fs,ft"},
	{"madd.s", 0, 10, 4, any, any, any, "", "fr,fs,ft"},
	{"msub.s", 0, 10, 5, any, any, any, "", "fr,fs,ft"},
	{"round.s", 0, 10, 8, any, any, any, "", "ar,fs,t"},
	{"trunc.s", 0, 10, 9, any, any, any, "", "ar,fs,t"},
	{"floor.s", 0, 10, 10, any, any, any, "", "ar,fs,t"},
	{"ceil.s", 0, 10, 11, any, any, any, "", "ar,fs,t"},
	{"float.s", 0, 10, 12, any, any, any, "", "fr,as,t"},
	{"ufloat.s", 0, 10, 13, any, any, any, "", "fr,as,t"},
	{"utrunc.s", 0, 10, 14, any, any, any, "", "ar,fs,t"},
	{"mov.s", 0, 10, 15, any, any, 0, "", "fr,fs"},
	{"abs.s", 0, 10, 15, any, any, 1, "", "fr,fs"},
	{"rfr", 0, 10, 15, any, any, 4, "", "ar,fs"},
	{"wfr", 0, 10, 15, any, any, 5, "", "fr,as"},
	{"neg.s", 0, 10, 15, any, any, 6, "", "fr,fs"},
	{"un.s", 0, 11, 1, any, any, any, "", "br,fs,ft"},
	{"oeq.s", 0, 11, 2, any, any, any, "", "br,fs,ft"},
	{"ueq.s", 0, 11, 3, any, any, any, "", "br,fs,ft"},
	{"olt.s", 0, 11, 4, any, any, any, "", "br,fs,ft"},
	{"ult.s", 0, 11, 5, any, any, any, "", "br,fs,ft"},
	{"ole.s", 0, 11, 6, any, any, any, "", "br,fs,ft"},
	{"ule.s", 0, 11, 7, any, any, any, "", "br,fs,ft"},
	{"moveqz.s", 0, 11, 8, any, any, any, "", "fr,fs,at"},
	{"movnez.s", 0, 11, 9, any, any, any, "", "fr,fs,at"},
	{"movltz.s", 0, 11, 10, any, any, any, "", "fr,fs,at"},
	{"movgez.s", 0, 11, 11, any, any, any, "", "fr,fs,at"},
	{"movf.s", 0, 11, 12, any, any, any, "", "fr,fs,bt"},
	{"movt.s", 0, 11, 13, any, any, any, "", "fr,fs,bt"},
	// op0=1 L32R.
	{"l32r", 1, any, any, any, any, any, "", "at,l32r"},
	// op0=2 LSAI, r selects; imm8 scaled by the access size.
	{"l8ui", 2, any, any, 0, any, any, "", "at,as,u8x1"},
	{"l16ui", 2, any, any, 1, any, any, "", "at,as,u8x2"},
	{"l32i", 2, any, any, 2, any, any, "", "at,as,u8x4"},
	{"s8i", 2, any, any, 4, any, any, "", "at,as,u8x1"},
	{"s16i", 2, any, any, 5, any, any, "", "at,as,u8x2"},
	{"s32i", 2, any, any, 6, any, any, "", "at,as,u8x4"},
	{"dpfr", 2, any, any, 7, any, 0, "", "as,u8x4"},
	{"dpfw", 2, any, any, 7, any, 1, "", "as,u8x4"},
	{"dpfro", 2, any, any, 7, any, 2, "", "as,u8x4"},
	{"dpfwo", 2, any, any, 7, any, 3, "", "as,u8x4"},
	{"dhwb", 2, any, any, 7, any, 4, "", "as,u8x4"},
	{"dhwbi", 2, any, any, 7, any, 5, "", "as,u8x4"},
	{"dhi", 2, any, any, 7, any, 6, "", "as,u8x4"},
	{"dii", 2, any, any, 7, any, 7, "", "as,u8x4"},
	{"dpfl", 2, 0, any, 7, any, 8, "", "as,imm4x16"},
	{"dhu", 2, 2, any, 7, any, 8, "", "as,imm4x16"},
	{"diu", 2, 3, any, 7, any, 8, "", "as,imm4x16"},
	{"diwb", 2, 4, any, 7, any, 8, "", "as,imm4x16"},
	{"diwbi", 2, 5, any, 7, any, 8, "", "as,imm4x16"},
	{"ipf", 2, any, any, 7, any, 12, "", "as,u8x4"},
	{"ipfl", 2, 0, any, 7, any, 13, "", "as,imm4x16"},
	{"ihu", 2, 2, any, 7, any, 13, "", "as,imm4x16"},
	{"iiu", 2, 3, any, 7, any, 13, "", "as,imm4x16"},
	{"ihi", 2, any, any, 7, any, 14, "", "as,u8x4"},
	{"iii", 2, any, any, 7, any, 15, "", "as,u8x4"},
	{"l16si", 2, any, any, 9, any, any, "", "at,as,u8x2"},
	{"movi", 2, any, any, 10, any, any, "", "at,movi"},
	{"l32ai", 2, any, any, 11, any, any, "", "at,as,u8x4"},
	{"addi", 2, any, any, 12, any, any, "", "at,as,s8"},
	{"addmi", 2, any, any, 13, any, any, "", "at,as,s8x256"},
	{"s32c1i", 2, any, any, 14, any, any, "", "at,as,u8x4"},
	{"s32ri", 2, any, any, 15, any, any, "", "at,as,u8x4"},
	// op0=3 LSCI.
	{"lsi", 3, any, any, 0, any, any, "", "ft,as,u8x4"},
	{"ssi", 3, any, any, 4, any, any, "", "ft,as,u8x4"},
	{"lsiu", 3, any, any, 8, any, any, "", "ft,as,u8x4"},
	{"ssiu", 3, any, any, 12, any, any, "", "ft,as,u8x4"},
	// op0=5 CALLN: n = t[1:0].
	{"call0", 5, any, any, any, any, any, "xx00", "call18"},
	{"call4", 5, any, any, any, any, any, "xx01", "call18"},
	{"call8", 5, any, any, any, any, any, "xx10", "call18"},
	{"call12", 5, any, any, any, any, any, "xx11", "call18"},
	// op0=6 SI: t = m:n.
	{"j", 6, any, any, any, any, any, "xx00", "rel18"},
	{"beqz", 6, any, any, any, any, 1, "", "as,rel12"},
	{"bnez", 6, any, any, any, any, 5, "", "as,rel12"},
	{"bltz", 6, any, any, any, any, 9, "", "as,rel12"},
	{"bgez", 6, any, any, any, any, 13, "", "as,rel12"},
	{"beqi", 6, any, any, any, any, 2, "", "as,b4const,rel8"},
	{"bnei", 6, any, any, any, any, 6, "", "as,b4const,rel8"},
	{"blti", 6, any, any, any, any, 10, "", "as,b4const,rel8"},
	{"bgei", 6, any, any, any, any, 14, "", "as,b4const,rel8"},
	{"entry", 6, any, any, any, any, 3, "", "as,imm12x8"},
	{"bf", 6, any, any, 0, any, 7, "", "bs,rel8"},
	{"bt", 6, any, any, 1, any, 7, "", "bs,rel8"},
	{"loop", 6, any, any, 8, any, 7, "", "as,urel8"},
	{"loopnez", 6, any, any, 9, any, 7, "", "as,urel8"},
	{"loopgtz", 6, any, any, 10, any, 7, "", "as,urel8"},
	{"bltui", 6, any, any, any, any, 11, "", "as,b4constu,rel8"},
	{"bgeui", 6, any, any, any, any, 15, "", "as,b4constu,rel8"},
	// op0=7 B: r selects.
	{"bnone", 7, any, any, 0, any, any, "", "as,at,rel8"},
	{"beq", 7, any, any, 1, any, any, "", "as,at,rel8"},
	{"blt", 7, any, any, 2, any, any, "", "as,at,rel8"},
	{"bltu", 7, any, any, 3, any, any, "", "as,at,rel8"},
	{"ball", 7, any, any, 4, any, any, "", "as,at,rel8"},
	{"bbc", 7, any, any, 5, any, any, "", "as,at,rel8"},
	{"bbci", 7, any, any, 6, any, any, "", "as,bbi,rel8"},
	{"bbci", 7, any, any, 7, any, any, "", "as,bbi,rel8"},
	{"bany", 7, any, any, 8, any, any, "", "as,at,rel8"},
	{"bne", 7, any, any, 9, any, any, "", "as,at,rel8"},
	{"bge", 7, any, any, 10, any, any, "", "as,at,rel8"},
	{"bgeu", 7, any, any, 11, any, any, "", "as,at,rel8"},
	{"bnall", 7, any, any, 12, any, any, "", "as,at,rel8"},
	{"bbs", 7, any, any, 13, any, any, "", "as,at,rel8"},
	{"bbsi", 7, any, any, 14, any, any, "", "as,bbi,rel8"},
	{"bbsi", 7, any, any, 15, any, any, "", "as,bbi,rel8"},
	// Narrow (16-bit) encodings, op0 8..13.
	{"l32i.n", 8, any, any, any, any, any, "", "at,as,r4"},
	{"s32i.n", 9, any, any, any, any, any, "", "at,as,r4"},
	{"add.n", 10, any, any, any, any, any, "", "ar,as,at"},
	{"addi.n", 11, any, any, any, any, any, "", "ar,as,addin"},
	{"movi.n", 12, any, any, any, any, any, "0xxx", "as,movin"},
	{"beqz.n", 12, any, any, any, any, any, "10xx", "as,rel6n"},
	{"bnez.n", 12, any, any, any, any, any, "11xx", "as,rel6n"},
	{"mov.n", 13, any, any, 0, any, any, "", "at,as"},
	{"ret.n", 13, any, any, 15, any, 0, "", ""},
	{"retw.n", 13, any, any, 15, any, 1, "", ""},
	{"break.n", 13, any, any, 15, any, 2, "", "sx"},
	{"nop.n", 13, any, any, 15, 0, 3, "", ""},
	{"ill.n", 13, any, any, 15, 0, 6, "", ""},
}

var b4const = [16]int32{-1, 1, 2, 3, 4, 5, 6, 7, 8, 10, 12, 16, 32, 64, 128, 256}
var b4constu = [16]int32{32768, 65536, 2, 3, 4, 5, 6, 7, 8, 10, 12, 16, 32, 64, 128, 256}

// specialRegs names the special registers of RSR/WSR/XSR.
var specialRegs = map[int]string{
	0: "lbeg", 1: "lend", 2: "lcount", 3: "sar", 4: "br", 5: "litbase", 12: "scompare1",
	16: "acclo", 17: "acchi", 32: "m0", 33: "m1", 34: "m2", 35: "m3",
	72: "windowbase", 73: "windowstart", 83: "ptevaddr", 89: "mmid", 90: "rasid", 91: "itlbcfg", 92: "dtlbcfg",
	96: "ibreakenable", 97: "memctl", 98: "cacheattr", 99: "atomctl", 104: "ddr", 106: "mepc", 107: "meps",
	108: "mesave", 109: "mesr", 110: "mecr", 111: "mevaddr", 128: "ibreaka0", 129: "ibreaka1",
	144: "dbreaka0", 145: "dbreaka1", 160: "dbreakc0", 161: "dbreakc1",
	177: "epc1", 178: "epc2", 179: "epc3", 180: "epc4", 181: "epc5", 182: "epc6", 183: "epc7",
	192: "depc", 194: "eps2", 195: "eps3", 196: "eps4", 197: "eps5", 198: "eps6", 199: "eps7",
	209: "excsave1", 210: "excsave2", 211: "excsave3", 212: "excsave4", 213: "excsave5", 214: "excsave6", 215: "excsave7",
	224: "cpenable", 226: "interrupt", 227: "intclear", 228: "intenable", 230: "ps", 231: "vecbase",
	232: "exccause", 233: "debugcause", 234: "ccount", 235: "prid", 236: "icount", 237: "icountlevel",
	238: "excvaddr", 240: "ccompare0", 241: "ccompare1", 242: "ccompare2", 244: "misc0", 245: "misc1", 246: "misc2", 247: "misc3",
}

var userRegs = map[int]string{231: "threadptr", 232: "fcr", 233: "fsr"}

// Decode decodes the instruction at the start of code, located at pc.
func Decode(code []byte, pc uint64) (Inst, error) {
	if len(code) < 2 {
		return Inst{}, errShort
	}
	op0 := int8(code[0] & 0xf)
	narrow := op0 >= 8 && op0 <= 13
	if op0 >= 14 {
		return Inst{}, errUnknown // FLIX bundles; not on these cores
	}
	if !narrow && len(code) < 3 {
		return Inst{}, errShort
	}
	var x uint32
	if narrow {
		x = uint32(code[0]) | uint32(code[1])<<8
	} else {
		x = uint32(code[0]) | uint32(code[1])<<8 | uint32(code[2])<<16
	}
	t, s, r := int8(x>>4&0xf), int8(x>>8&0xf), int8(x>>12&0xf)
	op1, op2 := int8(x>>16&0xf), int8(x>>20&0xf)
	for i := range encodings {
		e := &encodings[i]
		if e.op0 != op0 || !match(e.op1, op1) || !match(e.op2, op2) || !match(e.r, r) || !match(e.s, s) || !match(e.t, t) {
			continue
		}
		if e.tpat != "" && !matchPattern(e.tpat, uint32(t)) {
			continue
		}
		f := &fields{x: x, t: t, s: s, r: r, op1: op1, op2: op2, pc: pc}
		inst := Inst{Len: 3, Mnemonic: strings.ToUpper(e.mn)}
		if narrow {
			inst.Len = 2
		}
		mn := e.mn
		var ops []string
		if e.ops != "" {
			for _, op := range strings.Split(e.ops, ",") {
				if op == "sr" || op == "ur" {
					// rsr.ps a2: the register names the mnemonic.
					name, reg := f.sysreg(op)
					if name != "" {
						mn += "." + name
						ops = append(ops, "a"+strconv.Itoa(int(t)))
					} else {
						ops = append(ops, "a"+strconv.Itoa(int(t)), strconv.Itoa(reg))
					}
					continue
				}
				ops = append(ops, f.operand(op, &inst))
			}
		}
		inst.Text = mn
		if len(ops) > 0 {
			inst.Text += " " + strings.Join(ops, ", ")
		}
		inst.Call = strings.HasPrefix(e.mn, "call")
		return inst, nil
	}
	return Inst{}, errUnknown
}

func match(want, got int8) bool { return want == any || want == got }

func matchPattern(pat string, v uint32) bool {
	for i, c := range pat {
		bit := v >> uint(len(pat)-1-i) & 1
		if c == '0' && bit != 0 || c == '1' && bit != 1 {
			return false
		}
	}
	return true
}

type fields struct {
	x             uint32
	t, s, r       int8
	op1, op2      int8
	pc            uint64
}

func (f *fields) sysreg(kind string) (string, int) {
	n := int(f.x >> 8 & 0xff)
	if kind == "ur" {
		return userRegs[n], n
	}
	return specialRegs[n], n
}

func sext(v uint32, bits uint) int64 {
	if v&(1<<(bits-1)) != 0 {
		return int64(v) - 1<<bits
	}
	return int64(v)
}

func (f *fields) operand(op string, inst *Inst) string {
	imm8 := f.x >> 16 & 0xff
	// Immediates print in decimal when small, in (32-bit two's
	// complement) hex otherwise, as xtensa-objdump does.
	num := func(v int64) string {
		if v > -256 && v < 256 {
			return strconv.FormatInt(v, 10)
		}
		return "0x" + strconv.FormatUint(uint64(uint32(v)), 16)
	}
	target := func(addr int64, branch bool) string {
		inst.Target, inst.HasTarget, inst.Branch = uint64(uint32(addr)), true, branch
		return "0x" + strconv.FormatUint(inst.Target, 16)
	}
	switch op {
	case "ar", "as", "at", "fr", "fs", "ft", "br", "bs", "bt":
		return op[:1] + strconv.Itoa(int(f.reg(op[1])))
	case "bs4", "bs8":
		n, _ := strconv.Atoi(op[2:])
		base := int(f.s) &^ (n - 1)
		var parts []string
		for i := 0; i < n; i++ {
			parts = append(parts, "b"+strconv.Itoa(base+i))
		}
		return strings.Join(parts, ":")
	case "s":
		return num(int64(f.s))
	case "sx":
		return "0x" + strconv.FormatInt(int64(f.s), 16)
	case "imm4x16":
		return num(int64(f.op2) * 16)
	case "t":
		return num(int64(f.t))
	case "t7":
		return num(int64(f.t) + 7)
	case "r4":
		return num(int64(f.r) * 4)
	case "imm4e":
		return num(-64 + int64(f.r)*4)
	case "u8x1":
		return num(int64(imm8))
	case "u8x2":
		return num(int64(imm8) * 2)
	case "u8x4":
		return num(int64(imm8) * 4)
	case "s8":
		return num(sext(imm8, 8))
	case "s8x256":
		return num(sext(imm8, 8) * 256)
	case "movi":
		return num(sext(imm8|uint32(f.s)<<8, 12))
	case "imm12x8":
		return num(int64(f.x>>12&0xfff) * 8)
	case "b4const":
		return num(int64(b4const[f.r]))
	case "b4constu":
		return num(int64(b4constu[f.r]))
	case "bbi":
		return num(int64(f.t) | int64(f.r&1)<<4)
	case "ssai":
		return num(int64(f.s) | int64(f.t&1)<<4)
	case "rotw":
		return num(sext(uint32(f.t), 4))
	case "slli":
		return num(32 - (int64(f.t) | int64(f.op2&1)<<4))
	case "srai":
		return num(int64(f.s) | int64(f.op2&1)<<4)
	case "extsh":
		return num(int64(f.s) | int64(f.op1&1)<<4)
	case "extmask":
		return num(int64(f.op2) + 1)
	case "addin":
		if f.t == 0 {
			return "-1"
		}
		return num(int64(f.t))
	case "movin":
		v := int64(f.r) | int64(f.t&7)<<4
		if v>>5 == 3 {
			v -= 128
		}
		return num(v)
	case "rel6n":
		return target(int64(f.pc)+4+(int64(f.r)|int64(f.t&3)<<4), true)
	case "rel8":
		return target(int64(f.pc)+4+sext(imm8, 8), true)
	case "urel8":
		return target(int64(f.pc)+4+int64(imm8), true)
	case "rel12":
		return target(int64(f.pc)+4+sext(f.x>>12&0xfff, 12), true)
	case "rel18":
		return target(int64(f.pc)+4+sext(f.x>>6&0x3ffff, 18), true)
	case "call18":
		return target(int64(f.pc&^3)+4+sext(f.x>>6&0x3ffff, 18)*4, true)
	case "l32r":
		// The literal lies below the instruction: a 16-bit word offset
		// sign-extended as always negative, from the aligned PC+3.
		off := (int64(f.x>>8&0xffff) | ^0xffff) * 4
		return target(int64((f.pc+3)&^3)+off, false)
	}
	panic("xtensaasm: unknown operand code " + op)
}

func (f *fields) reg(which byte) int8 {
	switch which {
	case 'r':
		return f.r
	case 's':
		return f.s
	}
	return f.t
}

