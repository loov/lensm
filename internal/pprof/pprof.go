// Package pprof reads a pprof profile and places its samples on the
// instructions of the binary it was taken from.
package pprof

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"strings"

	"github.com/google/pprof/profile"
)

// Profile is the samples that landed on each instruction.
type Profile struct {
	// Flat is the sample value of the instructions the samples were in,
	// by their address in the binary.
	Flat map[uint64]int64
	// Total is the value of every sample, the whole for percentages.
	Total int64
	// Type names the value, such as "cpu nanoseconds".
	Type string
	// Calls are the samples whose stacks pass through each call, by the
	// address the stack holds, which lies inside the call instruction.
	// They are sorted by PC.
	Calls []Sample
	// Edges are the samples in which a function called another, by
	// the functions' names.
	Edges map[Edge]int64
	// Funcs are the samples of each function, by name.
	Funcs map[string]FuncSamples
}

// FuncSamples are the samples in one function: Flat in its own code,
// Cum also in what it called, counted once per sample.
type FuncSamples struct{ Flat, Cum int64 }

// Sample is the value of the samples at one address.
type Sample struct {
	PC    uint64
	Value int64
}

// Edge is a call from one function to another.
type Edge struct{ Caller, Callee string }

// Sum is the value of the samples in s at addresses in [lo, hi).
func Sum(s []Sample, lo, hi uint64) int64 {
	i, _ := slices.BinarySearchFunc(s, lo, func(x Sample, pc uint64) int { return cmp.Compare(x.PC, pc) })
	var total int64
	for ; i < len(s) && s[i].PC < hi; i++ {
		total += s[i].Value
	}
	return total
}

// Lookup finds the code of a function in the binary by name.
type Lookup func(name string) (addr, size uint64, ok bool)

// NameAt names the function whose code holds pc, in the binary's own
// addresses; empty when none does.
type NameAt func(pc uint64) string

// Load reads the profile at path. Its addresses are where the code ran,
// which for a position independent binary is shifted from the binary's
// own addresses, so Load works the shift out from the functions the
// samples name. A function the binary names differently than the
// profile, such as Go assembly local to its file, which the profile
// leaves without a package, takes the binary's name from nameAt.
func Load(path string, lookup Lookup, nameAt NameAt) (*Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	p, err := profile.Parse(f)
	if err != nil {
		return nil, err
	}
	if len(p.SampleType) == 0 {
		return nil, errors.New("profile has no sample types")
	}
	// The last type is the one pprof shows by default: cpu time rather
	// than the sample count, in-use space for a heap profile.
	value := len(p.SampleType) - 1
	st := p.SampleType[value]

	// Each leaf location in a known function bounds the shift: the
	// address minus the shift lies inside the function.
	lo, hi := int64(math.MinInt64), int64(math.MaxInt64)
	known := 0
	for _, loc := range p.Location {
		name := physical(loc)
		if loc.Address == 0 || name == "" {
			continue
		}
		addr, size, ok := lookup(name)
		if !ok || size == 0 {
			continue
		}
		known++
		lo = max(lo, int64(loc.Address)-int64(addr)-int64(size)+1)
		hi = min(hi, int64(loc.Address)-int64(addr))
	}
	if known == 0 || lo > hi {
		return nil, fmt.Errorf("%s was not taken from this binary", path)
	}
	shift := pickShift(lo, hi)
	physical := renamer(lookup, nameAt, shift)

	prof := &Profile{Flat: map[uint64]int64{}, Edges: map[Edge]int64{}, Funcs: map[string]FuncSamples{}, Type: st.Type + " " + st.Unit}
	seenFuncs := map[string]bool{}
	calls := map[uint64]int64{}
	// A recursive stack passes a call or an edge more than once; each
	// sample counts once for each.
	seenCalls := map[uint64]bool{}
	seenEdges := map[Edge]bool{}
	for _, s := range p.Sample {
		v := s.Value[value]
		prof.Total += v
		if len(s.Location) == 0 {
			continue
		}
		if s.Location[0].Address != 0 {
			prof.Flat[uint64(int64(s.Location[0].Address)-shift)] += v
		}
		clear(seenCalls)
		clear(seenEdges)
		clear(seenFuncs)
		if name := physical(s.Location[0]); name != "" {
			f := prof.Funcs[name]
			f.Flat += v
			prof.Funcs[name] = f
		}
		for i, loc := range s.Location {
			if name := physical(loc); name != "" && !seenFuncs[name] {
				seenFuncs[name] = true
				f := prof.Funcs[name]
				f.Cum += v
				prof.Funcs[name] = f
			}
			if i > 0 && loc.Address != 0 {
				pc := uint64(int64(loc.Address) - shift)
				if !seenCalls[pc] {
					seenCalls[pc] = true
					calls[pc] += v
				}
			}
			if i+1 < len(s.Location) {
				e := Edge{Caller: physical(s.Location[i+1]), Callee: physical(loc)}
				if e.Caller != "" && e.Callee != "" && !seenEdges[e] {
					seenEdges[e] = true
					prof.Edges[e] += v
				}
			}
		}
	}
	for pc, v := range calls {
		prof.Calls = append(prof.Calls, Sample{pc, v})
	}
	slices.SortFunc(prof.Calls, func(a, b Sample) int { return cmp.Compare(a.PC, b.PC) })
	return prof, nil
}

// physical names the function the code at loc is in. Its lines list
// the functions inlined there first and that function last.
func physical(loc *profile.Location) string {
	if len(loc.Line) == 0 || loc.Line[len(loc.Line)-1].Function == nil {
		return ""
	}
	return loc.Line[len(loc.Line)-1].Function.Name
}

// renamer returns physical, with the names the binary doesn't know
// replaced by the binary's name for the location's address. A caller's
// address is the return address, which can be the next function's
// entry after a call that doesn't return, so the address before it is
// tried too; the name that ends in the profile's wins.
func renamer(lookup Lookup, nameAt NameAt, shift int64) func(*profile.Location) string {
	names := map[*profile.Location]string{}
	return func(loc *profile.Location) string {
		if name, ok := names[loc]; ok {
			return name
		}
		name := physical(loc)
		if _, _, ok := lookup(name); !ok && name != "" && nameAt != nil && loc.Address != 0 {
			pc := uint64(int64(loc.Address) - shift)
			for _, at := range []uint64{pc, pc - 1} {
				if n := nameAt(at); n == name || strings.HasSuffix(n, "."+name) {
					name = n
					break
				}
			}
		}
		names[loc] = name
		return name
	}
}

// pickShift picks the shift in [lo, hi]: none when that fits, as for a
// binary at a fixed address, else one a page apart, as loaders place
// code at page boundaries.
func pickShift(lo, hi int64) int64 {
	if lo <= 0 && 0 <= hi {
		return 0
	}
	const page = 0x1000
	if s := (lo + page - 1) &^ (page - 1); s <= hi {
		return s
	}
	return lo
}
