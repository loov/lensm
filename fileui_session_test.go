package main

import (
	"testing"

	"gioui.org/widget/material"

	"loov.dev/lensm/internal/codeview"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
)

// TestSessionPerBinary checks that each binary gets its own tabs back,
// with the row selected in them.
func TestSessionPerBinary(t *testing.T) {
	functions := []disasm.Func{navigationTestFunc("main.A"), navigationTestFunc("main.B"), navigationTestFunc("main.C")}
	ui := &FileUI{
		Theme:     gui.NewTheme(material.NewTheme(), false),
		Funcs:     gui.NewFilterList[disasm.Func](),
		ActiveTab: -1,
	}
	ui.Navigation.Reset()

	ui.Config.Path = "/bin/a"
	ui.SetFile(navigationTestFile{funcs: functions})
	ui.openTab(functions[0], false)
	ui.openTab(functions[1], false)
	code := &ui.activeTab().Code
	code.SelectedView, code.SelectedFile, code.SelectedLine = codeview.ViewSource, "b.go", 7

	ui.Config.Path = "/bin/b"
	ui.SetFile(navigationTestFile{funcs: functions})
	ui.openTab(functions[2], false)

	ui.Config.Path = "/bin/a"
	ui.SetFile(navigationTestFile{funcs: functions})
	var names []string
	for _, tab := range ui.CodeTabs {
		names = append(names, tab.Name)
	}
	if len(names) != 2 || names[0] != "main.A" || names[1] != "main.B" || ui.activeTab().Name != "main.B" {
		t.Fatalf("tabs back in a = %q, active %q", names, ui.activeTab().Name)
	}
	s, ok := ui.Settings.session("/bin/a")
	if !ok || len(s.Tabs) != 2 || s.Tabs[1].View != codeview.ViewSource || s.Tabs[1].Line != 7 {
		t.Errorf("session of a = %+v", s)
	}
	if s, _ := ui.Settings.session("/bin/b"); len(s.Tabs) == 0 || s.Tabs[len(s.Tabs)-1].Func != "main.C" {
		t.Errorf("session of b = %+v", s)
	}
}

func TestSettingsReadOldSession(t *testing.T) {
	s := AppSettings{LastPath: "/bin/a", OpenTabs: []string{"main.A", "main.A", "main.B"}, ActiveTab: "main.B"}.normalized()
	got, ok := s.session("/bin/a")
	if !ok || len(got.Tabs) != 2 || got.Active != "main.B" || s.OpenTabs != nil {
		t.Errorf("sessions = %+v, open tabs %q", s.Sessions, s.OpenTabs)
	}
}
