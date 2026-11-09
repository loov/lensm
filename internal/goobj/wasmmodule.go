package goobj

import (
	"strings"

	"github.com/eliben/watgo"
	"github.com/eliben/watgo/wasmir"
)

// wasmInfo is what the statements need from the whole module: the
// types of calls and blocks, and the names of locals and globals,
// which the listing of one function doesn't carry. Function bodies are
// decoded one at a time, since the whole module decoded holds about a
// hundred times its size in memory.
type wasmInfo struct {
	// m is the module without its bodies, data and DWARF.
	m    *wasmir.Module
	data []byte
	// types is the module's type section, header included, and bodies
	// where each function body is in data.
	types  []byte
	bodies []wasmSpan

	// funcTypes is the type of each function, the imported first, and
	// imported how many are.
	funcTypes []wasmir.TypeDef
	imported  int
	globals   []string
	goWasm    bool
	// locals are the names DWARF gives each function's locals.
	locals map[int]map[uint32]string
}

type wasmSpan struct{ start, end int }

// loadWasmInfo reads a core module. It is nil for a component or a
// module it can't read.
func loadWasmInfo(data []byte) (info *wasmInfo) {
	defer func() {
		// A malformed module loses its statements, not its listing.
		if recover() != nil {
			info = nil
		}
	}()
	// A core module is version 1; a component has another version.
	if len(data) < 8 || string(data[:8]) != "\x00asm\x01\x00\x00\x00" {
		return nil
	}
	info = &wasmInfo{data: data}
	stripped := []byte(string(data[:8]))
	codeStart := 0
	for p := 8; p < len(data); {
		size, k := uleb(data[p+1:])
		start := p + 1 + k
		end := start + int(size)
		if k == 0 || end > len(data) || end < start {
			return nil
		}
		switch data[p] {
		case 1:
			info.types = data[p:end]
		case 10:
			codeStart = start
			if info.bodies = wasmBodies(data, start, end); info.bodies == nil {
				return nil
			}
			// Each body empty: 0 locals, end.
			code := appendUleb(nil, uint64(len(info.bodies)))
			for range info.bodies {
				code = append(code, 2, 0, 0x0b)
			}
			stripped = append(stripped, 10)
			stripped = appendUleb(stripped, uint64(len(code)))
			stripped = append(stripped, code...)
			p = end
			continue
		case 11, 12:
			// The data and its count: nothing the statements need.
			p = end
			continue
		}
		stripped = append(stripped, data[p:end]...)
		p = end
	}
	m, err := watgo.DecodeWASM(stripped)
	if err != nil {
		return nil
	}
	info.m = m
	info.funcTypes = wasmFuncTypes(m)
	info.imported = len(info.funcTypes) - len(m.Funcs)
	info.goWasm = isGoWasm(m)
	info.globals = wasmGlobalNames(m)
	if info.goWasm {
		for i, name := range goWasmGlobals {
			if i < len(info.globals) && info.globals[i] == "" {
				info.globals[i] = name
			}
		}
	}
	// DWARF addresses count from the code section's contents.
	starts := make([]uint64, len(info.bodies))
	for i, b := range info.bodies {
		starts[i] = uint64(b.start - codeStart)
	}
	info.locals = wasmLocalNames(m, starts)
	m.CustomSections = nil
	return info
}

// body decodes the body of the k-th of the module's own functions,
// wrapped in a module of its own with the module's types.
func (info *wasmInfo) body(k int) []wasmir.Instruction {
	if k < 0 || k >= len(info.bodies) || info.types == nil {
		return nil
	}
	b := info.bodies[k]
	mod := []byte("\x00asm\x01\x00\x00\x00")
	mod = append(mod, info.types...)
	mod = append(mod, 3, 2, 1, 0) // function section: [type 0]
	entry := appendUleb([]byte{1}, uint64(b.end-b.start))
	entry = append(entry, info.data[b.start:b.end]...)
	mod = append(mod, 10)
	mod = appendUleb(mod, uint64(len(entry)))
	mod = append(mod, entry...)
	m, err := watgo.DecodeWASM(mod)
	if err != nil || len(m.Funcs) != 1 {
		return nil
	}
	return m.Funcs[0].Body
}

// wasmBodies is where each body of the code section data[start:end] is.
func wasmBodies(data []byte, start, end int) []wasmSpan {
	code := data[start:end]
	count, k := uleb(code)
	if k == 0 {
		return nil
	}
	q := k
	bodies := []wasmSpan{}
	for range count {
		n, k := uleb(code[min(q, len(code)):])
		if k == 0 {
			return nil
		}
		q += k
		if int(n) > len(code)-q {
			return nil
		}
		bodies = append(bodies, wasmSpan{start + q, start + q + int(n)})
		q += int(n)
	}
	return bodies
}

// appendUleb appends v as an unsigned LEB128.
func appendUleb(dst []byte, v uint64) []byte {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			c |= 0x80
		}
		dst = append(dst, c)
		if v == 0 {
			return dst
		}
	}
}

// wasmFuncTypes is the type of each function in the module's index
// space, the imported ones first.
func wasmFuncTypes(m *wasmir.Module) []wasmir.TypeDef {
	var types []wasmir.TypeDef
	for _, imp := range m.Imports {
		if imp.Kind == wasmir.ExternalKindFunction {
			types = append(types, typeOf(m, imp.TypeIdx))
		}
	}
	for _, f := range m.Funcs {
		types = append(types, typeOf(m, f.TypeIdx))
	}
	return types
}

// wasmGlobalNames is the name of each global in the module's index
// space, empty for one without.
func wasmGlobalNames(m *wasmir.Module) []string {
	var names []string
	for _, imp := range m.Imports {
		if imp.Kind == wasmir.ExternalKindGlobal {
			names = append(names, imp.Name)
		}
	}
	for _, g := range m.Globals {
		names = append(names, strings.TrimPrefix(g.Name, "$"))
	}
	return names
}

// goWasmGlobals are the globals the Go linker gives every wasm module,
// in order, without naming them in the module.
var goWasmGlobals = []string{"SP", "CTXT", "g", "RET0", "RET1", "RET2", "RET3", "PAUSE"}

// isGoWasm reports whether the Go toolchain built the module, which
// marks it with its build ID.
func isGoWasm(m *wasmir.Module) bool {
	for _, c := range m.CustomSections {
		if c.Name == "go:buildid" {
			return true
		}
	}
	return false
}
