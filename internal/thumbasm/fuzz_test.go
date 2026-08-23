package thumbasm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestFuzzObjdump generates random instances of every encoding, wraps them
// in a minimal ELF and compares the decoding with llvm-objdump. It runs
// only when THUMBASM_FUZZ is set, since it depends on an external tool.
func TestFuzzObjdump(t *testing.T) {
	if os.Getenv("THUMBASM_FUZZ") == "" {
		t.Skip("THUMBASM_FUZZ not set")
	}
	objdump := os.Getenv("OBJDUMP")
	if objdump == "" {
		objdump = "/usr/bin/objdump"
	}
	prepare.Do(compileTables)
	rng := rand.New(rand.NewPCG(1, 2))
	const perEncoding = 40
	const textAddr = 0x10000

	// Each sample is followed by four NOPs: enough to resynchronise after
	// a length disagreement and to drain any IT block it opened.
	var text bytes.Buffer
	type sample struct {
		addr uint64
		enc  string
	}
	var samples []sample
	simd := map[string]bool{}
	for _, e := range encodings {
		if e.simd {
			simd[e.name] = true
		}
		for range perEncoding {
			x := e.value | rng.Uint32()&^e.mask
			x = x&^e.sbMask | e.sbValue
			if e.width == 16 {
				x &= 0xffff
			}
			samples = append(samples, sample{textAddr + uint64(text.Len()), e.name})
			if e.width == 32 {
				binary.Write(&text, binary.LittleEndian, uint16(x>>16))
				binary.Write(&text, binary.LittleEndian, uint16(x))
			} else {
				binary.Write(&text, binary.LittleEndian, uint16(x))
			}
			for range 4 {
				binary.Write(&text, binary.LittleEndian, uint16(0xbf00))
			}
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "fuzz.elf")
	if err := os.WriteFile(path, minimalELF(text.Bytes(), textAddr), 0o644); err != nil {
		t.Fatal(err)
	}
	triple := os.Getenv("OBJDUMP_TRIPLE")
	if triple == "" {
		triple = "thumbv8a-none-eabi"
	}
	out, err := exec.Command(objdump, "-d", "--triple="+triple, path).Output()
	if err != nil {
		t.Skipf("objdump: %v", err)
	}
	ref := parseObjdump(string(out))

	data := text.Bytes()
	var dec Decoder
	mismatch := map[string]int{}
	shown := map[string]int{}
	var total, undecoded, unknownRef int
	pc := uint64(textAddr)
	si := 0
	for int(pc-textAddr) < len(data) {
		inst, err := dec.Decode(data[pc-textAddr:], pc)
		want, ok := ref[pc]
		enc := ""
		if si < len(samples) && samples[si].addr == pc {
			enc = samples[si].enc
			si++
		} else if si < len(samples) && samples[si].addr < pc {
			si++
		}
		if err != nil {
			if ok && !strings.HasPrefix(want, "<unknown>") && enc != "" && !acceptedDifference(enc) && !simd[enc] {
				undecoded++
				mismatch[enc]++
				if shown[enc] < 2 {
					shown[enc]++
					t.Errorf("%s %#x: undecoded (%v), objdump %q", enc, pc, debugDecode(data[pc-textAddr:], pc, enc), want)
				}
			}
			pc += 2
			continue
		}
		if acceptedDifference(enc) || simd[enc] {
			pc += uint64(inst.Len)
			continue
		}
		if enc == "" {
			pc += uint64(inst.Len)
			continue
		}
		total++
		if !ok || strings.HasPrefix(want, "<unknown>") {
			unknownRef++
			pc += uint64(inst.Len)
			continue
		}
		if strings.Contains(want, "[pc") || strings.HasPrefix(want, "adr") {
			pc += uint64(inst.Len)
			continue
		}
		if normalize(inst.Text) != normalize(want) {
			mismatch[enc]++
			if shown[enc] < 2 {
				shown[enc]++
				t.Errorf("%s %#x: got %q, objdump %q", enc, pc, inst.Text, want)
			}
		}
		pc += uint64(inst.Len)
	}
	t.Logf("%d decoded, %d undecoded, %d objdump-unknown, %d encodings with mismatches", total, undecoded, unknownRef, len(mismatch))
}

// acceptedDifference lists encodings where thumbasm and llvm-objdump
// legitimately disagree: coprocessor transfers (not relevant to T32-only
// cores, and spelled differently), and the single-register PUSH/POP
// aliases the manual prefers but objdump spells as STR/LDR.
func acceptedDifference(enc string) bool {
	for _, prefix := range []string{
		"LDC", "STC", "MCR", "MRC", "MCRR", "MRRC", "CDP", "POP_LDR", "PUSH_STR",
		// A-profile system instructions; objdump spells their operands
		// with A-profile register names.
		"CPS", "MRS", "MSR", "SRS", "RFE",
		// Doubleword loads/stores with Rn=PC (UNPREDICTABLE or literal).
		"LDRD", "STRD",
		// ISB options other than SY print by name here, by number there.
		"ISB",
		// Deprecated FPINST registers and the FLDMX/FSTMX forms.
		"VMRS", "VMSR", "FLDM", "FSTM",
		// UNPREDICTABLE operand combinations objdump resolves differently
		// (Rn != Rm for CLZ/REV/RBIT, msb < lsb for BFC/BFI, IT with NV).
		"CLZ_", "RBIT_", "REV", "BFC_", "BFI_", "IT_T1",
		// Hints objdump prints generically, and its UDF #0xfe = trap.
		"CLRBHB", "ESB", "TSB", "UDF_T1",
	} {
		if strings.HasPrefix(enc, prefix) {
			return true
		}
	}
	return false
}

// debugDecode reports why a specific encoding rejected an instruction.
func debugDecode(code []byte, pc uint64, name string) error {
	for i := range encodings {
		e := &encodings[i]
		if e.name != name {
			continue
		}
		x := uint32(binary.LittleEndian.Uint16(code))
		if e.width == 32 {
			x = x<<16 | uint32(binary.LittleEndian.Uint16(code[2:]))
		}
		ev := &env{vars: map[string]value{}}
		for _, f := range e.fields {
			ev.set(f.name, mkBits(uint64(x>>uint(f.lo)), f.n))
		}
		if e.condExpr != nil {
			if v, err := ev.eval(e.condExpr); err != nil || !v.truthy() {
				return fmt.Errorf("cond %q: %v", e.cond, err)
			}
		}
		if e.prog != nil {
			if err := ev.run(e.prog); err != nil {
				return fmt.Errorf("decode: %v", err)
			}
		}
		if e.aliasExpr != nil {
			if v, err := ev.eval(e.aliasExpr); err != nil || !v.truthy() {
				return fmt.Errorf("alias %q: %v", e.alias, err)
			}
		}
		f := &formatter{enc: e, env: ev, raw: x, pc: pc, width: e.width, cond: 14}
		_, err := f.format()
		return fmt.Errorf("format: %v", err)
	}
	return fmt.Errorf("no such encoding")
}

// minimalELF wraps code in an ELF32 ARM executable with a .text section
// and a $t mapping symbol, enough for objdump to disassemble it as Thumb.
func minimalELF(code []byte, addr uint64) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	// Layout: ehdr(52) | code | shstrtab | strtab | symtab | shdrs
	const ehsz = 52
	codeOff := ehsz
	shstr := []byte("\x00.text\x00.shstrtab\x00.strtab\x00.symtab\x00")
	shstrOff := codeOff + len(code)
	strtab := []byte("\x00$t\x00")
	strtabOff := shstrOff + len(shstr)
	// symtab: null + $t (st_name=1, value=addr, size 0, info NOTYPE LOCAL, shndx=1)
	var symtab bytes.Buffer
	symtab.Write(make([]byte, 16))
	binary.Write(&symtab, le, uint32(1))
	binary.Write(&symtab, le, uint32(addr))
	binary.Write(&symtab, le, uint32(0))
	symtab.WriteByte(0) // info
	symtab.WriteByte(0) // other
	binary.Write(&symtab, le, uint16(1))
	symtabOff := strtabOff + len(strtab)
	shOff := symtabOff + symtab.Len()
	shOff = (shOff + 3) &^ 3

	// ELF header
	b.Write([]byte{0x7f, 'E', 'L', 'F', 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	binary.Write(&b, le, uint16(2))         // ET_EXEC
	binary.Write(&b, le, uint16(40))        // EM_ARM
	binary.Write(&b, le, uint32(1))         // version
	binary.Write(&b, le, uint32(addr))      // entry
	binary.Write(&b, le, uint32(0))         // phoff
	binary.Write(&b, le, uint32(shOff))     // shoff
	binary.Write(&b, le, uint32(0x5000000)) // flags: EABI5
	binary.Write(&b, le, uint16(ehsz))
	binary.Write(&b, le, uint16(32)) // phentsize
	binary.Write(&b, le, uint16(0))  // phnum
	binary.Write(&b, le, uint16(40)) // shentsize
	binary.Write(&b, le, uint16(5))  // shnum
	binary.Write(&b, le, uint16(2))  // shstrndx
	b.Write(code)
	b.Write(shstr)
	b.Write(strtab)
	b.Write(symtab.Bytes())
	for b.Len() < shOff {
		b.WriteByte(0)
	}
	shdr := func(name, typ, flags, addr, off, size, link, info, align, entsize uint32) {
		for _, v := range []uint32{name, typ, flags, addr, off, size, link, info, align, entsize} {
			binary.Write(&b, le, v)
		}
	}
	shdr(0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	shdr(1, 1, 6, uint32(addr), uint32(codeOff), uint32(len(code)), 0, 0, 4, 0) // .text PROGBITS ALLOC|EXEC
	shdr(7, 3, 0, 0, uint32(shstrOff), uint32(len(shstr)), 0, 0, 1, 0)          // .shstrtab
	shdr(17, 3, 0, 0, uint32(strtabOff), uint32(len(strtab)), 0, 0, 1, 0)       // .strtab
	shdr(25, 2, 0, 0, uint32(symtabOff), uint32(symtab.Len()), 3, 1, 4, 16)     // .symtab
	return b.Bytes()
}
