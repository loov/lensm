package main

import (
	"testing"

	"gioui.org/widget/material"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
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
	theme := gui.NewTheme(material.NewTheme(), false)
	ui := &FileUI{
		Theme:     theme,
		File:      disasm.Share(navigationTestFile{funcs: functions}),
		Funcs:     gui.NewFilterList[disasm.Func](theme),
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
	theme := gui.NewTheme(material.NewTheme(), false)
	ui := &FileUI{
		Theme:     theme,
		File:      disasm.Share(navigationTestFile{funcs: functions}),
		Funcs:     gui.NewFilterList[disasm.Func](theme),
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

	// Acting on the preview content keeps it too.
	if !ui.activeTab().Preview {
		t.Fatalf("expected active tab to be preview")
	}
	ui.keepActiveTab()
	if ui.activeTab().Preview {
		t.Fatalf("keepActiveTab did not clear Preview")
	}
}

// TestFileUIReloadKeepsView checks that reloading the same binary, as
// watch mode does, keeps each tab's selection and preview state.
func TestFileUIReloadKeepsView(t *testing.T) {
	functions := []disasm.Func{navigationTestFunc("main.A"), navigationTestFunc("main.B")}
	theme := gui.NewTheme(material.NewTheme(), false)
	ui := &FileUI{
		Theme:     theme,
		Funcs:     gui.NewFilterList[disasm.Func](theme),
		ActiveTab: -1,
	}
	ui.Navigation.Reset()
	ui.SetFile(navigationTestFile{funcs: functions})
	ui.openTab(functions[0], false)
	ui.previewTab(functions[1])
	ui.activeTab().Code.SelectedAsm = 5

	ui.SetFile(navigationTestFile{funcs: functions})
	if len(ui.CodeTabs) != 2 {
		t.Fatalf("%d tabs after reload, want 2", len(ui.CodeTabs))
	}
	tab := ui.activeTab()
	if tab.Name != "main.B" || !tab.Preview || tab.Code.SelectedAsm != 5 {
		t.Fatalf("active tab after reload = %s preview=%v selected=%d", tab.Name, tab.Preview, tab.Code.SelectedAsm)
	}
}
