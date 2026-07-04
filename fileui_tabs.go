package main

import (
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"image"
	"loov.dev/lensm/internal/disasm"
)

type CodeTab struct {
	Name  string
	Func  disasm.Func
	Code  CodeUI
	Tab   widget.Clickable
	Close widget.Clickable
}

func (ui *FileUI) activeTab() *CodeTab {
	if !InRange(ui.ActiveTab, len(ui.CodeTabs)) {
		return nil
	}
	return ui.CodeTabs[ui.ActiveTab]
}

func (ui *FileUI) activeCode() *CodeUI {
	tab := ui.activeTab()
	if tab == nil {
		return nil
	}
	return &tab.Code
}

func (ui *FileUI) appendCodeTab(fn disasm.Func) *CodeTab {
	if fn == nil {
		return nil
	}
	tab := &CodeTab{
		Name: fn.Name(),
		Func: fn,
	}
	tab.Code.Code, ui.LoadError = fn.Load(ui.loadOptions())
	tab.Code.SelectedAsm = -1
	tab.Code.SelectedView = CodeViewGoAsm
	tab.Code.ResetScroll()
	ui.CodeTabs = append(ui.CodeTabs, tab)
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
		if next && InRange(ui.ActiveTab, index) {
			at := ui.ActiveTab + 1
			if at < index {
				copy(ui.CodeTabs[at+1:], ui.CodeTabs[at:index])
				ui.CodeTabs[at] = tab
				index = at
			}
		}
		ui.ActiveTab = index
	}
	ui.selectFuncByName(name)
	ui.commentKey = ""
	ui.recordNavigation(name)
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

func (ui *FileUI) selectTab(index int) {
	if !InRange(index, len(ui.CodeTabs)) {
		return
	}
	ui.ActiveTab = index
	ui.commentKey = ""
	ui.selectFuncByName(ui.CodeTabs[index].Name)
	ui.recordNavigation(ui.CodeTabs[index].Name)
	ui.saveSessionState()
}

func (ui *FileUI) closeTab(index int) {
	if !InRange(index, len(ui.CodeTabs)) {
		return
	}
	ui.CodeTabs = append(ui.CodeTabs[:index], ui.CodeTabs[index+1:]...)
	switch {
	case len(ui.CodeTabs) == 0:
		ui.ActiveTab = -1
		ui.commentKey = ""
		ui.Funcs.Selected = ""
		ui.Funcs.SelectedItem = nil
		ui.Funcs.List.Selected = -1
	case ui.ActiveTab > index:
		ui.ActiveTab--
	case ui.ActiveTab == index && ui.ActiveTab >= len(ui.CodeTabs):
		ui.ActiveTab = len(ui.CodeTabs) - 1
	}
	if tab := ui.activeTab(); tab != nil {
		ui.selectFuncByName(tab.Name)
		ui.recordNavigation(tab.Name)
	} else {
		ui.commentKey = ""
	}
	ui.saveSessionState()
}

func (ui *FileUI) layoutCodeTabs(gtx layout.Context, colors UIColors) layout.Dimensions {
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

	height := gtx.Metric.Dp(22)
	if height < 20 {
		height = 20
	}
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

	list := material.List(ui.Theme, &ui.Tabs)
	list.AnchorStrategy = material.Overlay
	return list.Layout(gtx, len(ui.CodeTabs), func(gtx layout.Context, index int) layout.Dimensions {
		gtx.Constraints = layout.Exact(image.Pt(tabWidth, height))
		return ui.layoutCodeTab(gtx, colors, ui.CodeTabs[index], index == ui.ActiveTab)
	})
}

func (ui *FileUI) layoutCodeTab(gtx layout.Context, colors UIColors, tab *CodeTab, active bool) layout.Dimensions {
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

					label := material.Body1(ui.Theme, tab.Name)
					label.MaxLines = 1
					label.TextSize = ui.Theme.TextSize * 8 / 10
					label.Color = colors.Text
					if active {
						label.Font.Weight = font.Black
					}
					dims := layout.W.Layout(gtx, label.Layout)
					return layout.Dimensions{Size: size, Baseline: dims.Baseline}
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				closeWidth := gtx.Metric.Dp(22)
				gtx.Constraints = layout.Exact(image.Pt(closeWidth, gtx.Constraints.Max.Y))
				return tab.Close.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					size := gtx.Constraints.Max
					if tab.Close.Hovered() {
						paint.FillShape(gtx.Ops, colors.Selection, clip.Rect{Max: size}.Op())
					}
					label := material.Body1(ui.Theme, "x")
					label.MaxLines = 1
					label.TextSize = ui.Theme.TextSize * 8 / 10
					label.Color = colors.MutedText
					dims := layout.Center.Layout(gtx, label.Layout)
					return layout.Dimensions{Size: size, Baseline: dims.Baseline}
				})
			}),
		)
	})

	return layout.Dimensions{Size: size}
}
