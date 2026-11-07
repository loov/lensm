package gui

import (
	"image"
	"slices"
	"strings"

	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// quickOpenLimit caps the results; nobody scrolls past the first page
// of a fuzzy search.
const quickOpenLimit = 100

// QuickOpen is a command-palette style popup that finds an item by a
// fuzzy match on its name.
type QuickOpen[T FilterListItem] struct {
	Open bool

	Editor   widget.Editor
	Results  []T
	Selected int

	all     []T
	list    widget.List
	rows    [quickOpenLimit]widget.Clickable
	scrim   int
	pattern string
}

// SetItems replaces the searchable items.
func (q *QuickOpen[T]) SetItems(all []T) {
	q.all = all
	q.search()
}

// Show opens the popup with the previous pattern selected, so typing
// replaces it.
func (q *QuickOpen[T]) Show(gtx layout.Context) {
	q.Open = true
	q.Editor.SingleLine = true
	q.Editor.Submit = true
	q.Editor.SetCaret(q.Editor.Len(), 0)
	gtx.Execute(key.FocusCmd{Tag: &q.Editor})
	q.search()
}

// Update returns the chosen item once Enter is pressed or a result
// clicked. Call it before Layout.
func (q *QuickOpen[T]) Update(gtx layout.Context) (T, bool) {
	var zero T
	if !q.Open {
		return zero, false
	}
	choose := false
	for {
		ev, ok := q.Editor.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.SubmitEvent:
			choose = true
		}
	}
	if text := q.Editor.Text(); text != q.pattern {
		q.search()
	}
	for {
		ev, ok := gtx.Event(
			key.Filter{Focus: &q.Editor, Name: key.NameUpArrow},
			key.Filter{Focus: &q.Editor, Name: key.NameDownArrow},
			key.Filter{Focus: &q.Editor, Name: key.NameEscape},
			pointer.Filter{Target: &q.scrim, Kinds: pointer.Press},
		)
		if !ok {
			break
		}
		switch ev := ev.(type) {
		case key.Event:
			if ev.State != key.Press {
				continue
			}
			switch ev.Name {
			case key.NameUpArrow:
				q.move(-1)
			case key.NameDownArrow:
				q.move(1)
			case key.NameEscape:
				q.Open = false
			}
		case pointer.Event:
			q.Open = false
		}
	}
	for i := range min(len(q.Results), len(q.rows)) {
		if q.rows[i].Clicked(gtx) {
			q.Selected, choose = i, true
		}
	}
	if choose && q.Selected < len(q.Results) {
		q.Open = false
		return q.Results[q.Selected], true
	}
	return zero, false
}

func (q *QuickOpen[T]) move(step int) {
	if len(q.Results) == 0 {
		return
	}
	q.Selected = min(max(q.Selected+step, 0), len(q.Results)-1)
	pos := &q.list.Position
	if q.Selected < pos.First {
		pos.First, pos.Offset = q.Selected, 0
	} else if q.Selected >= pos.First+pos.Count-1 && pos.Count > 0 {
		pos.First, pos.Offset = q.Selected-pos.Count+2, 0
	}
}

func (q *QuickOpen[T]) search() {
	q.pattern = q.Editor.Text()
	type match struct {
		item  T
		score int
	}
	var matches []match
	for _, item := range q.all {
		if score, ok := FuzzyScore(q.pattern, item.Name()); ok {
			matches = append(matches, match{item, score})
		}
		// With no pattern every item matches; keep the first page.
		if q.pattern == "" && len(matches) == quickOpenLimit {
			break
		}
	}
	slices.SortStableFunc(matches, func(a, b match) int { return b.score - a.score })
	q.Results = q.Results[:0]
	for _, m := range matches[:min(len(matches), quickOpenLimit)] {
		q.Results = append(q.Results, m.item)
	}
	q.Selected = 0
	q.list.Position = layout.Position{}
}

