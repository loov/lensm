package main

import (
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
