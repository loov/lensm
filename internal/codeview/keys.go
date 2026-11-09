package codeview

import (
	"time"

	"gioui.org/io/key"
	"gioui.org/layout"
)

// navKeys move the selected instruction from the keyboard.
var navKeys = []key.Name{
	key.NameUpArrow, key.NameDownArrow,
	key.NamePageUp, key.NamePageDown,
	key.NameHome, key.NameEnd,
	key.NameLeftArrow, key.NameRightArrow,
}

// navigate handles a navigation key: the arrows step through the
// instructions, → follows a jump or call and ← returns from a jump.
func (ui Style) navigate(gtx layout.Context, c codeColumns, name key.Name) {
	insts := ui.Code.Insts
	if len(insts) == 0 {
		return
	}
	lineHeight := c.lineHeight
	viewRows := max(gtx.Constraints.Max.Y/lineHeight, 1)
	current := ui.SelectedAsm
	if current < 0 || current >= len(insts) {
		// Start from the top row in view.
		current = min(max(int(-ui.asm.Offset)/lineHeight, 0), len(insts)-1)
		if name == key.NameUpArrow || name == key.NameDownArrow {
			ui.selectAsm(gtx, c, ui.nextInst(current-1, 1), false)
			return
		}
	}
	switch name {
	case key.NameUpArrow:
		ui.selectAsm(gtx, c, ui.nextInst(current-1, -1), false)
	case key.NameDownArrow:
		ui.selectAsm(gtx, c, ui.nextInst(current+1, 1), false)
	case key.NamePageUp:
		ui.selectAsm(gtx, c, ui.nextInst(max(current-(viewRows-1), 0), 1), false)
	case key.NamePageDown:
		ui.selectAsm(gtx, c, ui.nextInst(min(current+viewRows-1, len(insts)-1), -1), false)
	case key.NameHome:
		ui.selectAsm(gtx, c, ui.nextInst(0, 1), false)
	case key.NameEnd:
		ui.selectAsm(gtx, c, ui.nextInst(len(insts)-1, -1), false)
	case key.NameRightArrow:
		ix := insts[current]
		switch {
		case ix.Call != "" && ui.TryOpen != nil:
			ui.TryOpen(gtx, ix.Call)
		case ix.RefOffset != 0:
			ui.jumpBack = append(ui.jumpBack, current)
			ui.selectAsm(gtx, c, current+ix.RefOffset, true)
		}
	case key.NameLeftArrow:
		if n := len(ui.jumpBack); n > 0 {
			from := ui.jumpBack[n-1]
			ui.jumpBack = ui.jumpBack[:n-1]
			if from < len(insts) {
				ui.selectAsm(gtx, c, from, true)
			}
		}
	}
}

// nextInst is the first instruction from i on in direction dir, past
// spacer rows, or the nearest one the other way at the ends.
func (ui Style) nextInst(i, dir int) int {
	insts := ui.Code.Insts
	for _, d := range []int{dir, -dir} {
		for j := min(max(i, 0), len(insts)-1); 0 <= j && j < len(insts); j += d {
			if insts[j].Text != "" {
				return j
			}
		}
	}
	return -1
}

// selectAsm selects instruction i and scrolls it into view. A jump
// keeps the row at the same height on screen, as a click on it does.
func (ui Style) selectAsm(gtx layout.Context, c codeColumns, i int, jump bool) {
	if i < 0 || i >= len(ui.Code.Insts) {
		return
	}
	lineHeight := c.lineHeight
	from := ui.SelectedAsm
	if ui.SelectedView != ViewNativeAsm {
		ui.SelectedView = ViewGoAsm
	}
	ui.SelectedAsm = i
	ui.SelectedFile, ui.SelectedLine = "", 0
	ui.Selection.Clear()
	ui.keyboardRow = true

	top := ui.asm.Offset + float32(i*lineHeight)
	target := ui.asm.Offset
	switch {
	case jump && from >= 0:
		target -= float32((i - from) * lineHeight)
	case top < 0:
		target -= top
	case top+float32(lineHeight) > float32(gtx.Constraints.Max.Y):
		target -= top + float32(lineHeight) - float32(gtx.Constraints.Max.Y)
	}
	if target == ui.asm.Offset {
		return
	}
	if jump {
		ui.asm.Anim.Start(gtx, ui.asm.Offset, target, 150*time.Millisecond)
		return
	}
	ui.asm.Anim.Stop()
	ui.asm.Offset = target
	ui.syncScroll(gtx, c, true)
}
