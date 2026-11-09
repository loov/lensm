// Package pprof reads a pprof profile and places its samples on the
// instructions of the binary it was taken from.
package pprof

import (
	"errors"
	"fmt"
	"math"
	"os"

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
}

// Lookup finds the code of a function in the binary by name.
type Lookup func(name string) (addr, size uint64, ok bool)

// Load reads the profile at path. Its addresses are where the code ran,
// which for a position independent binary is shifted from the binary's
// own addresses, so Load works the shift out from the functions the
// samples name.
func Load(path string, lookup Lookup) (*Profile, error) {
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
		if loc.Address == 0 || len(loc.Line) == 0 || loc.Line[len(loc.Line)-1].Function == nil {
			continue
		}
		// The last line is the function the code is in, the ones before
		// it are inlined into it.
		addr, size, ok := lookup(loc.Line[len(loc.Line)-1].Function.Name)
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

	prof := &Profile{Flat: map[uint64]int64{}, Type: st.Type + " " + st.Unit}
	for _, s := range p.Sample {
		v := s.Value[value]
		prof.Total += v
		if len(s.Location) == 0 || s.Location[0].Address == 0 {
			continue
		}
		prof.Flat[uint64(int64(s.Location[0].Address)-shift)] += v
	}
	return prof, nil
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
