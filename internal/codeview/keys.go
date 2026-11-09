package codeview

import (
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
)

// navKeys move the selected row from the keyboard. Shift extends the
// selection with the vertical ones.
var navKeys = []key.Name{
	key.NameUpArrow, key.NameDownArrow,
	key.NamePageUp, key.NamePageDown,
	key.NameHome, key.NameEnd,
	key.NameLeftArrow, key.NameRightArrow,
	key.NameTab,
}

// rowNav is one column as the keyboard steps through it.
type rowNav struct {
	view   View
	n      int
	ok     func(i int) bool // a row that can be selected
	region *scrollRegion
	// current is the selected row, -1 for none.
	current int
	sel     func(i int, jump bool)
}

// asmNav steps through the instructions, past the spacer rows.
func (ui Style) asmNav(gtx layout.Context, c codeColumns) rowNav {
	view := ViewGoAsm
	if ui.SelectedView == ViewNativeAsm || !ui.ShowGo {
		view = ViewNativeAsm
	}
	current := ui.SelectedAsm
	if ui.SelectedView == ViewSource {
		current = -1
	}
	return rowNav{
		view: view, n: len(ui.Code.Insts), region: &ui.asm, current: current,
		ok:  func(i int) bool { return ui.Code.Insts[i].Text != "" },
		sel: func(i int, jump bool) { ui.selectAsm(gtx, c, view, i, jump) },
	}
}

// sourceNav steps through the source lines, past headers, gaps and notes.
func (ui Style) sourceNav(gtx layout.Context, c codeColumns) rowNav {
	var lines []bool
	for r := range sourceRows(ui.Code, ui.noteRows()) {
		lines = append(lines, r.kind == sourceRowLine)
	}
	current := -1
	if ui.SelectedView == ViewSource && ui.SelectedLine > 0 {
		current = ui.sourceLineRow(ui.SelectedFile, ui.SelectedLine)
	}
	return rowNav{
		view: ViewSource, n: len(lines), region: &ui.src, current: current,
		ok:  func(i int) bool { return lines[i] },
		sel: func(i int, _ bool) { ui.selectSource(gtx, c, i) },
	}
}

// next is the first selectable row from i on in direction dir, or the
// nearest one the other way at the ends.
func (nav rowNav) next(i, dir int) int {
	for _, d := range []int{dir, -dir} {
		for j := min(max(i, 0), nav.n-1); 0 <= j && j < nav.n; j += d {
			if nav.ok(j) {
				return j
			}
		}
	}
	return -1
}

// navigate handles a navigation key. The arrows step through the rows
// of the selected column, Tab moves to the other column, and in the
// assembly → follows a jump or call and ← returns from a jump.
func (ui Style) navigate(gtx layout.Context, c codeColumns, name key.Name, mods key.Modifiers) {
	if len(ui.Code.Insts) == 0 {
		return
	}
	inSource := ui.SelectedView == ViewSource && ui.ShowSource || !ui.ShowGo && !ui.ShowNative
	if name == key.NameTab {
		ui.switchColumn(gtx, c, inSource)
		return
	}
	nav := ui.asmNav(gtx, c)
	if inSource {
		nav = ui.sourceNav(gtx, c)
	}
	if nav.n == 0 {
		return
	}
	lineHeight := c.lineHeight
	viewRows := max(gtx.Constraints.Max.Y/lineHeight, 1)
	current := nav.current
	if current < 0 || current >= nav.n {
		// Start from the top row in view.
		current = min(max(int(-nav.region.Offset)/lineHeight, 0), nav.n-1)
		if name == key.NameUpArrow || name == key.NameDownArrow {
			nav.sel(nav.next(current, 1), false)
			return
		}
	}
	to := -1
	switch name {
	case key.NameUpArrow:
		to = nav.next(current-1, -1)
	case key.NameDownArrow:
		to = nav.next(current+1, 1)
	case key.NamePageUp:
		to = nav.next(max(current-(viewRows-1), 0), 1)
	case key.NamePageDown:
		to = nav.next(min(current+viewRows-1, nav.n-1), -1)
	case key.NameHome:
		to = nav.next(0, 1)
	case key.NameEnd:
		to = nav.next(nav.n-1, -1)
	case key.NameRightArrow:
		if inSource {
			return
		}
		ix := ui.Code.Insts[current]
		switch {
		case ix.Call != "" && ui.TryOpen != nil:
			ui.TryOpen(gtx, ix.Call)
		case ix.RefOffset != 0:
			ui.jumpBack = append(ui.jumpBack, current)
			nav.sel(current+ix.RefOffset, true)
		}
		return
	case key.NameLeftArrow:
		if n := len(ui.jumpBack); n > 0 && !inSource {
			from := ui.jumpBack[n-1]
			ui.jumpBack = ui.jumpBack[:n-1]
			if from < nav.n {
				nav.sel(from, true)
			}
		}
		return
	}
	if to < 0 {
		return
	}
	anchor := current
	if s := ui.Selection; s.Active && s.View == nav.view {
		anchor = s.Anchor
	}
	nav.sel(to, false)
	if mods.Contain(key.ModShift) {
		ui.Selection = TextSelection{View: nav.view, Anchor: anchor, Head: to, Active: true}
	}
}

