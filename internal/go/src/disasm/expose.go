package disasm

import (
	"strings"

	"loov.dev/lensm/internal/go/src/objfile"
)

func (d *Disasm) Syms() []objfile.Sym { return d.syms }
func (d *Disasm) TextStart() uint64   { return d.textStart }
func (d *Disasm) TextEnd() uint64     { return d.textEnd }
func (d *Disasm) PCLN() objfile.Liner { return d.pcln }
func (d *Disasm) GOARCH() string      { return d.goarch }

// DecodeSyntax disassembles the text segment range [start, end), calling f for
// each instruction with Go assembler syntax and native (GNU) syntax separately.
//
// This is a lensm addition, re-applied on every `go generate` because upstream
// Decode only yields a single combined syntax. It relies on Decode's gnuAsm=true
// format of "%-36s // %s" (goText // gnuText) to split the two apart.
func (d *Disasm) DecodeSyntax(start, end uint64, relocs []objfile.Reloc, f func(pc, size uint64, file string, line int, goText, nativeText string)) {
	if start < d.textStart {
		start = d.textStart
	}
	if end > d.textEnd {
		end = d.textEnd
	}
	code := d.text[:end-d.textStart]
	lookup := d.lookup
	for pc := start; pc < end; {
		i := pc - d.textStart
		combined, size := d.disasm(code[i:], pc, lookup, d.byteOrder, true)
		goText, nativeText := combined, combined
		if j := strings.Index(combined, " // "); j >= 0 {
			goText = strings.TrimRight(combined[:j], " ")
			nativeText = combined[j+len(" // "):]
		}
		file, line, _ := d.pcln.PCToLine(pc)
		reloc := ""
		sep := "\t"
		for len(relocs) > 0 && relocs[0].Addr < i+uint64(size) {
			reloc += sep + relocs[0].Stringer.String(pc-start)
			sep = " "
			relocs = relocs[1:]
		}
		f(pc, uint64(size), file, line, goText+reloc, nativeText+reloc)
		pc += uint64(size)
	}
}
