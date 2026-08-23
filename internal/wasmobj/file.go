package wasmobj

import (
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

	funcs []disasm.Func
}

func (file *File) Funcs() []disasm.Func { return file.funcs }

// Func contains information about the executable.
type Func struct {
	obj   *File
	index wasm.Index
	name  string
	code  *wasm.Code
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

	for i, fnname := range module.NameSection.FunctionNames {
		code := module.CodeSection[i]
		sym := &Func{
			obj:   obj,
			index: fnname.Index,
			name:  fnname.Name,
			code:  code,
		}
		obj.funcs = append(obj.funcs, sym)
	}

	sort.SliceStable(obj.funcs, func(i, k int) bool {
		return strings.ToLower(obj.funcs[i].Name()) < strings.ToLower(obj.funcs[k].Name())
	})

	return obj, nil
}

func (fn *Func) Load(opts disasm.Options) (*disasm.Code, error) {
	return fn.obj.LoadCode(fn, opts), nil
}

func (file *File) LoadCode(fn *Func, opts disasm.Options) *disasm.Code {
	code := &disasm.Code{
		Name: fn.name,
	}

	// TODO: https://www.w3.org/TR/2019/REC-wasm-core-1-20191205/#binary-instr

	for i, b := range fn.code.Body {
		code.Insts = append(code.Insts, disasm.Inst{
			PC:   uint64(i),
			Text: fmt.Sprintf("BYTE 0x%02x", b),
		})
	}
	return code
}
