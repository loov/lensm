package main

import (
	"image"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/codeview"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
)

type CodeTab struct {
	Name    string
	Func    disasm.Func
	Code    codeview.UI
	Preview bool
	// LoadError is why the function failed to disassemble. It is kept
	// apart from FileUI.LoadError, which is about the file.
	LoadError error
	// loading is set while the code is disassembled in the background.
	loading bool
	Tab     widget.Clickable
	Close   widget.Clickable
}

func (ui *FileUI) activeTab() *CodeTab {
	if ui.ActiveTab < 0 || ui.ActiveTab >= len(ui.CodeTabs) {
		return nil
	}
	return ui.CodeTabs[ui.ActiveTab]
}

func (ui *FileUI) activeCode() *codeview.UI {
	tab := ui.activeTab()
	if tab == nil {
		return nil
	}
	return &tab.Code
}

// codeResult is a function disassembled for a tab off the UI thread.
type codeResult struct {
	tab  *CodeTab
	fn   disasm.Func
	code *disasm.Code
	err  error
}

// loadTabCode shows fn in tab. A large function takes a good part of a
// second to disassemble, so with an event loop running the work happens
// on a goroutine and setTabCode installs the result.
func (ui *FileUI) loadTabCode(tab *CodeTab, fn disasm.Func) {
	tab.Name = fn.Name()
	tab.Func = fn
	tab.Code = codeview.UI{}
	tab.LoadError = nil
	tab.Code.SelectedAsm = -1
	tab.Code.SelectedView = codeview.ViewGoAsm
	tab.Code.ResetScroll()

	opts := ui.loadOptions()
	if ui.codeResults == nil || ui.File == nil {
		code, err := fn.Load(opts)
		ui.setTabCode(codeResult{tab, fn, code, err})
		return
	}
	tab.loading = true
	// Hold the file open: a reload may close it while this runs.
	file := ui.File.Acquire()
	results, exited := ui.codeResults, ui.exited
	go func() {
		defer file.Close()
		code, err := fn.Load(opts)
		select {
		case results <- codeResult{tab, fn, code, err}:
		case <-exited:
		}
	}()
}

// setTabCode installs a load result unless the tab has moved on to
// another function since.
func (ui *FileUI) setTabCode(res codeResult) {
	if res.tab.Func != res.fn {
		return
	}
	res.tab.loading = false
	res.tab.Code.Code, res.tab.LoadError = res.code, res.err
}

func (ui *FileUI) appendCodeTab(fn disasm.Func) *CodeTab {
	if fn == nil {
		return nil
	}
	tab := &CodeTab{}
	ui.loadTabCode(tab, fn)
	ui.CodeTabs = append(ui.CodeTabs, tab)
	return tab
}

// previewIndex returns the index of the single preview tab, or -1.
func (ui *FileUI) previewIndex() int {
	for i, tab := range ui.CodeTabs {
		if tab.Preview {
			return i
		}
	}
	return -1
}

// previewTab shows fn in a single reusable preview tab. Browsing the function
// list (keyboard up/down or clicking a name) replaces the preview in place
// instead of piling up permanent tabs. Clicking the tab keeps it open, see
// selectTab, which clears Preview.
func (ui *FileUI) previewTab(fn disasm.Func) *CodeTab {
	if fn == nil {
		return nil
	}
	name := fn.Name()
	tab := ui.findCodeTab(name, fn)
	if tab == nil {
		if slot := ui.previewIndex(); slot >= 0 {
			tab = ui.CodeTabs[slot]
			ui.loadTabCode(tab, fn)
			ui.ActiveTab = slot
		} else {
			tab = ui.appendCodeTab(fn)
			if tab == nil {
				return nil
			}
			ui.ActiveTab = len(ui.CodeTabs) - 1
		}
		tab.Preview = true
	}
	ui.Funcs.Select(name)
	ui.Comment.Reset()
	ui.recordNavigation(name, true)
	ui.saveSessionState()
	return tab
}

// openTab opens fn in a tab and activates it. When next is true a newly
// created tab is inserted right after the active tab instead of appended
// at the end; an already-open tab is activated in place regardless.
func (ui *FileUI) openTab(fn disasm.Func, next bool) *CodeTab {
	if fn == nil {
		return nil
	}
	name := fn.Name()
	tab := ui.findCodeTab(name, fn)
	if tab == nil {
		tab = ui.appendCodeTab(fn)
		if tab == nil {
			return nil
		}
		index := len(ui.CodeTabs) - 1
		if next && 0 <= ui.ActiveTab && ui.ActiveTab < index {
			at := ui.ActiveTab + 1
			if at < index {
				copy(ui.CodeTabs[at+1:], ui.CodeTabs[at:index])
				ui.CodeTabs[at] = tab
				index = at
			}
		}
		ui.ActiveTab = index
	}
	tab.Preview = false
	ui.Funcs.Select(name)
	ui.Comment.Reset()
	ui.recordNavigation(name, false)
	ui.saveSessionState()
	return tab
}

// findCodeTab returns the open tab for name, refreshing its Func and
// making it active, or nil if no tab is open for it.
func (ui *FileUI) findCodeTab(name string, fn disasm.Func) *CodeTab {
	for i, tab := range ui.CodeTabs {
		if tab.Name == name {
			tab.Func = fn
			ui.ActiveTab = i
			return tab
		}
	}
	return nil
}

// keepActiveTab promotes the active preview tab to a permanent one, called
// when the user acts on the tab's content.
func (ui *FileUI) keepActiveTab() {
	if tab := ui.activeTab(); tab != nil && tab.Preview {
		tab.Preview = false
		ui.saveSessionState()
	}
}

