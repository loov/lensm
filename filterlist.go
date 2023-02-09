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

type FilterListItem interface {
	Name() string
}

// FilterList lists symbols for filtering and selection.
type FilterList[T FilterListItem] struct {
	All         []T
	Filter      widget.Editor
	FilterError string
	Filtered    []T

	Selected     string
	SelectedItem T

	List SelectList
}

// NewFilterList creates a new list with the specified theme.
func NewFilterList[T FilterListItem](theme *material.Theme) *FilterList[T] {
	ui := &FilterList[T]{}
	ui.Filter.SingleLine = true
	ui.List = NewVerticalSelectList(unit.Dp(theme.TextSize) + 4)
	return ui
}

// SelectIndex selects the specified item.
func (ui *FilterList[T]) SelectIndex(index int) {
	if !InRange(index, len(ui.Filtered)) {
		return
	}

	ui.List.Selected = index
	ui.Selected = ui.Filtered[index].Name()
	ui.SelectedItem = ui.Filtered[index]
}

// SetItems updates the full list.
func (ui *FilterList[T]) SetItems(all []T) {
	ui.All = all
	ui.updateFiltered()
}

// SetFilter sets the filter.
func (ui *FilterList[T]) SetFilter(filter string) {
	ui.Filter.SetText(filter)
	ui.updateFiltered()
}

// updateFiltered updates the filtered list from the unfiltered content.
func (ui *FilterList[T]) updateFiltered() {
	defer func() {
		ui.List.Selected = -1
		for i, item := range ui.Filtered {
			if item.Name() == ui.Selected {
				ui.List.Selected = i
				ui.SelectedItem = item
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
	for _, item := range ui.All {
		if rx.MatchString(item.Name()) {
			ui.Filtered = append(ui.Filtered, item)
		}
	}
}

// Layout draws the list.
func (ui *FilterList[T]) Layout(th *material.Theme, gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, secondaryBackground, clip.Rect{Max: gtx.Constraints.Min}.Op())

	defer func() {
		ui.SelectIndex(ui.List.Selected)

		changed := false
		for _, ev := range ui.Filter.Events() {
			if _, ok := ev.(widget.ChangeEvent); ok {
				changed = true
			}
		}

		if changed {
			ui.updateFiltered()
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
					return ui.Filtered[index].Name()
				}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			body := material.Body1(th, fmt.Sprintf("%d / %d", len(ui.Filtered), len(ui.All)))
			body.TextSize *= 0.8
			return layout.Center.Layout(gtx, body.Layout)
		}),
	)
}
