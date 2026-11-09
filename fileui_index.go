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
	if i, ok := ui.CallersList.Clicked(gtx); ok && i < len(callers) {
		ui.openTab(ui.findFunc(callers[i]), true)
		gtx.Execute(op.InvalidateCmd{})
	}
	return ui.panelFrame(gtx, "CALLERS", nil, func(gtx layout.Context) layout.Dimensions {
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
			return layout.Inset{Left: 16, Right: 16}.Layout(gtx, th.Muted(empty, 0.9).Layout)
		}
		return ui.CallersList.Layout(gtx, th, len(callers), -1, func(i int) (string, string) {
			pkg, name := gui.SplitPackage(callers[i])
			return name, strings.TrimSuffix(pkg, ".")
		})
	})
}

// summaryKeys name what the summary panel sorts by: size, then the
// marks in disasm.Marks order.
var summaryKeys = append([]string{"Size"}, markTitles[:]...)

// summaryValue is the stat of s that key sorts by.
func summaryValue(s disasm.FuncStats, key int) uint64 {
	if key == 0 {
		return s.Size
	}
	return uint64(s.Marks[key-1])
}

// layoutSummaryPanel lists the functions with the most code or cost
// marks first, to point at where to look.
func (ui *FileUI) layoutSummaryPanel(gtx layout.Context) layout.Dimensions {
	th := ui.Theme
	if i, ok := ui.SummarySelect.Update(gtx); ok {
		ui.summaryKey = i
	}
	index := ui.Index
	if index != nil && (ui.summaryFor != index || ui.summarySortedBy != ui.summaryKey) {
		// Sort once per scan and key: a binary can have 100k functions.
		ui.summaryFor, ui.summarySortedBy = index, ui.summaryKey
		ui.summary = ui.summary[:0]
		for i, s := range index.Stats {
			if summaryValue(s, ui.summaryKey) > 0 {
				ui.summary = append(ui.summary, i)
			}
		}
		slices.SortStableFunc(ui.summary, func(a, b int) int {
			return cmp.Compare(summaryValue(index.Stats[b], ui.summaryKey), summaryValue(index.Stats[a], ui.summaryKey))
		})
	}
	funcs := ui.File.Funcs()
	if i, ok := ui.SummaryList.Clicked(gtx); ok && index != nil && i < len(ui.summary) {
		ui.openTab(funcs[ui.summary[i]], false)
		gtx.Execute(op.InvalidateCmd{})
	}
	sortBy := func(gtx layout.Context) layout.Dimensions {
		return ui.SummarySelect.Layout(gtx, th, summaryKeys, ui.summaryKey)
	}
	return ui.panelFrame(gtx, "SUMMARY", sortBy, func(gtx layout.Context) layout.Dimensions {
		if index == nil {
			return layout.Inset{Left: 16, Right: 16}.Layout(gtx, th.Muted("Scanning the functions...", 0.9).Layout)
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
			return funcs[k].Name(), detail
		})
	})
}