// switchColumn moves the selection between the assembly and the
// source, to the counterpart of the selected row.
func (ui Style) switchColumn(gtx layout.Context, c codeColumns, inSource bool) {
	if inSource {
		if !ui.ShowGo && !ui.ShowNative {
			return
		}
		nav := ui.asmNav(gtx, c)
		for i, ix := range ui.Code.Insts {
			if ix.Text != "" && ix.File == ui.SelectedFile && ix.Line == ui.SelectedLine {
				nav.sel(i, false)
				return
			}
		}
		nav.sel(nav.next(int(-ui.asm.Offset)/c.lineHeight, 1), false)
		return
	}
	if !ui.ShowSource {
		return
	}
	nav := ui.sourceNav(gtx, c)
	if i := ui.SelectedAsm; 0 <= i && i < len(ui.Code.Insts) {
		if row := ui.sourceLineRow(ui.Code.Insts[i].File, ui.Code.Insts[i].Line); row >= 0 {
			nav.sel(row, false)
			return
		}
	}
	if nav.n > 0 {
		nav.sel(nav.next(int(-ui.src.Offset)/c.lineHeight, 1), false)
	}
}

// selectAsm selects instruction i in view and scrolls it into view. A
// jump keeps the row at the same height on screen, as a click on it
// does.
func (ui Style) selectAsm(gtx layout.Context, c codeColumns, view View, i int, jump bool) {
	if i < 0 || i >= len(ui.Code.Insts) {
		return
	}
	from := ui.SelectedAsm
	if ui.SelectedView == ViewSource {
		from = -1
	}
	ui.SelectedView = view
	ui.SelectedAsm = i
	ui.SelectedFile, ui.SelectedLine = "", 0
	ui.Selection.Clear()
	ui.keyboardRow = true
	if jump && from >= 0 {
		target := ui.asm.Offset - float32((i-from)*c.lineHeight)
		ui.asm.Anim.Start(gtx, ui.asm.Offset, target, 150*time.Millisecond)
		return
	}
	if scrollIntoView(gtx, c, &ui.asm, i) {
		ui.syncScroll(gtx, c, true)
	}
}

// selectSource selects the source line on row.
func (ui Style) selectSource(gtx layout.Context, c codeColumns, row int) {
	file, line, ok := ui.sourceRowLine(row)
	if !ok {
		return
	}
	ui.SelectedView = ViewSource
	ui.SelectedAsm = -1
	ui.SelectedFile, ui.SelectedLine = file, line
	ui.Selection.Clear()
	ui.keyboardRow = true
	if scrollIntoView(gtx, c, &ui.src, row) {
		ui.syncScroll(gtx, c, false)
	}
}

// scrollIntoView scrolls region the least that shows row and reports
// whether it moved.
func scrollIntoView(gtx layout.Context, c codeColumns, region *scrollRegion, row int) bool {
	lineHeight := c.lineHeight
	top := region.Offset + float32(row*lineHeight)
	target := region.Offset
	switch {
	case top < 0:
		target -= top
	case top+float32(lineHeight) > float32(gtx.Constraints.Max.Y):
		target -= top + float32(lineHeight) - float32(gtx.Constraints.Max.Y)
	}
	if target == region.Offset {
		return false
	}
	region.Anim.Stop()
	region.Offset = target
	return true
}
