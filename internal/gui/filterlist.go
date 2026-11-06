package gui

import (
	"fmt"
	"regexp"
	"strings"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type FilterListItem interface {
	Name() string
}

// FilterList lists symbols for filtering and selection.
type FilterList[T FilterListItem] struct {
	All []T
	// lower holds the names of All in lower case, for plain filters.
	lower       []string
	Filter      widget.Editor
	FilterError string
	Filtered    []T

	Selected     string
	SelectedItem T

	List SelectList
}

// NewFilterList creates a new list with the specified theme.
func NewFilterList[T FilterListItem](theme *Theme) *FilterList[T] {
	ui := &FilterList[T]{}
	ui.Filter.SingleLine = true
	ui.List = NewVerticalSelectList()
	return ui
}

// SelectIndex selects the specified item.
func (ui *FilterList[T]) SelectIndex(index int) {
	if index < 0 || index >= len(ui.Filtered) {
		return
	}

	ui.List.Selected = index
	ui.Selected = ui.Filtered[index].Name()
	ui.SelectedItem = ui.Filtered[index]
}

// Select selects the item called name. It stays selected while the
// filter hides it, but has no row in the list then.
func (ui *FilterList[T]) Select(name string) {
	ui.Selected = name
	ui.List.Selected = -1
	var zero T
	ui.SelectedItem = zero
	for i, item := range ui.Filtered {
		if item.Name() == name {
			ui.List.Selected = i
			ui.SelectedItem = item
			return
		}
	}
	for _, item := range ui.All {
		if item.Name() == name {
			ui.SelectedItem = item
			return
		}
	}
}

// SetItems updates the full list.
func (ui *FilterList[T]) SetItems(all []T) {
	ui.All = all
	ui.lower = make([]string, len(all))
	for i, item := range all {
		ui.lower[i] = strings.ToLower(item.Name())
	}
	ui.updateFiltered()
	if ui.List.Selected == -1 && len(ui.Filtered) > 0 {
		ui.SelectIndex(0)
	}
}

// SetFilter sets the filter.
func (ui *FilterList[T]) SetFilter(filter string) {
	ui.Filter.SetText(filter)
	ui.updateFiltered()
}

// updateFiltered updates the filtered list from the unfiltered content.
func (ui *FilterList[T]) updateFiltered() {
	defer ui.Select(ui.Selected)

	filter := ui.Filter.Text()
	ui.FilterError = ""
	// Most filters are plain text. A substring search over names
	// lowercased once is about 13x faster than (?i) matching, which ran
	// 60 ms per keystroke on a binary with 66k functions.
	if regexp.QuoteMeta(filter) == filter {
		filter = strings.ToLower(filter)
		ui.Filtered = ui.Filtered[:0]
		for i, item := range ui.All {
			if strings.Contains(ui.lower[i], filter) {
				ui.Filtered = append(ui.Filtered, item)
			}
		}
		return
	}

	rx, err := regexp.Compile("(?i)" + filter)
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
func (ui *FilterList[T]) Layout(th *Theme, gtx layout.Context) layout.Dimensions {
	paint.FillShape(gtx.Ops, th.Colors.SecondaryBackground, clip.Rect{Max: gtx.Constraints.Min}.Op())

	ui.SelectIndex(ui.List.Selected)

	changed := false
	for {
		ev, ok := ui.Filter.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.ChangeEvent); ok {
			changed = true
		}
	}

	if changed {
		ui.updateFiltered()
		gtx.Execute(op.InvalidateCmd{})
	}

	return layout.Flex{
		Axis: layout.Vertical,
	}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return FocusBorder(th.Theme, gtx.Focused(&ui.Filter)).Layout(gtx,
				material.Editor(th.Theme, &ui.Filter, "Filter (regexp)").Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if ui.FilterError == "" {
				return layout.Dimensions{}
			}
			return th.ErrorLabel(ui.FilterError, 1).Layout(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return ui.List.Layout(th.Theme, gtx, len(ui.Filtered),
				StringListItem(th.Theme, &ui.List, func(index int) string {
					return ui.Filtered[index].Name()
				}))
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			body := th.Label(fmt.Sprintf("%d / %d", len(ui.Filtered), len(ui.All)), 0.9)
			return layout.Center.Layout(gtx, body.Layout)
		}),
	)
}