// selectTab activates the tab at index and keeps it open.
func (ui *FileUI) selectTab(index int) {
	if index < 0 || index >= len(ui.CodeTabs) {
		return
	}
	ui.CodeTabs[index].Preview = false
	ui.activateTab(index)
}

// activateTab activates the tab at index, leaving a preview a preview.
func (ui *FileUI) activateTab(index int) {
	if index < 0 || index >= len(ui.CodeTabs) {
		return
	}
	ui.ActiveTab = index
	ui.Comment.Reset()
	ui.Funcs.Select(ui.CodeTabs[index].Name)
	ui.recordNavigation(ui.CodeTabs[index].Name, false)
	ui.saveSessionState()
}

func (ui *FileUI) closeTab(index int) {
	if index < 0 || index >= len(ui.CodeTabs) {
		return
	}
	ui.CodeTabs = append(ui.CodeTabs[:index], ui.CodeTabs[index+1:]...)
	switch {
	case len(ui.CodeTabs) == 0:
		ui.ActiveTab = -1
		ui.Comment.Reset()
		ui.Funcs.Selected = ""
		ui.Funcs.SelectedItem = nil
		ui.Funcs.List.Selected = -1
	case ui.ActiveTab > index:
		ui.ActiveTab--
	case ui.ActiveTab == index && ui.ActiveTab >= len(ui.CodeTabs):
		ui.ActiveTab = len(ui.CodeTabs) - 1
	}
	if tab := ui.activeTab(); tab != nil {
		ui.Funcs.Select(tab.Name)
		ui.recordNavigation(tab.Name, false)
	} else {
		ui.Comment.Reset()
	}
	ui.saveSessionState()
}

func (ui *FileUI) layoutCodeTabs(gtx layout.Context, colors gui.UIColors) layout.Dimensions {
	for i := 0; i < len(ui.CodeTabs); i++ {
		tab := ui.CodeTabs[i]
		for tab.Tab.Clicked(gtx) {
			ui.selectTab(i)
		}
		closed := false
		for tab.Close.Clicked(gtx) {
			ui.closeTab(i)
			closed = true
		}
		if closed {
			i--
		}
	}

	if len(ui.CodeTabs) == 0 {
		return layout.Dimensions{}
	}

	// 22 dp at the default 12 sp, growing with the text size.
	height := max(gtx.Metric.Sp(ui.Theme.TextSize)+gtx.Metric.Dp(10), 20)
	availableWidth := gtx.Constraints.Max.X
	gtx.Constraints = layout.Exact(image.Pt(availableWidth, height))
	paint.FillShape(gtx.Ops, colors.SecondaryBackground, clip.Rect{Max: gtx.Constraints.Max}.Op())

	tabWidth := gtx.Metric.Dp(220)
	minTabWidth := gtx.Metric.Dp(120)
	if len(ui.CodeTabs) <= 3 && availableWidth > 0 {
		tabWidth = min(tabWidth, max(minTabWidth, availableWidth/len(ui.CodeTabs)))
	}
	if tabWidth < minTabWidth {
		tabWidth = minTabWidth
	}

	list := material.List(ui.Theme.Theme, &ui.Tabs)
	list.AnchorStrategy = material.Overlay
	return list.Layout(gtx, len(ui.CodeTabs), func(gtx layout.Context, index int) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(tabWidth, height))
		return ui.layoutCodeTab(gtx, colors, ui.CodeTabs[index], index == ui.ActiveTab)
	})
}

func (ui *FileUI) layoutCodeTab(gtx layout.Context, colors gui.UIColors, tab *CodeTab, active bool) layout.Dimensions {
	size := gtx.Constraints.Max
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()

	bg := colors.SecondaryBackground
	if active {
		bg = colors.Background
	} else if tab.Tab.Hovered() {
		bg = colors.Selection
	}
	paint.FillShape(gtx.Ops, bg, clip.Rect{Max: size}.Op())
	if active {
		paint.FillShape(gtx.Ops, ui.Theme.ContrastBg, clip.Rect{Max: image.Pt(size.X, 2)}.Op())
	}
	paint.FillShape(gtx.Ops, colors.Splitter, clip.Rect{
		Min: image.Pt(size.X-1, 0),
		Max: size,
	}.Op())

	layout.Inset{Top: 1, Right: 4, Bottom: 1, Left: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints = layout.Exact(gtx.Constraints.Max)
				return tab.Tab.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					size := gtx.Constraints.Max
					defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()

					label := ui.Theme.Label(tab.Name, 0.9)
					label.MaxLines = 1
					if active {
						label.Font.Weight = font.Black
					}
					if tab.Preview {
						label.Font.Style = font.Italic
					}
					dims := layout.W.Layout(gtx, label.Layout)
					return layout.Dimensions{Size: size, Baseline: dims.Baseline}
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				closeWidth := gtx.Constraints.Max.Y
				gtx.Constraints = layout.Exact(image.Pt(closeWidth, gtx.Constraints.Max.Y))
				return tab.Close.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					size := gtx.Constraints.Max
					if tab.Close.Hovered() {
						paint.FillShape(gtx.Ops, colors.Selection, clip.Rect{Max: size}.Op())
					}
					label := ui.Theme.Muted("x", 0.9)
					label.MaxLines = 1
					dims := layout.Center.Layout(gtx, label.Layout)
					return layout.Dimensions{Size: size, Baseline: dims.Baseline}
				})
			}),
		)
	})

	return layout.Dimensions{Size: size}
}
