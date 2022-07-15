package main

import (
	"fmt"
	"regexp"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type SymbolList struct {
	Symbols     []Symbol
	Filter      widget.Editor
	FilterError string
	Filtered    []*Symbol
	Selected    string

	List SelectList
}

func NewSymbolList(theme *material.Theme) *SymbolList {
	ui := &SymbolList{}
	ui.Filter.SingleLine = true
	ui.List = VerticalSelectList(unit.Dp(theme.TextSize) + 4)
	return ui
}

func (ui *SymbolList) SetSymbols(syms []Symbol) {
	ui.Symbols = syms
	ui.UpdateFiltered()
}

func (ui *SymbolList) UpdateFiltered() {
	defer func() {
		ui.List.Selected = -1
		for i := range ui.Filtered {
			if ui.Filtered[i].Name == ui.Selected {
				ui.List.Selected = i
				// TODO, maybe scroll into view?
				break
			}
		}
	}()

	rx, err := regexp.Compile("(?i)" + ui.Filter.Text())
	ui.FilterError = ""
	if err != nil {
		ui.FilterError = err.Error()
		return
	}

	ui.Filtered = ui.Filtered[:0]
	for i := range ui.Symbols {
		sym := &ui.Symbols[i]
		if rx.MatchString(sym.Name) {
			ui.Filtered = append(ui.Filtered, sym)
		}
	}
}

func (ui *SymbolList) Layout(th *material.Theme, gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, secondaryBackground, clip.Rect{Max: gtx.Constraints.Min}.Op())

	defer func() {
		if inRange(ui.List.Selected, len(ui.Filtered)) {
			ui.Selected = ui.Filtered[ui.List.Selected].Name
		}

		changed := false
		for _, ev := range ui.Filter.Events() {
			if _, ok := ev.(widget.ChangeEvent); ok {
				changed = true
			}
		}

		if changed {
			ui.UpdateFiltered()
			op.InvalidateOp{}.Add(gtx.Ops)
		}
	}()

	return layout.Flex{
		Axis: layout.Vertical,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return FocusBorder(th, ui.Filter.Focused()).Layout(gtx,
				material.Editor(th, &ui.Filter, "Filter (regexp)").Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if ui.FilterError == "" {
				return layout.Dimensions{}
			}
			return material.Body1(th, ui.FilterError).Layout(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return ui.List.Layout(th, gtx, len(ui.Filtered),
				StringListItem(th, &ui.List, func(index int) string {
					return ui.Filtered[index].Name
				}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			body := material.Body1(th, fmt.Sprintf("filtered %d / total %d", len(ui.Filtered), len(ui.Symbols)))
			return layout.Center.Layout(gtx, body.Layout)
		}),
	)
}

func inRange(v int, length int) bool {
	return v >= 0 && v < length
}
