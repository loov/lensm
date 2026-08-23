package objfile

import (
	"fmt"
	"io"
	"sync"

	"golang.org/x/arch/arm/armasm"
	"golang.org/x/arch/arm64/arm64asm"
	"golang.org/x/arch/loong64/loong64asm"
	"golang.org/x/arch/ppc64/ppc64asm"
	"golang.org/x/arch/riscv64/riscv64asm"
	"golang.org/x/arch/s390x/s390xasm"
	"golang.org/x/arch/x86/x86asm"

	"loov.dev/lensm/internal/thumbasm"
)

// decodeMu serializes the x/arch decoders, which mutate package-level
// state on every Decode.
var decodeMu sync.Mutex

// Inst is a single decoded instruction.
type Inst struct {
	Addr uint64
	Len  int
	// Op is the canonical decoder mnemonic, e.g. "LD1" where the Go
	// syntax spells it "VLD1"; empty for undecodable bytes.
	Op string
	// Text is the Go assembler syntax, GNU the native syntax.
	Text, GNU string
	// Ref is the absolute target of a branch or call when the decoder
	// resolved one (Thumb); zero otherwise, where callers parse Text.
	Ref uint64
	// Call reports that Ref is a call target rather than a jump.
	Call bool
}

// Disassemble decodes the machine code of fn. Undecodable bytes become
// BYTE pseudo-instructions so padding or data inside a function does
// not abort decoding.
func (b *Binary) Disassemble(fn *Func) ([]Inst, error) {
	decodeMu.Lock()
	defer decodeMu.Unlock()

	code, addr := fn.Code(), fn.Addr
	lookup := b.Lookup
	var insts []Inst
	emit := func(n int, op, text, gnu string) {
		insts = append(insts, Inst{Addr: addr, Len: n, Op: op, Text: text, GNU: gnu})
		code, addr = code[n:], addr+uint64(n)
	}
	undecodable := func(n int) {
		n = min(n, len(code))
		emit(n, "", fmt.Sprintf("BYTE %#x", code[:n]), fmt.Sprintf(".byte %#x", code[:n]))
	}
	reader := textReader{code, addr}

	switch b.Arch {
	case "amd64", "386":
		mode := map[string]int{"amd64": 64, "386": 32}[b.Arch]
		for len(code) > 0 {
			inst, err := x86asm.Decode(code, mode)
			if err != nil || inst.Len == 0 || inst.Op == 0 {
				undecodable(1)
				continue
			}
			emit(inst.Len, inst.Op.String(), x86asm.GoSyntax(inst, addr, lookup), x86asm.GNUSyntax(inst, addr, nil))
		}
	case "arm64":
		for len(code) > 0 {
			inst, err := arm64asm.Decode(code)
			if err != nil || inst.Op == 0 {
				undecodable(4)
				continue
			}
			emit(4, inst.Op.String(), arm64asm.GoSyntax(inst, addr, lookup, reader), arm64asm.GNUSyntax(inst))
		}
	case "arm":
		// Mapping symbols split the text into ARM, Thumb and data
		// regions; a binary without them is all ARM.
		data := func(w int) {
			w = min(w, len(code))
			var v uint64
			for i := w - 1; i >= 0; i-- {
				v = v<<8 | uint64(code[i])
			}
			directive := map[int]string{4: ".word", 2: ".short", 1: ".byte"}[w]
			text := fmt.Sprintf("%s %#0*x", directive, 2*w+2, v)
			emit(w, "", text, text)
		}
		for len(code) > 0 {
			kind, end := byte('a'), addr+uint64(len(code))
			if b.arm32 != nil {
				kind, end = b.arm32.at(addr, end)
			}
			switch kind {
			case 't':
				var dec thumbasm.Decoder
				for addr < end {
					inst, err := dec.Decode(code[:end-addr], addr)
					if err != nil {
						undecodable(2)
						continue
					}
					text := inst.Text
					if inst.HasTarget {
						if name, base := lookup(inst.Target); name != "" {
							if base == inst.Target {
								text += " <" + name + ">"
							} else {
								text += fmt.Sprintf(" <%s+%#x>", name, inst.Target-base)
							}
						}
					}
					in := Inst{Addr: addr, Len: inst.Len, Op: inst.Mnemonic, Text: text, GNU: text, Call: inst.Call}
					if inst.Branch {
						in.Ref = inst.Target
					}
					insts = append(insts, in)
					code, addr = code[inst.Len:], addr+uint64(inst.Len)
				}
			case 'd':
				for addr < end {
					switch n := end - addr; {
					case n >= 4 && addr%4 == 0:
						data(4)
					case n >= 2 && addr%2 == 0:
						data(2)
					default:
						data(1)
					}
				}
			default:
				for addr < end {
					inst, err := armasm.Decode(code[:end-addr], armasm.ModeARM)
					if err != nil || inst.Len == 0 || inst.Op == 0 {
						undecodable(4)
						continue
					}
					emit(inst.Len, inst.Op.String(), armasm.GoSyntax(inst, addr, lookup, reader), armasm.GNUSyntax(inst))
				}
			}
		}
	case "loong64":
		for len(code) > 0 {
			inst, err := loong64asm.Decode(code)
			if err != nil || inst.Op == 0 {
				undecodable(4)
				continue
			}
			emit(4, inst.Op.String(), loong64asm.GoSyntax(inst, addr, lookup), loong64asm.GNUSyntax(inst))
		}
	case "ppc64", "ppc64le":
		for len(code) > 0 {
			inst, err := ppc64asm.Decode(code, b.byteOrder)
			if err != nil || inst.Len == 0 {
				undecodable(4)
				continue
			}
			emit(inst.Len, inst.Op.String(), ppc64asm.GoSyntax(inst, addr, lookup), ppc64asm.GNUSyntax(inst, addr))
		}
	case "riscv64", "riscv32":
		for len(code) > 0 {
			inst, err := riscv64asm.Decode(code)
			if err != nil || inst.Len == 0 || inst.Op == 0 {
				undecodable(2)
				continue
			}
			emit(inst.Len, inst.Op.String(), riscv64asm.GoSyntax(inst, addr, lookup, reader), riscv64asm.GNUSyntax(inst))
		}
	case "s390x":
		for len(code) > 0 {
			inst, err := s390xasm.Decode(code)
			if err != nil || inst.Len == 0 || inst.Op == 0 {
				undecodable(2)
				continue
			}
			emit(inst.Len, inst.Op.String(), s390xasm.GoSyntax(inst, addr, lookup), s390xasm.GNUSyntax(inst, addr))
		}
	default:
		return nil, fmt.Errorf("unsupported architecture %q", b.Arch)
	}
	return insts, nil
}

// textReader serves the function's code at its virtual address, so
// GoSyntax can render pc-relative literal loads as constants.
type textReader struct {
	code []byte
	addr uint64
}

func (r textReader) ReadAt(p []byte, off int64) (int, error) {
	if off < int64(r.addr) || off-int64(r.addr) >= int64(len(r.code)) {
		return 0, io.EOF
	}
	n := copy(p, r.code[off-int64(r.addr):])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}
