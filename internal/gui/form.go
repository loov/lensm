package gui

import (
	"image"
	"image/color"

	"gioui.org/font"
	"gioui.org/io/semantic"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
)

// Segment is one toggle of a Segmented control.
type Segment struct {
	Label string
	Value *widget.Bool
}

// Segmented is a row of independent toggles drawn as one control.
type Segmented struct {
	Theme    *Theme
	Segments []Segment
}

func (s Segmented) Layout(gtx layout.Context) layout.Dimensions {
	c := s.Theme.Colors
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			rect := image.Rectangle{Max: gtx.Constraints.Min}
			paint.FillShape(gtx.Ops, c.Fill, clip.UniformRRect(rect, gtx.Metric.Dp(7)).Op(gtx.Ops))
			return layout.Dimensions{Size: rect.Max}
		},
		func(gtx layout.Context) layout.Dimensions {
			children := make([]layout.FlexChild, len(s.Segments))
			for i, seg := range s.Segments {
				children[i] = layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return seg.Value.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						semantic.Switch.Add(gtx.Ops)
						label := s.Theme.Label(seg.Label, 0.9)
						label.MaxLines = 1
						if !seg.Value.Value {
							label.Color = c.MutedText
						}
						return layout.Background{}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								if seg.Value.Value {
									rect := image.Rectangle{Max: gtx.Constraints.Min}
									r := gtx.Metric.Dp(5)
									paint.FillShape(gtx.Ops, c.Shadow, clip.UniformRRect(rect.Add(image.Pt(0, gtx.Metric.Dp(1))), r).Op(gtx.Ops))
									paint.FillShape(gtx.Ops, c.Control, clip.UniformRRect(rect, r).Op(gtx.Ops))
								}
								return layout.Dimensions{Size: gtx.Constraints.Min}
							},
							func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: 3, Bottom: 3, Left: 10, Right: 10}.Layout(gtx, label.Layout)
							},
						)
					})
				})
			}
			return layout.UniformInset(2).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{}.Layout(gtx, children...)
			})
		},
	)
}

// SearchButton looks like a search field and opens a search when
// clicked, for example a command palette.
type SearchButton struct {
	Theme       *Theme
	Clickable   *widget.Clickable
	Placeholder string
	Shortcut    string
}

func (b SearchButton) Layout(gtx layout.Context) layout.Dimensions {
	c := b.Theme.Colors
	gtx.Constraints.Min.X = min(gtx.Metric.Dp(380), gtx.Constraints.Max.X)
	gtx.Constraints.Max.X = gtx.Constraints.Min.X
	return b.Clickable.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		semantic.Button.Add(gtx.Ops)
		semantic.DescriptionOp(b.Placeholder).Add(gtx.Ops)
		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				rect := image.Rectangle{Max: gtx.Constraints.Min}
				r := gtx.Metric.Dp(7)
				paint.FillShape(gtx.Ops, c.Fill, clip.UniformRRect(rect, r).Op(gtx.Ops))
				if b.Clickable.Hovered() {
					paint.FillShape(gtx.Ops, c.Hover, clip.UniformRRect(rect, r).Op(gtx.Ops))
				}
				return layout.Dimensions{Size: rect.Max}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 5, Bottom: 5, Left: 10, Right: 8}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Right: 8}.Layout(gtx, iconWidget(SearchIcon, 14, c.MutedText))
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							label := b.Theme.Muted(b.Placeholder, 0.92)
							label.MaxLines = 1
							return label.Layout(gtx)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if b.Shortcut == "" {
								return layout.Dimensions{}
							}
							return Keycap(b.Theme, b.Shortcut)(gtx)
						}),
					)
				})
			},
		)
	})
}

// Keycap draws a keyboard shortcut in a small outlined box.
func Keycap(th *Theme, text string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		label := th.Muted(text, 0.8)
		return frame(gtx, th.Colors.Background, th.Colors.Splitter, layout.Inset{Left: 5, Right: 5}, label.Layout)
	}
}

