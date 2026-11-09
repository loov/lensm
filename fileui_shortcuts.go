package main

import (
	"runtime"

	"gioui.org/font"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/unit"
	"gioui.org/widget"

	"loov.dev/lensm/internal/gui"
)

// shortcutSheet lists the keyboard shortcuts.
type shortcutSheet struct {
	Open  bool
	Close widget.Clickable
	List  widget.List
}

type shortcutRow struct{ keys, action string }

// shortcutSections groups the shortcuts as the sheet shows them.
func shortcutSections() []struct {
	title string
	rows  []shortcutRow
} {
	shift := "Shift+"
	if runtime.GOOS == "darwin" {
		shift = "⇧"
	}
	return []struct {
		title string
		rows  []shortcutRow
	}{
		{"Anywhere", []shortcutRow{
			{shortcut("P"), "Go to a function by name or address"},
			{shortcut("F"), "Filter the functions, outside the code"},
			{shortcut("O"), "Open a binary"},
			{shortcut("R"), "Reload the binary"},
			{shortcut(","), "Settings"},
			{shortcut("B"), "Show or hide the sidebar"},
			{"Ctrl+Tab", "Next tab, with Shift the previous one"},
			{shortcut("W"), "Close the tab"},
			{altShortcut("←") + "  " + altShortcut("→"), "Back and forward between functions"},
			{"?", "This list"},
		}},
		{"In the code", []shortcutRow{
			{"↑  ↓", "Previous or next row"},
			{"Page Up  Page Down", "A screen up or down"},
			{"Home  End", "First or last row"},
			{shift + "arrows", "Extend the selection"},
			{"Tab", "To the other column, at the matching row"},
			{"→", "Follow the jump or call"},
			{"←", "Return from the followed jump"},
			{"Enter", "Comment on the row"},
			{shortcut("C"), "Copy the selection"},
			{shortcut("A"), "Select the whole column"},
			{"Escape", "Clear the selection"},
			{shortcut("F"), "Find in the function"},
			{"Right-click", "Line up the matching row in the other column"},
		}},
		{"Find", []shortcutRow{
			{"Enter  " + shortcut("G"), "Next match"},
			{shift + "Enter  " + shift + shortcut("G"), "Previous match"},
			{"Escape", "Close the find bar"},
		}},
	}
}

// layoutShortcuts draws the sheet over the window while it is open.
func (ui *FileUI) layoutShortcuts(gtx layout.Context) {
	s := &ui.Shortcuts
	if !s.Open {
		return
	}
	for s.Close.Clicked(gtx) {
		s.Open = false
	}
	for {
		ev, ok := gtx.Event(key.Filter{Name: key.NameEscape})
		if !ok {
			break
		}
		if ev, ok := ev.(key.Event); ok && ev.State == key.Press {
			s.Open = false
		}
	}
	th := ui.Theme
	sections := shortcutSections()
	var rows []layout.Widget
	for i, sec := range sections {
		top := unit16(i > 0)
		rows = append(rows, func(gtx layout.Context) layout.Dimensions {
			label := th.Muted(sec.title, 0.85)
			label.Font.Weight = font.SemiBold
			return layout.Inset{Top: top, Bottom: 6}.Layout(gtx, label.Layout)
		})
		for _, row := range sec.rows {
			rows = append(rows, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: 2, Bottom: 2}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Min.X = gtx.Metric.Dp(170)
							keys := th.Label(row.keys, 0.9)
							keys.Font.Typeface = gui.Monospace
							return keys.Layout(gtx)
						}),
						layout.Flexed(1, th.Label(row.action, 0.9).Layout),
					)
				})
			})
		}
	}
	s.List.Axis = layout.Vertical
	gui.Dialog{
		Theme: th, Width: 560, Height: 600,
		Page: func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(th.Title("Keyboard Shortcuts")),
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return s.List.Layout(gtx, len(rows), func(gtx layout.Context, i int) layout.Dimensions {
						return rows[i](gtx)
					})
				}),
			)
		},
		Footer: gui.Button{Theme: th, Clickable: &s.Close, Text: "Close", Primary: true}.Layout,
	}.Layout(gtx, s)
}

// unit16 is the gap above every section but the first.
func unit16(gap bool) unit.Dp {
	if gap {
		return 16
	}
	return 0
}

// shortcut spells a key chord the way the platform does.
func shortcut(key string) string {
	if runtime.GOOS == "darwin" {
		return "⌘" + key
	}
	return "Ctrl+" + key
}

func altShortcut(key string) string {
	if runtime.GOOS == "darwin" {
		return "⌥" + key
	}
	return "Alt+" + key
}
