package wasmobj

import (
	"bytes"
	"debug/dwarf"
	"fmt"
	"os"
	"sort"
	"strings"

	wasmdisasm "github.com/go-interpreter/wagon/disasm"
	"github.com/go-interpreter/wagon/wasm"
	"github.com/go-interpreter/wagon/wasm/operators"

	"loov.dev/lensm/internal/disasm"
)

var _ disasm.File = (*File)(nil)
var _ disasm.Func = (*Func)(nil)

// File contains information about the object file.
type File struct {
	module *wasm.Module
	dwarf  *dwarf.Data

	funcs []disasm.Func
}

func (file *File) Funcs() []disasm.Func { return file.funcs }

// Func contains information about the executable.
type Func struct {
	obj      *File
	fn       *wasm.Function
	sortName string
}

func (fn *Func) Name() string { return fn.fn.Name }

func (file *File) Close() error {
	return nil
}

func Load(path string) (*File, error) {
	obj := &File{}

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
		sym := &Func{
			obj:      obj,
			fn:       &fn,
			sortName: strings.ToLower(fn.Name),
		}
		obj.funcs = append(obj.funcs, sym)
	}

	sort.SliceStable(obj.funcs, func(i, k int) bool {
		return strings.ToLower(obj.funcs[i].Name()) < strings.ToLower(obj.funcs[k].Name())
	})

	return obj, nil
}

func (fn *Func) Load(opts disasm.Options) *disasm.Code {
	return fn.obj.LoadCode(fn, opts)
}

func (file *File) LoadCode(fn *Func, opts disasm.Options) *disasm.Code {
	dis, err := wasmdisasm.NewDisassembly(*fn.fn, file.module)
	if err != nil {
		return &disasm.Code{Name: err.Error()}
	}

	code := &disasm.Code{
		Name: fn.fn.Name,
	}

	for i, ix := range dis.Code {
		code.Insts = append(code.Insts, file.toInstr(dis, i, ix))
	}

	return code
}

func (file *File) toInstr(dis *wasmdisasm.Disassembly, i int, ix wasmdisasm.Instr) disasm.Inst {
	inst := disasm.Inst{
		PC:   uint64(i),
		Text: ix.Op.Name + " " + file.immediatesToString(ix.Immediates),
	}

	switch ix.Op.Code {
	case operators.Call:
		target := ix.Immediates[0].(uint32)
		fn := file.module.FunctionIndexSpace[target]
		inst.Text = ix.Op.Name + " " + fn.Name
		inst.Call = fn.Name

	// TODO: figure out ix.Branches and ix.Block.IfElseIndex (similar)
	default:

	}

	return inst
}

func (file *File) immediatesToString(xs []interface{}) string {
	var str strings.Builder
	for _, im := range xs {
		fmt.Fprintf(&str, " %v", im)
	}
	return str.String()
}
