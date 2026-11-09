package main

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/pprof"
)

// loadProfile reads Config.ProfilePath for the open binary, or drops
// the profile when there is none. Profiles are small enough to read
// on the UI goroutine; move it to the loader if one is ever slow.
func (ui *FileUI) loadProfile() {
	ui.Profile, ui.ProfileFlat = nil, nil
	if ui.Config.ProfilePath == "" || ui.File == nil {
		return
	}
	funcs := ui.File.Funcs()
	type span struct {
		addr, size uint64
		i          int
	}
	byName := map[string]span{}
	var spans []span
	for i, fn := range funcs {
		r, ok := fn.(disasm.Ranged)
		if !ok {
			continue
		}
		addr, size := r.Range()
		if size == 0 {
			continue
		}
		s := span{addr, size, i}
		if _, dup := byName[fn.Name()]; !dup {
			byName[fn.Name()] = s
		}
		spans = append(spans, s)
	}
	prof, err := pprof.Load(ui.Config.ProfilePath, func(name string) (uint64, uint64, bool) {
		s, ok := byName[name]
		return s.addr, s.size, ok
	})
	if err != nil {
		ui.LoadError = fmt.Errorf("profile: %w", err)
		ui.Config.ProfilePath = ""
		return
	}
	ui.Profile = prof
	// Sum the samples per function for the summary.
	slices.SortFunc(spans, func(a, b span) int { return cmp.Compare(a.addr, b.addr) })
	ui.ProfileFlat = make([]int64, len(funcs))
	for pc, v := range prof.Flat {
		k, _ := slices.BinarySearchFunc(spans, pc, func(s span, pc uint64) int { return cmp.Compare(s.addr, pc+1) })
		if k > 0 && pc-spans[k-1].addr < spans[k-1].size {
			ui.ProfileFlat[spans[k-1].i] += v
		}
	}
}

// profileStatus names the loaded profile for the status bar.
func (ui *FileUI) profileStatus() string {
	if ui.Profile == nil {
		return ""
	}
	return fmt.Sprintf("%s · %s", filepath.Base(ui.Config.ProfilePath), ui.Profile.Type)
}

func profileFlat(p *pprof.Profile) map[uint64]int64 {
	if p == nil {
		return nil
	}
	return p.Flat
}

func profileCalls(p *pprof.Profile) []pprof.Sample {
	if p == nil {
		return nil
	}
	return p.Calls
}

func profileTotal(p *pprof.Profile) int64 {
	if p == nil {
		return 0
	}
	return p.Total
}
