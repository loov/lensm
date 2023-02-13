package wasmobj

import (
	"debug/dwarf"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/tetratelabs/wabin/binary"
	"github.com/tetratelabs/wabin/wasm"

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
	index    wasm.Index
	name     string
	code     *wasm.Code
	sortName string
}

func (fn *Func) Name() string { return fn.name }

func (file *File) Close() error {
	return nil
}

func Load(path string) (*File, error) {
	obj := &File{}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	module, err := binary.DecodeModule(data, wasm.CoreFeaturesV2)
	if err != nil {
		return nil, err
	}
	obj.module = module

	tryParseDWARF(module)

	fmt.Println("LOCAL NAMES", module.NameSection.LocalNames)
	fmt.Println("FUNC NAMES", module.NameSection.FunctionNames)

	for i, fnname := range module.NameSection.FunctionNames {
		code := module.CodeSection[i]
		sym := &Func{
			obj:      obj,
			index:    fnname.Index,
			name:     fnname.Name,
			code:     code,
			sortName: strings.ToLower(fnname.Name),
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
	code := &disasm.Code{
		Name: fn.name,
	}

	// TODO: https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#binary-instr

	for i, b := range fn.code.Body {
		code.Insts = append(code.Insts, disasm.Inst{
			PC:   uint64(i),
			Text: fmt.Sprintf("BYTE 0x%0x2", b),
		})
	}
	return code
}

/*
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
*/

func tryParseDWARF(module *wasm.Module) {
	customSectionData := func(name string) []byte {
		for _, sec := range module.CustomSections {
			if sec.Name == name {
				return sec.Data
			}
		}
		return nil
	}

	dwarfdata, err := dwarf.New(
		customSectionData(".debug_abbrev"),
		customSectionData(".debug_aranges"),
		customSectionData(".debug_frame"),
		customSectionData(".debug_info"),
		customSectionData(".debug_line"),
		customSectionData(".debug_pubnames"),
		customSectionData(".debug_ranges"),
		customSectionData(".debug_str"),
	)
	if err != nil {
		fmt.Println("ERROR", err)
		return
	}

	rd := dwarfdata.Reader()
	for {
		entry, err := rd.Next()
		if entry == nil && err == nil {
			break
		}
		if err != nil {
			fmt.Println(err)
			break
		}
		if entry.Tag == dwarf.TagCompileUnit {
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
}
