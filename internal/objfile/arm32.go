package objfile

import (
	"cmp"
	"debug/elf"
	"slices"
)

// armRegion starts an ARM ($a), Thumb ($t) or data ($d) region, per the
// ELF for the Arm Architecture mapping symbols.
type armRegion struct {
	addr uint64
	kind byte // 'a', 't' or 'd'
}

type armRegions struct {
	regions []armRegion // sorted by addr
}

// armRegionsFromMapping collects the mapping symbols inside the text
// section; nil when there are none.
func armRegionsFromMapping(syms []elf.Symbol, textStart, textEnd uint64) *armRegions {
	var r armRegions
	for _, s := range syms {
		if len(s.Name) < 2 || s.Name[0] != '$' || s.Value < textStart || s.Value >= textEnd {
			continue
		}
		switch s.Name[1] {
		case 'a', 't', 'd':
			if len(s.Name) == 2 || s.Name[2] == '.' {
				r.regions = append(r.regions, armRegion{addr: s.Value, kind: s.Name[1]})
			}
		}
	}
	if len(r.regions) == 0 {
		return nil
	}
	slices.SortStableFunc(r.regions, func(x, y armRegion) int { return cmp.Compare(x.addr, y.addr) })
	return &r
}

// armRegionsFromFuncs derives regions from the Thumb bit of function
// symbols, for binaries without mapping symbols; nil when every function
// is ARM.
func armRegionsFromFuncs(syms []elf.Symbol) *armRegions {
	var r armRegions
	thumb := false
	for _, s := range syms {
		if elf.ST_TYPE(s.Info) != elf.STT_FUNC || s.Value == 0 {
			continue
		}
		kind := byte('a')
		if s.Value&1 != 0 {
			kind, thumb = 't', true
		}
		r.regions = append(r.regions, armRegion{addr: s.Value &^ 1, kind: kind})
	}
	if !thumb {
		return nil
	}
	slices.SortStableFunc(r.regions, func(x, y armRegion) int { return cmp.Compare(x.addr, y.addr) })
	return &r
}

// at returns the kind of the region containing addr and where the next
// region begins (or limit, whichever comes first). Addresses before the
// first mapping symbol are taken as ARM code.
func (r *armRegions) at(addr, limit uint64) (kind byte, end uint64) {
	kind, end = 'a', limit
	i, found := slices.BinarySearchFunc(r.regions, addr, func(s armRegion, a uint64) int {
		return cmp.Compare(s.addr, a)
	})
	if !found {
		i--
	}
	// Several mapping symbols may share an address; the last one wins.
	for i+1 < len(r.regions) && r.regions[i+1].addr == addr {
		i++
	}
	if i >= 0 {
		kind = r.regions[i].kind
	}
	if i+1 < len(r.regions) && r.regions[i+1].addr < end {
		end = r.regions[i+1].addr
	}
	return kind, end
}
