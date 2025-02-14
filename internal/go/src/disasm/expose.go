package disasm

import "loov.dev/lensm/internal/go/src/objfile"

func (d *Disasm) Syms() []objfile.Sym { return d.syms }
func (d *Disasm) TextStart() uint64   { return d.textStart }
func (d *Disasm) TextEnd() uint64     { return d.textEnd }
func (d *Disasm) PCLN() objfile.Liner { return d.pcln }