// Layout draws the popup near the top of gtx, over everything else.
func (q *QuickOpen[T]) Layout(gtx layout.Context, th *Theme, placeholder string) layout.Dimensions {
	window := gtx.Constraints.Max
	if !q.Open {
		return layout.Dimensions{Size: window}
	}
	c := th.Colors
	area := clip.Rect{Max: window}.Push(gtx.Ops)
	event.Op(gtx.Ops, &q.scrim)
	area.Pop()

	width := min(gtx.Metric.Dp(600), window.X-gtx.Metric.Dp(32))
	rowHeight := gtx.Metric.Sp(th.TextSize) + gtx.Metric.Dp(12)
	rows := min(len(q.Results), 12)

	macro := op.Record(gtx.Ops)
	gtx.Constraints = layout.Exact(image.Pt(width, 0))
	gtx.Constraints.Max.Y = window.Y
	dims := layout.UniformInset(6).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						rect := image.Rectangle{Max: gtx.Constraints.Min}
						paint.FillShape(gtx.Ops, c.Fill, clip.UniformRRect(rect, gtx.Metric.Dp(6)).Op(gtx.Ops))
						return layout.Dimensions{Size: rect.Max}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: 7, Bottom: 7, Left: 10, Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Right: 8}.Layout(gtx, iconWidget(SearchIcon, 16, c.MutedText))
								}),
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									editor := material.Editor(th.Theme, &q.Editor, placeholder)
									editor.Color = c.Text
									editor.HintColor = c.MutedText
									editor.SelectionColor = c.Selection
									return editor.Layout(gtx)
								}),
							)
						})
					},
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if rows == 0 {
					return layout.Inset{Top: 10, Bottom: 6, Left: 10}.Layout(gtx, th.Muted("No matching functions", 0.95).Layout)
				}
				gtx.Constraints.Max.Y = rows * rowHeight
				q.list.Axis = layout.Vertical
				return layout.Inset{Top: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					style := material.List(th.Theme, &q.list)
					th.StyleScrollbar(&style.ScrollbarStyle)
					style.AnchorStrategy = material.Overlay
					return style.Layout(gtx, len(q.Results), func(gtx layout.Context, i int) layout.Dimensions {
						gtx.Constraints = layout.Exact(image.Pt(gtx.Constraints.Max.X, rowHeight))
						return q.layoutRow(gtx, th, i)
					})
				})
			}),
		)
	})
	panel := macro.Stop()

	defer op.Offset(image.Pt((window.X-width)/2, gtx.Metric.Dp(52))).Push(gtx.Ops).Pop()
	Panel(gtx, th, dims.Size, gtx.Metric.Dp(10))
	// Presses on the panel's padding must not close it.
	area = clip.Rect{Max: dims.Size}.Push(gtx.Ops)
	event.Op(gtx.Ops, q)
	area.Pop()
	panel.Add(gtx.Ops)
	return layout.Dimensions{Size: window}
}

func (q *QuickOpen[T]) layoutRow(gtx layout.Context, th *Theme, i int) layout.Dimensions {
	c := th.Colors
	click := &q.rows[i]
	return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		semantic.Button.Add(gtx.Ops)
		rect := image.Rectangle{Max: gtx.Constraints.Min}
		fg, muted := c.Text, c.MutedText
		switch {
		case i == q.Selected:
			paint.FillShape(gtx.Ops, c.Accent, clip.UniformRRect(rect, gtx.Metric.Dp(5)).Op(gtx.Ops))
			fg, muted = c.AccentText, c.AccentText
		case click.Hovered():
			paint.FillShape(gtx.Ops, c.Hover, clip.UniformRRect(rect, gtx.Metric.Dp(5)).Op(gtx.Ops))
		}
		pkg, name := SplitPackage(q.Results[i].Name())
		pkg = strings.TrimSuffix(pkg, ".")
		return layout.Inset{Left: 10, Right: 10}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return Start(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						label := th.Label(name, 0.95)
						label.Color = fg
						label.MaxLines = 1
						return label.Layout(gtx)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						label := th.Label(pkg, 0.85)
						label.Color = muted
						label.MaxLines = 1
						return layout.Inset{Left: 10}.Layout(gtx, label.Layout)
					}),
				)
			})
		})
	})
}
