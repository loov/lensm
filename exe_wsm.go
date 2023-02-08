package main

import (
	"bytes"
	"os"
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
	sortName string
}

func (sym *WasmSymbol) Name() string { return sym.Sym.Name }

func (exe *WasmObj) Close() error {
	return nil
}

func LoadWASM(path string) (*WasmObj, error) {
	wasm := &WasmObj{}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	mod, err := wasm.DecodeModule(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	sort.SliceStable(exe.symbols, func(i, k int) bool {
		return exe.symbols[i].sortName < exe.symbols[k].sortName
	})
	for _, sym := range exe.symbols {
		exe.symbols2 = append(exe.symbols2, sym)
	}

	return exe, nil
}

func (sym *WasmObj) Load(opts Options) *Code {
	return sym.obj.LoadSymbol(sym, opts)
}

func (exe *WasmObj) LoadSymbol(sym *GoSymbol, opts Options) *Code {
	return &Code{}
}
