package main

import (
	"testing"

	"gioui.org/widget/material"

	"loov.dev/lensm/internal/disasm"
)

type navigationTestFunc string

func (fn navigationTestFunc) Name() string { return string(fn) }
func (fn navigationTestFunc) Load(disasm.Options) *disasm.Code {
	return &disasm.Code{Name: string(fn)}
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

	ui.openFuncTab(functions[0])
	ui.openFuncTabNext(functions[1])
	ui.openFuncTabNext(functions[2])
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
