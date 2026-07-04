package main

import (
	"testing"

	"gioui.org/widget/material"

	"loov.dev/lensm/internal/disasm"
)

type navigationTestFunc string

func (fn navigationTestFunc) Name() string { return string(fn) }
func (fn navigationTestFunc) Load(disasm.Options) (*disasm.Code, error) {
	return &disasm.Code{Name: string(fn)}, nil
}

type navigationTestFile struct{ funcs []disasm.Func }

func (file navigationTestFile) Close() error         { return nil }
func (file navigationTestFile) Funcs() []disasm.Func { return file.funcs }

func TestFileUINavigationBackAndForward(t *testing.T) {
	functions := []disasm.Func{
		navigationTestFunc("main.A"),
		navigationTestFunc("main.B"),
		navigationTestFunc("main.C"),
	}
	theme := material.NewTheme()
	ui := &FileUI{
		Theme:     theme,
		File:      navigationTestFile{funcs: functions},
		Funcs:     NewFilterList[disasm.Func](theme),
		ActiveTab: -1,
	}
	ui.Navigation.Reset()
	ui.Funcs.SetItems(functions)

	ui.openTab(functions[0], false)
	ui.openTab(functions[1], true)
	ui.openTab(functions[2], true)
	if got := ui.Navigation.Current(); got != "main.C" {
		t.Fatalf("current history entry = %q", got)
	}

	ui.navigateBack()
	if got := ui.activeTab().Name; got != "main.B" {
		t.Fatalf("active tab after Back = %q", got)
	}
	ui.navigateForward()
	if got := ui.activeTab().Name; got != "main.C" {
		t.Fatalf("active tab after Forward = %q", got)
	}
}

func TestFileUIPreviewTab(t *testing.T) {
	functions := []disasm.Func{
		navigationTestFunc("main.A"),
		navigationTestFunc("main.B"),
		navigationTestFunc("main.C"),
	}
	theme := material.NewTheme()
	ui := &FileUI{
		Theme:     theme,
		File:      navigationTestFile{funcs: functions},
		Funcs:     NewFilterList[disasm.Func](theme),
		ActiveTab: -1,
	}
	ui.Navigation.Reset()
	ui.Funcs.SetItems(functions)

	// Browsing the list only ever keeps a single preview tab.
	ui.previewTab(functions[0])
	ui.previewTab(functions[1])
	ui.previewTab(functions[2])
	if len(ui.CodeTabs) != 1 {
		t.Fatalf("preview browsing tabs = %d, want 1", len(ui.CodeTabs))
	}
	if got := ui.activeTab().Name; got != "main.C" {
		t.Fatalf("preview tab name = %q", got)
	}

	// Clicking the tab keeps it; the next preview opens a second tab.
	ui.selectTab(0)
	if ui.CodeTabs[0].Preview {
		t.Fatalf("tab still preview after selectTab")
	}
	ui.previewTab(functions[0])
	if len(ui.CodeTabs) != 2 {
		t.Fatalf("tabs after keeping = %d, want 2", len(ui.CodeTabs))
	}
}
