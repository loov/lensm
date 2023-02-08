package main

import (
	"bytes"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/go-interpreter/wagon/wasm"
)

var _ Obj = (*WasmObj)(nil)
var _ Symbol = (*WasmSymbol)(nil)

// WasmObj contains information about the object file.
type WasmObj struct {
	symbols  []*WasmSymbol
	symbols2 []Symbol
}

func (exe *WasmObj) Symbols() []Symbol { return exe.symbols2 }

// WasmSymbol contains information about the executable.
type WasmSymbol struct {
	obj      *WasmObj
	name     string
	sortName string
}

func (sym *WasmSymbol) Name() string { return sym.name }

func (exe *WasmObj) Close() error {
	return nil
}

func LoadWASM(path string) (*WasmObj, error) {
	obj := &WasmObj{}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	mod, err := wasm.ReadModule(bytes.NewReader(data),
		func(name string) (*wasm.Module, error) {
			return nil, fmt.Errorf("not found %q", name)
		})
	if err != nil {
		return nil, err
	}

	for _, fn := range mod.FunctionIndexSpace {
		sym := &WasmSymbol{
			obj:      obj,
			name:     fn.Name,
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
	return &Code{}
}
