package goobj

import (
	"debug/dwarf"
	"encoding/binary"

	"github.com/eliben/watgo/wasmir"
)

// wasmVar is a variable DWARF places in a local over a range of the
// code section, [lo, hi).
type wasmVar struct {
	local  uint32
	lo, hi uint64
}

// wasmLocalNames reads the names DWARF gives the locals of each
// function, by the function's index among the module's own functions.
// LLVM reuses a local for several variables, so a local gets a name
// only when DWARF puts no other variable in it. Only DWARF 4 location
// lists are read. A module without them gets none, and so does one
// whose DWARF names a function that isn't there: wasm-opt rewrites
// code without updating DWARF.
func wasmLocalNames(m *wasmir.Module, module []byte) map[int]map[uint32]string {
	sections := map[string][]byte{}
	for _, c := range m.CustomSections {
		sections[c.Name] = c.Payload
	}
	if sections[".debug_info"] == nil {
		return nil
	}
	data, err := dwarf.New(sections[".debug_abbrev"], nil, nil, sections[".debug_info"],
		nil, nil, sections[".debug_ranges"], sections[".debug_str"])
	if err != nil {
		return nil
	}
	// DWARF addresses count from the code section's contents.
	funcs := map[uint64]int{}
	for i, start := range wasmBodyStarts(module) {
		funcs[start] = i
	}

	names := map[int]map[uint32]string{}
	r := data.Reader()
	fn, depth, fnDepth := -1, 0, 0
	var cuBase uint64
	for {
		e, err := r.Next()
		if err != nil {
			return nil
		}
		if e == nil {
			break
		}
		if e.Tag == 0 {
			depth--
			if depth < fnDepth {
				fn = -1
			}
			continue
		}
		switch e.Tag {
		case dwarf.TagCompileUnit:
			cuBase, _ = e.Val(dwarf.AttrLowpc).(uint64)
		case dwarf.TagSubprogram:
			fn = -1
			if low, ok := e.Val(dwarf.AttrLowpc).(uint64); ok && low != 0 {
				i, ok := funcs[low]
				if !ok {
					return nil
				}
				fn, fnDepth = i, depth+1
			}
		case dwarf.TagVariable, dwarf.TagFormalParameter:
			n, _ := e.Val(dwarf.AttrName).(string)
			if fn < 0 || n == "" {
				break
			}
			if names[fn] == nil {
				names[fn] = map[uint32]string{}
			}
			for _, v := range wasmLocations(e.Val(dwarf.AttrLocation), sections[".debug_loc"], cuBase) {
				if old, ok := names[fn][v.local]; ok && old != n {
					n = ""
				}
				names[fn][v.local] = n
			}
		}
		if e.Children {
			depth++
		}
	}
	return names
}

// wasmLocations are the locals a variable's location puts its value, or
// the first piece of it, in, and where: an expression, which holds
// everywhere, or an offset into a DWARF 4 location list, whose
// addresses count from base.
func wasmLocations(loc any, list []byte, base uint64) []wasmVar {
	var vars []wasmVar
	add := func(expr []byte, lo, hi uint64) {
		// DW_OP_WASM_location 0 n: local n.
		if len(expr) < 3 || expr[0] != 0xed || expr[1] != 0 || lo >= hi {
			return
		}
		if n, k := uleb(expr[2:]); k > 0 {
			vars = append(vars, wasmVar{local: uint32(n), lo: lo, hi: hi})
		}
	}
	switch loc := loc.(type) {
	case []byte:
		add(loc, 0, ^uint64(0))
	case int64:
		for p := int(loc); p >= 0 && p+8 <= len(list); {
			begin := binary.LittleEndian.Uint32(list[p:])
			end := binary.LittleEndian.Uint32(list[p+4:])
			p += 8
			switch {
			case begin == 0 && end == 0:
				return vars
			case begin == ^uint32(0):
				// A new base address, with no expression.
				base = uint64(end)
				continue
			}
			if p+2 > len(list) {
				break
			}
			n := int(binary.LittleEndian.Uint16(list[p:]))
			p += 2
			if p+n > len(list) {
				break
			}
			add(list[p:p+n], base+uint64(begin), base+uint64(end))
			p += n
		}
	}
	return vars
}

// wasmBodyStarts is where each function body in the module's code
// section starts, counted from the section's contents.
func wasmBodyStarts(module []byte) (starts []uint64) {
	p := 8
	for p < len(module) {
		id := module[p]
		size, k := uleb(module[p+1:])
		if k == 0 {
			return nil
		}
		p += 1 + k
		end := p + int(size)
		if end > len(module) || end < p {
			return nil
		}
		if id != 10 {
			p = end
			continue
		}
		code := module[p:end]
		count, k := uleb(code)
		q := k
		for range count {
			n, k := uleb(code[min(q, len(code)):])
			if k == 0 {
				return nil
			}
			q += k
			starts = append(starts, uint64(q))
			q += int(n)
			if q > len(code) {
				return nil
			}
		}
		return starts
	}
	return nil
}

// uleb decodes an unsigned LEB128 number and says how many bytes it
// took, 0 when b ends first.
func uleb(b []byte) (uint64, int) {
	var v uint64
	for i, c := range b {
		if i == 10 {
			break
		}
		v |= uint64(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return v, i + 1
		}
	}
	return 0, 0
}
