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
//
// starts are where the function bodies start, from the code section's
// contents, as DWARF counts.
func wasmLocalNames(m *wasmir.Module, starts []uint64) map[int]map[uint32]string {
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
	for _, name := range []string{".debug_addr", ".debug_str_offsets", ".debug_line_str", ".debug_rnglists"} {
		if s := sections[name]; s != nil {
			_ = data.AddSection(name, s)
		}
	}
	funcs := map[uint64]int{}
	for i, start := range starts {
		funcs[start] = i
	}

	names := map[int]map[uint32]string{}
	r := data.Reader()
	fn, depth, fnDepth := -1, 0, 0
	lists := wasmLocLists{loc: sections[".debug_loc"], loclists: sections[".debug_loclists"], addr: sections[".debug_addr"]}
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
			lists.base, _ = e.Val(dwarf.AttrLowpc).(uint64)
			lists.addrBase, _ = e.Val(dwarf.AttrAddrBase).(int64)
			lists.listsBase, _ = e.Val(dwarf.AttrLoclistsBase).(int64)
			lists.addrSize = r.AddressSize()
		case dwarf.TagSubprogram:
			fn = -1
			if low, ok := e.Val(dwarf.AttrLowpc).(uint64); ok && low != 0 && e.Children {
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
			for _, v := range lists.locations(e.Val(dwarf.AttrLocation)) {
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

// wasmLocLists reads location lists, DWARF 4's from .debug_loc and
// DWARF 5's from .debug_loclists, for the current compilation unit.
type wasmLocLists struct {
	loc, loclists, addr []byte
	// base is the unit's base address, addrBase where its addresses
	// start in .debug_addr and listsBase its offsets in .debug_loclists.
	base                uint64
	addrBase, listsBase int64
	addrSize            int
	// read are the lists read so far, by section and offset: entries
	// can share one.
	read map[wasmListKey][]wasmVar
}

type wasmListKey struct {
	dwarf5 bool
	off    int64
}

// list reads the list at off once.
func (l *wasmLocLists) list(dwarf5 bool, off int64) []wasmVar {
	key := wasmListKey{dwarf5, off}
	if vars, ok := l.read[key]; ok {
		return vars
	}
	var vars []wasmVar
	add := func(expr []byte, lo, hi uint64) {
		if v, ok := wasmLocal(expr, lo, hi); ok {
			vars = append(vars, v)
		}
	}
	if dwarf5 {
		l.dwarf5(off, add)
	} else {
		l.dwarf4(off, add)
	}
	if l.read == nil {
		l.read = map[wasmListKey][]wasmVar{}
	}
	l.read[key] = vars
	return vars
}

// wasmLocal is the local a location expression puts a variable, or the
// first piece of it, in over [lo, hi).
func wasmLocal(expr []byte, lo, hi uint64) (wasmVar, bool) {
	// DW_OP_WASM_location 0 n: local n.
	if len(expr) < 3 || expr[0] != 0xed || expr[1] != 0 || lo >= hi {
		return wasmVar{}, false
	}
	n, k := uleb(expr[2:])
	return wasmVar{local: uint32(n), lo: lo, hi: hi}, k > 0
}

// locations are the locals a variable's location puts its value, or
// the first piece of it, in, and where: an expression holds
// everywhere, an int64 is an offset into a location list section, and
// a uint64 is an index into the unit's DWARF 5 list offsets.
func (l *wasmLocLists) locations(loc any) []wasmVar {
	switch loc := loc.(type) {
	case []byte:
		if v, ok := wasmLocal(loc, 0, ^uint64(0)); ok {
			return []wasmVar{v}
		}
	case int64:
		return l.list(l.loc == nil && l.loclists != nil, loc)
	case uint64:
		// The offsets after the lists' header are 4 bytes each, in
		// 32-bit DWARF, from listsBase.
		p := l.listsBase + 4*int64(loc)
		if loc > uint64(len(l.loclists)) || p < 0 || p+4 > int64(len(l.loclists)) {
			break
		}
		return l.list(true, l.listsBase+int64(binary.LittleEndian.Uint32(l.loclists[p:])))
	}
	return nil
}

// address reads an address of the unit's size at p in b.
func (l *wasmLocLists) address(b []byte, p int) (uint64, bool) {
	switch {
	case p < 0 || p+l.addrSize > len(b):
		return 0, false
	case l.addrSize == 8:
		return binary.LittleEndian.Uint64(b[p:]), true
	case l.addrSize == 4:
		return uint64(binary.LittleEndian.Uint32(b[p:])), true
	}
	return 0, false
}

// dwarf4 reads the .debug_loc list at off.
func (l *wasmLocLists) dwarf4(off int64, add func(expr []byte, lo, hi uint64)) {
	list, base := l.loc, l.base
	all := ^uint64(0) >> (64 - 8*min(l.addrSize, 8))
	if off < 0 || off >= int64(len(list)) {
		return
	}
	for p := int(off); ; {
		begin, ok1 := l.address(list, p)
		end, ok2 := l.address(list, p+l.addrSize)
		if !ok1 || !ok2 {
			return
		}
		p += 2 * l.addrSize
		switch {
		case begin == 0 && end == 0:
			return
		case begin == all:
			// A new base address, with no expression.
			base = end
			continue
		}
		if p+2 > len(list) {
			return
		}
		n := int(binary.LittleEndian.Uint16(list[p:]))
		p += 2
		if n > len(list)-p {
			return
		}
		add(list[p:p+n], base+begin, base+end)
		p += n
	}
}

// dwarf5 reads the .debug_loclists list at off.
func (l *wasmLocLists) dwarf5(off int64, add func(expr []byte, lo, hi uint64)) {
	list, base := l.loclists, l.base
	if off < 0 || off >= int64(len(list)) {
		return
	}
	p := int(off)
	bad := false
	num := func() uint64 {
		v, k := uleb(list[min(p, len(list)):])
		if k == 0 {
			bad = true
		}
		p += k
		return v
	}
	addr := func() uint64 {
		v, ok := l.address(list, p)
		bad = bad || !ok
		p += l.addrSize
		return v
	}
	addrx := func() uint64 {
		v, ok := l.address(l.addr, int(l.addrBase)+int(num())*l.addrSize)
		bad = bad || !ok
		return v
	}
	expr := func(lo, hi uint64) {
		n := num()
		if bad || n > uint64(len(list)-p) {
			bad = true
			return
		}
		add(list[p:p+int(n)], lo, hi)
		p += int(n)
	}
	for !bad && p < len(list) {
		kind := list[p]
		p++
		switch kind {
		case 0: // DW_LLE_end_of_list
			return
		case 1: // DW_LLE_base_addressx
			base = addrx()
		case 2: // DW_LLE_startx_endx
			lo := addrx()
			expr(lo, addrx())
		case 3: // DW_LLE_startx_length
			lo := addrx()
			expr(lo, lo+num())
		case 4: // DW_LLE_offset_pair
			lo := base + num()
			expr(lo, base+num())
		case 5: // DW_LLE_default_location
			expr(0, ^uint64(0))
		case 6: // DW_LLE_base_address
			base = addr()
		case 7: // DW_LLE_start_end
			lo := addr()
			expr(lo, addr())
		case 8: // DW_LLE_start_length
			lo := addr()
			expr(lo, lo+num())
		default:
			return
		}
	}
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
