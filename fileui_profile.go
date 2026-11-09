package main

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"gioui.org/layout"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/pprof"
)

// loadProfile reads Config.ProfilePath for the open binary, or drops
// the profile when there is none. Profiles are small enough to read
// on the UI goroutine; move it to the loader if one is ever slow.
func (ui *FileUI) loadProfile() {
	ui.Profile, ui.ProfileFlat = nil, nil
	defer ui.saveSessionState()
	if ui.Config.ProfilePath == "" || ui.File == nil {
		return
	}
	if info, err := os.Stat(ui.Config.ProfilePath); err == nil {
		ui.profileModTime = info.ModTime()
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
	slices.SortFunc(spans, func(a, b span) int { return cmp.Compare(a.addr, b.addr) })
	// spanAt is the index into spans of the function holding pc, or -1.
	spanAt := func(pc uint64) int {
		k, _ := slices.BinarySearchFunc(spans, pc, func(s span, pc uint64) int { return cmp.Compare(s.addr, pc+1) })
		if k > 0 && pc-spans[k-1].addr < spans[k-1].size {
			return k - 1
		}
		return -1
	}
	prof, err := pprof.Load(ui.Config.ProfilePath, ui.Config.ProfileSample, func(name string) (uint64, uint64, bool) {
		s, ok := byName[name]
		return s.addr, s.size, ok
	}, func(pc uint64) string {
		if k := spanAt(pc); k >= 0 {
			return funcs[spans[k].i].Name()
		}
		return ""
	})
	if err != nil {
		ui.LoadError = fmt.Errorf("profile: %w", err)
		ui.Config.ProfilePath = ""
		return
	}
	ui.Profile = prof
	defer ui.loadBaseProfile()
	// Sum the samples per function for the summary.
	ui.ProfileFlat = make([]int64, len(funcs))
	for pc, v := range prof.Flat {
		if k := spanAt(pc); k >= 0 {
			ui.ProfileFlat[spans[k].i] += v
		}
	}
}

// loadBaseProfile reads Config.BaseProfilePath, a profile to compare
// the profile with, for the same value. It can come from another build,
// so only its function names are read.
func (ui *FileUI) loadBaseProfile() {
	ui.BaseProfile = nil
	if ui.Config.BaseProfilePath == "" || ui.Profile == nil {
		return
	}
	base, err := pprof.Load(ui.Config.BaseProfilePath, ui.Profile.Type, nil, nil)
	if err == nil && base.Type != ui.Profile.Type {
		err = fmt.Errorf("it has no %s", ui.Profile.Type)
	}
	if err != nil {
		ui.LoadError = fmt.Errorf("base profile: %w", err)
		ui.Config.BaseProfilePath = ""
		ui.saveSessionState()
		return
	}
	ui.BaseProfile = base
	ui.saveSessionState()
}

// profileChanged reports whether, while watching, the profile file
// changed and has been left alone for a moment, so it isn't read while
// it is still being written.
func (ui *FileUI) profileChanged() bool {
	if !ui.Config.Watch || ui.Profile == nil {
		return false
	}
	info, err := os.Stat(ui.Config.ProfilePath)
	if err != nil {
		return false
	}
	return !info.ModTime().Equal(ui.profileModTime) && time.Since(info.ModTime()) > 500*time.Millisecond
}

// layoutProfileStatus names the loaded profile in the status bar; a
// click picks which of its values to show.
func (ui *FileUI) layoutProfileStatus(gtx layout.Context) layout.Dimensions {
	th := ui.Theme
	prof := ui.Profile
	if i, ok := ui.ProfileMenu.Update(gtx); ok && i < len(prof.Types) && prof.Types[i] != prof.Type {
		ui.Config.ProfileSample = prof.Types[i]
		ui.loadProfile()
		if ui.Profile == nil {
			return layout.Dimensions{}
		}
		prof = ui.Profile
	}
	items := make([]gui.MenuItem, len(prof.Types))
	for i, t := range prof.Types {
		items[i] = gui.MenuItem{Label: t, Checked: t == prof.Type}
	}
	ui.ProfileMenu.Up = true
	return ui.ProfileMenu.Layout(gtx, th, items, func(gtx layout.Context, open bool) layout.Dimensions {
		text := fmt.Sprintf("%s · %s", filepath.Base(ui.Config.ProfilePath), prof.Type)
		if ui.BaseProfile != nil {
			text = fmt.Sprintf("%s − %s · %s", filepath.Base(ui.Config.ProfilePath), filepath.Base(ui.Config.BaseProfilePath), prof.Type)
		}
		if len(prof.Types) > 1 {
			text += " ▾"
		}
		return th.StatusText(text)(gtx)
	})
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