// iconWidget draws icon as a square of size dp.
func iconWidget(icon *widget.Icon, size int, c color.NRGBA) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		s := gtx.Dp(unit.Dp(size))
		gtx.Constraints = layout.Exact(image.Pt(s, s))
		return icon.Layout(gtx, c)
	}
}

// Checkbox is a flat check box with a label.
type Checkbox struct {
	Theme *Theme
	Value *widget.Bool
	Label string
}

func (b Checkbox) Layout(gtx layout.Context) layout.Dimensions {
	c := b.Theme.Colors
	return b.Value.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		semantic.CheckBox.Add(gtx.Ops)
		return layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					s := gtx.Metric.Dp(16)
					rect := image.Rectangle{Max: image.Pt(s, s)}
					r := gtx.Metric.Dp(4)
					if b.Value.Value {
						paint.FillShape(gtx.Ops, c.Accent, clip.UniformRRect(rect, r).Op(gtx.Ops))
						inset := gtx.Metric.Dp(2)
						trans := op.Offset(image.Pt(inset, inset)).Push(gtx.Ops)
						gtx.Constraints = layout.Exact(image.Pt(s-2*inset, s-2*inset))
						CheckIcon.Layout(gtx, c.AccentText)
						trans.Pop()
					} else {
						paint.FillShape(gtx.Ops, c.MutedText, clip.UniformRRect(rect, r).Op(gtx.Ops))
						b := gtx.Metric.Dp(1)
						paint.FillShape(gtx.Ops, c.Background, clip.UniformRRect(rect.Inset(b), r-b).Op(gtx.Ops))
					}
					return layout.Dimensions{Size: rect.Max}
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: 8}.Layout(gtx, b.Theme.Label(b.Label, 0.95).Layout)
				}),
			)
		})
	})
}

// Select picks one of several options from a popup menu.
type Select struct {
	Menu Menu
}

// Update returns the index of a newly chosen option.
func (s *Select) Update(gtx layout.Context) (int, bool) {
	return s.Menu.Update(gtx)
}

func (s *Select) Layout(gtx layout.Context, th *Theme, options []string, current int) layout.Dimensions {
	c := th.Colors
	items := make([]MenuItem, len(options))
	for i, option := range options {
		items[i] = MenuItem{Label: option, Checked: i == current}
	}
	return s.Menu.Layout(gtx, th, items, func(gtx layout.Context, open bool) layout.Dimensions {
		bg := c.Control
		if open {
			bg = c.Fill
		}
		gtx.Constraints.Min.X = gtx.Metric.Dp(170)
		return frame(gtx, bg, c.Splitter, layout.Inset{Top: 3, Bottom: 3, Left: 9, Right: 4}, func(gtx layout.Context) layout.Dimensions {
			label := ""
			if current >= 0 && current < len(options) {
				label = options[current]
			}
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, th.Label(label, 0.95).Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					s := gtx.Metric.Dp(16)
					paint.FillShape(gtx.Ops, c.Accent, clip.UniformRRect(image.Rectangle{Max: image.Pt(s, s)}, gtx.Metric.Dp(4)).Op(gtx.Ops))
					gtx.Constraints = layout.Exact(image.Pt(s, s))
					return ExpandMoreIcon.Layout(gtx, c.AccentText)
				}),
			)
		})
	})
}

// FormRow puts a label in a fixed column so the controls line up.
func FormRow(th *Theme, label string, w layout.Widget) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Metric.Dp(150)
					return th.Label(label, 0.95).Layout(gtx)
				}),
				layout.Rigid(w),
			)
		})
	}
}

// FormHeading is a muted group title followed by a line.
func FormHeading(th *Theme, title string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: 18, Bottom: 6}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					label := th.Muted(title, 0.9)
					label.Font.Weight = font.SemiBold
					return layout.Inset{Right: 8}.Layout(gtx, label.Layout)
				}),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					return HorizontalLine{Height: 1, Color: th.Colors.Divider}.Layout(gtx)
				}),
			)
		})
	}
}

// FormHint is muted help text under a control.
func FormHint(th *Theme, text string) layout.Widget {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Left: 24, Bottom: 4}.Layout(gtx, th.Muted(text, 0.88).Layout)
	}
}
