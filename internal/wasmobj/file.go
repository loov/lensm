package main

import (
	"bytes"
	"debug/dwarf"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/go-interpreter/wagon/disasm"
	"github.com/go-interpreter/wagon/wasm"
	"github.com/go-interpreter/wagon/wasm/operators"
)

var _ Obj = (*WasmObj)(nil)
var _ Symbol = (*WasmSymbol)(nil)

// WasmObj contains information about the object file.
type WasmObj struct {
	module *wasm.Module
	dwarf  *dwarf.Data

	symbols  []*WasmSymbol
	symbols2 []Symbol
}

func (exe *WasmObj) Symbols() []Symbol { return exe.symbols2 }

// WasmSymbol contains information about the executable.
type WasmSymbol struct {
	obj      *WasmObj
	fn       *wasm.Function
	sortName string
}

func (sym *WasmSymbol) Name() string { return sym.fn.Name }

func (exe *WasmObj) Close() error {
	return nil
}

func LoadWASM(path string) (*WasmObj, error) {
	obj := &WasmObj{}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	module, err := wasm.ReadModule(bytes.NewReader(data),
		func(name string) (*wasm.Module, error) {
			return nil, fmt.Errorf("not found %q", name)
		})
	if err != nil {
		return nil, err
	}
	obj.module = module

	dwarfdata, err := dwarf.New(
		module.Custom(".debug_abbrev").Data,
		nil,
		nil,
		module.Custom(".debug_info").Data,
		module.Custom(".debug_line").Data,
		nil,
		module.Custom(".debug_ranges").Data,
		module.Custom(".debug_str").Data,
	)
	if err != nil {
		return nil, err
	}

	rd := dwarfdata.Reader()
	for {
		entry, err := rd.Next()
		if entry == nil && err == nil {
			continue
		}
		if err != nil {
			fmt.Println(err)
			break
		}
		if entry.Tag == dwarf.TagCompileUnit {
			fmt.Println("loading", entry.Field)
			lrd, err := dwarfdata.LineReader(entry)
			if err != nil {
				fmt.Println(err)
				break
			}

			for _, fln := range lrd.Files() {
				fmt.Println(fln)
			}

			var lineEntry dwarf.LineEntry
			for lrd.Next(&lineEntry) == nil {
				fmt.Println(
					lineEntry.Address,
					lineEntry.Line,
					lineEntry.Column,
					lineEntry.File.Name,
				)
			}
		}
	}

	obj.dwarf = dwarfdata

	for _, fn := range module.FunctionIndexSpace {
		fn := fn
		sym := &WasmSymbol{
			obj:      obj,
			fn:       &fn,
			sortName: strings.ToLower(fn.Name),
		}
		obj.symbols = append(obj.symbols, sym)
	}

	sort.SliceStable(obj.symbols, func(i, k int) bool {
		return obj.symbols[i].sortName < obj.symbols[k].sortName
	})
	for _, sym := range obj.symbols {
		obj.symbols2 = append(obj.symbols2, sym)
	}

	return obj, nil
}

func (sym *WasmSymbol) Load(opts Options) *Code {
	return sym.obj.LoadSymbol(sym, opts)
}

func (exe *WasmObj) LoadSymbol(sym *WasmSymbol, opts Options) *Code {
	dis, err := disasm.NewDisassembly(*sym.fn, exe.module)
	if err != nil {
		return &Code{Name: err.Error()}
	}

	code := &Code{
		Name: sym.fn.Name,
	}

	for i, ix := range dis.Code {
		code.Insts = append(code.Insts, exe.toInstr(dis, i, ix))
	}

	return code
}

func (exe *WasmObj) toInstr(dis *disasm.Disassembly, i int, ix disasm.Instr) Inst {
	inst := Inst{
		PC:   uint64(i),
		Text: ix.Op.Name + " " + exe.immediatesToString(ix.Immediates),
	}

	switch ix.Op.Code {
	case operators.Call:
		target := ix.Immediates[0].(uint32)
		fn := exe.module.FunctionIndexSpace[target]
		inst.Text = ix.Op.Name + " " + fn.Name
		inst.Call = fn.Name

	// TODO: figure out ix.Branches and ix.Block.IfElseIndex (similar)
	default:

	}

	return inst
}

func (exe *WasmObj) immediatesToString(xs []interface{}) string {
	var str strings.Builder
	for _, im := range xs {
		fmt.Fprintf(&str, " %v", im)
	}
	return str.String()
}
