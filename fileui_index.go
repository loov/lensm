package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/pprof"
)

type indexResult struct {
	file  *disasm.Shared
	index *disasm.Index
}

// startIndex scans the loaded file for callers and stats on its own
// goroutine, as it disassembles every function.
func (ui *FileUI) startIndex() {
	ui.Index = nil
	file := ui.File
	if ui.indexResults == nil {
		ui.Index = file.Index()
		return
	}
	held := file.Acquire()
	results, exited := ui.indexResults, ui.exited
	go func() {
		defer held.Close()
		index := held.Index()
		select {
		case results <- indexResult{file, index}:
		case <-exited:
		}
	}()
}

// setIndex installs a scan unless another file loaded since.
func (ui *FileUI) setIndex(res indexResult) {
	if res.file == ui.File {
		ui.Index = res.index
	}
}

// layoutCallersPanel lists the functions that call the open one.
func (ui *FileUI) layoutCallersPanel(gtx layout.Context) layout.Dimensions {
	th := ui.Theme
	tab := ui.activeTab()
	var callers []string
	if tab != nil && ui.Index != nil {
		callers = ui.Index.Callers[tab.Name]
	}
	// With a profile the callers that spend the most through the open
	// function come first.
	var sent func(caller string) int64
	if ui.Profile != nil && tab != nil {
		sent = func(caller string) int64 { return ui.Profile.Edges[pprof.Edge{Caller: caller, Callee: tab.Name}] }
		callers = slices.Clone(callers)
		slices.SortStableFunc(callers, func(a, b string) int { return cmp.Compare(sent(b), sent(a)) })
	}
	if i, ok := ui.CallersList.Clicked(gtx); ok && i < len(callers) {
		ui.openTab(ui.findFunc(callers[i]), true)
		gtx.Execute(op.InvalidateCmd{})
	}
	return gui.PanelFrame{Theme: ui.Theme, Title: "CALLERS"}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		var empty string
		switch {
		case tab == nil:
			empty = "Open a function to see what calls it."
		case ui.Index == nil && ui.File != nil:
			empty = "Looking for callers..."
		case len(callers) == 0:
			empty = "No function calls this one directly."
		}
		if empty != "" {
			return gui.PanelNote(th, empty)(gtx)
		}
		return ui.CallersList.Layout(gtx, th, len(callers), -1, func(i int) (string, string) {
			pkg, name := gui.SplitPackage(callers[i])
			detail := strings.TrimSuffix(pkg, ".")
			if sent != nil {
				if v := sent(callers[i]); v > 0 {
					detail += "  " + percentOf(v, ui.Profile.Total)
				}
			}
			return name, detail
		})
	})
}

// summaryKeys name what the summary panel sorts by: size, the marks
// in disasm.Marks order, and with a profile its samples.
func (ui *FileUI) summaryKeys() []string {
	keys := append([]string{"Size"}, markTitles[:]...)
	if ui.ProfileFlat != nil {
		keys = append(keys, "Profile")
	}
	return keys
}

// summaryValue is the stat of function i that key sorts by.
func (ui *FileUI) summaryValue(i, key int) uint64 {
	s := ui.Index.Stats[i]
	switch {
	case key == 0:
		return s.Size
	case key <= len(s.Marks):
		return uint64(s.Marks[key-1])
	case ui.ProfileFlat != nil:
		return uint64(ui.ProfileFlat[i])
	}
	return 0
}

// layoutSummaryPanel lists the functions with the most code or cost
// marks first, to point at where to look.
func (ui *FileUI) layoutSummaryPanel(gtx layout.Context) layout.Dimensions {
	th := ui.Theme
	if i, ok := ui.SummarySelect.Update(gtx); ok {
		ui.summaryKey = i
	}
	keys := ui.summaryKeys()
	if ui.summaryKey >= len(keys) {
		ui.summaryKey = 0
	}
	index := ui.Index
	profile := ui.ProfileFlat
	if index != nil && (ui.summaryFor != index || ui.summarySortedBy != ui.summaryKey || !sameSlice(ui.summaryProfile, profile)) {
		// Sort once per scan and key: a binary can have 100k functions.
		ui.summaryFor, ui.summarySortedBy, ui.summaryProfile = index, ui.summaryKey, profile
		ui.summary = ui.summary[:0]
		for i := range index.Stats {
			if ui.summaryValue(i, ui.summaryKey) > 0 {
				ui.summary = append(ui.summary, i)
			}
		}
		slices.SortStableFunc(ui.summary, func(a, b int) int {
			return cmp.Compare(ui.summaryValue(b, ui.summaryKey), ui.summaryValue(a, ui.summaryKey))
		})
	}
	funcs := ui.File.Funcs()
	if i, ok := ui.SummaryList.Clicked(gtx); ok && index != nil && i < len(ui.summary) {
		ui.openTab(funcs[ui.summary[i]], false)
		gtx.Execute(op.InvalidateCmd{})
	}
	sortBy := func(gtx layout.Context) layout.Dimensions {
		return ui.SummarySelect.Layout(gtx, th, keys, ui.summaryKey)
	}
	return gui.PanelFrame{Theme: ui.Theme, Title: "SUMMARY", Action: sortBy}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if index == nil {
			return gui.PanelNote(th, "Scanning the functions...")(gtx)
		}
		return ui.SummaryList.Layout(gtx, th, len(ui.summary), -1, func(i int) (string, string) {
			k := ui.summary[i]
			s := index.Stats[k]
			detail := fmt.Sprintf("%d B", s.Size)
			for m, n := range s.Marks {
				if n > 0 {
					detail += fmt.Sprintf("  %d %s", n, disasm.Marks[m])
				}
			}
			if profile != nil && profile[k] > 0 {
				detail += "  " + percentOf(profile[k], ui.Profile.Total)
			}
			return funcs[k].Name(), detail
		})
	})
}

// sameSlice reports whether a and b are the same slice, so a newly
// loaded profile sorts the summary again.
func sameSlice(a, b []int64) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}

// percentOf spells v as a share of total.
func percentOf(v, total int64) string {
	if total <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f%% samples", 100*float64(v)/float64(total))
}
