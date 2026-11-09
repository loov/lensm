package codeview

import (
	"fmt"
	"image"
	"math"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
)

// handleInput processes pointer and keyboard events for the frame,
// updating selection and scroll state, and reports whether the release
// was a click (as opposed to a drag).
func (ui Style) handleInput(gtx layout.Context, c codeColumns) (mouseClicked bool) {
	lineHeight := c.lineHeight
	event.Op(gtx.Ops, ui.UI)
	selectionAt := func(position f32.Point) (View, int, bool) {
		inGo := c.asm.Contains(position.X)
		if inGo || ui.ShowNative && c.native.Contains(position.X) {
			view := ViewNativeAsm
			if inGo {
				view = ViewGoAsm
			}
			relative := position.Y - ui.asm.Offset
			if relative < 0 {
				return ViewNone, -1, false
			}
			line := int(relative) / lineHeight
			return view, line, line < len(ui.Code.Insts)
		}
		if c.source.Contains(position.X) {
			line := sourceRowAtY(ui.Code, ui.noteRows(), ui.src.Offset, lineHeight, position.Y)
			return ViewSource, line, line >= 0
		}
		return ViewNone, -1, false
	}
	// selectionDragLine clamps a drag position to the selection view's
	// content, so a fast drag past an edge selects through to the first
	// or last line instead of stopping at the last in-range sample.
	selectionDragLine := func(view View, position f32.Point) (int, bool) {
		var line, count int
		switch view {
		case ViewGoAsm, ViewNativeAsm:
			count = len(ui.Code.Insts)
			line = int(position.Y-ui.asm.Offset) / lineHeight
		case ViewSource:
			count = sourceRowCount(ui.Code, ui.noteRows())
			line = int(position.Y-ui.src.Offset) / lineHeight
		default:
			return -1, false
		}
		if count == 0 {
			return -1, false
		}
		return min(max(line, 0), count-1), true
	}
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target: ui.UI,
			Kinds:  pointer.Move | pointer.Leave | pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll,
			// One filter serves both columns, so the range can't follow
			// either column's offset; LayoutBar clamps each afterwards.
			ScrollY: pointer.ScrollRange{Min: -math.MaxInt32, Max: math.MaxInt32},
		})
		if !ok {
			break
		}
		if ev, ok := ev.(pointer.Event); ok {
			switch ev.Kind {
			case pointer.Move:
				ui.mousePosition = ev.Position
				ui.keyboardRow = false
			case pointer.Leave:
				if !ui.selecting {
					ui.mousePosition = f32.Pt(-1, -1)
				}
			case pointer.Press:
				ui.mousePosition = ev.Position
				ui.keyboardRow = false
				if ev.Buttons.Contain(pointer.ButtonSecondary) {
					ui.alignCounterpart(gtx, c, ev.Position)
				}
				if ev.Buttons.Contain(pointer.ButtonPrimary) {
					if ui.OnInteract != nil {
						ui.OnInteract()
					}
					view, line, selectable := selectionAt(ev.Position)
					if selectable {
						ui.Selection.Begin(view, line, ev.Modifiers.Contain(key.ModShift))
						ui.selecting = true
						ui.selectionPointer = ev.PointerID
						ui.selectionStart = ev.Position
						ui.selectionMoved = false
						gtx.Execute(pointer.GrabCmd{Tag: ui.UI, ID: ev.PointerID})
						gtx.Execute(key.FocusCmd{Tag: ui.UI})
					} else {
						ui.Selection.Clear()
					}
				}
			case pointer.Drag:
				ui.mousePosition = ev.Position
				if ui.selecting && ev.PointerID == ui.selectionPointer {
					if math.Abs(float64(ev.Position.X-ui.selectionStart.X)) > 3 || math.Abs(float64(ev.Position.Y-ui.selectionStart.Y)) > 3 {
						ui.selectionMoved = true
					}
					if line, ok := selectionDragLine(ui.Selection.View, ev.Position); ok {
						ui.Selection.Extend(ui.Selection.View, line)
					}
				}
			case pointer.Release:
				ui.mousePosition = ev.Position
				if ui.selecting && ev.PointerID == ui.selectionPointer {
					if line, ok := selectionDragLine(ui.Selection.View, ev.Position); ok {
						ui.Selection.Extend(ui.Selection.View, line)
					}
					mouseClicked = !ui.selectionMoved
					ui.selecting = false
				}
			case pointer.Cancel:
				ui.selecting = false
			case pointer.Scroll:
				ui.mousePosition = ev.Position
				// Clamp here, with the bounds layoutScrollbars uses, so the
				// frame isn't drawn scrolled past the end.
				overflow := float32(lineHeight)
				switch {
				case c.asm.Contains(ev.Position.X) || ui.ShowNative && c.native.Contains(ev.Position.X):
					ui.asm.Anim.Stop()
					ui.asm.Offset -= ev.Scroll.Y
					ui.asm.Clamp(gtx, -overflow, float32(len(ui.Code.Insts)*lineHeight)+overflow)
					ui.syncScroll(gtx, c, true)
				case c.source.Contains(ev.Position.X):
					ui.src.Anim.Stop()
					ui.src.Offset -= ev.Scroll.Y
					ui.src.Clamp(gtx, -overflow, float32(sourceRowCount(ui.Code, ui.noteRows())*lineHeight)+overflow)
					ui.syncScroll(gtx, c, false)
				}
			}
		}
	}
	selectionFocus := event.Tag(ui.UI)
	var commentTag event.Tag
	if ui.CommentEditor != nil {
		commentTag = &ui.CommentEditor.Editor
	}
	commentFocused := commentTag != nil && gtx.Focused(commentTag)
	if ui.Selection.Active && !commentFocused {
		// A drag can leave keyboard focus on a surrounding widget on macOS.
		// An active line selection still owns Cmd/Ctrl+C, Cmd/Ctrl+A, and
		// Escape unless the user is editing comment text. (A focused text
		// editor elsewhere still wins: it polls earlier in layout order.)
		selectionFocus = nil
	}
	for {
		ev, ok := gtx.Event(key.Filter{Focus: selectionFocus, Required: key.ModShortcut, Name: key.Name("C")})
		if !ok {
			break
		}
		keyEvent, ok := ev.(key.Event)
		if ok && keyEvent.State == key.Press {
			if text := ui.SelectedText(); text != "" && ui.CopyText != nil {
				ui.CopyText(gtx, text)
			}
		}
	}
	filters := []event.Filter{
		key.FocusFilter{Target: ui.UI},
		key.Filter{Focus: selectionFocus, Required: key.ModShortcut, Name: key.Name("A")},
		key.Filter{Focus: selectionFocus, Name: key.NameEscape},
		key.Filter{Focus: selectionFocus, Name: key.NameReturn},
	}
	for _, name := range navKeys {
		filters = append(filters, key.Filter{Focus: selectionFocus, Name: name})
	}
	if commentTag != nil {
		// A nil Focus would match whatever has focus.
		filters = append(filters, key.Filter{Focus: commentTag, Name: key.NameEscape})
	}
	for {
		ev, ok := gtx.Event(filters...)
		if !ok {
			break
		}
		keyEvent, ok := ev.(key.Event)
		if !ok || keyEvent.State != key.Press {
			continue
		}
		switch keyEvent.Name {
		case key.Name("A"):
			view := ui.Selection.View
			if view == ViewNone {
				view = ViewGoAsm
			}
			lineCount := len(ui.Code.Insts)
			if view == ViewSource {
				lineCount = sourceRowCount(ui.Code, ui.noteRows())
			}
			if lineCount > 0 {
				ui.Selection = TextSelection{View: view, Anchor: 0, Head: lineCount - 1, Active: true}
			}
		case key.NameEscape:
			if commentFocused {
				gtx.Execute(key.FocusCmd{Tag: ui.UI})
			} else {
				ui.Selection.Clear()
			}
		case key.NameReturn:
			ui.editComment(gtx)
		default:
			if !commentFocused {
				ui.navigate(gtx, c, keyEvent.Name)
			}
		}
	}
	return mouseClicked
}

// doubleClicked records a click on row and reports whether it is the
// second of a double click.
func (ui Style) doubleClicked(gtx layout.Context, row string) bool {
	double := row == ui.lastClickRow && gtx.Now.Sub(ui.lastClickAt) < 500*time.Millisecond
	ui.lastClickRow, ui.lastClickAt = row, gtx.Now
	if double {
		ui.lastClickRow = ""
	}
	return double
}

// editComment focuses the comment editor on the selected row. A plain
// click only selects, so stray typing and shortcuts after it don't land
// in a comment.
func (ui Style) editComment(gtx layout.Context) {
	if ui.CommentEditor == nil {
		return
	}
	if ui.SelectedAsm < 0 && (ui.SelectedView != ViewSource || ui.SelectedLine <= 0) {
		return
	}
	gtx.Execute(key.FocusCmd{Tag: &ui.CommentEditor.Editor})
}

// resolveHover computes the pointer hover state and handles clicks on the
// assembly column: following call targets, selecting a line for comment
// editing, and activating jump animations.
func (ui Style) resolveHover(gtx layout.Context, c codeColumns, mouseClicked bool) codeHover {
	lineHeight := c.lineHeight
	mousePosition := ui.mousePosition
	mouseInAsm := c.asm.Contains(mousePosition.X) || (ui.ShowNative && c.native.Contains(mousePosition.X))
	mouseInSource := c.source.Contains(mousePosition.X)
	if mouseInAsm || mouseInSource {
		pointer.CursorText.Add(gtx.Ops)
	}
	highlightAsmIndex := -1
	if relative := mousePosition.Y - ui.asm.Offset; mouseInAsm && relative >= 0 {
		highlightAsmIndex = int(relative) / lineHeight
	}
	if ui.keyboardRow {
		// The pointer stays put while the keyboard moves the selection.
		highlightAsmIndex = ui.SelectedAsm
		mouseClicked = false
	}

	if 0 <= highlightAsmIndex && highlightAsmIndex < len(ui.Code.Insts) {
		activateClicked := mouseClicked && ui.SelectedAsm == highlightAsmIndex
		ix := &ui.Code.Insts[highlightAsmIndex]
		callTargetHovered := ui.TryOpen != nil &&
			ix.Call != "" &&
			c.asm.Contains(mousePosition.X) &&
			mousePosition.X <= float32(c.goCol.textLeft+c.goCol.codeWidth) &&
			ui.callTargetHit(gtx, *ix, c.goCol.textLeft+ix.Indent*c.indentStep, mousePosition.X)
		if callTargetHovered {
			pointer.CursorPointer.Add(gtx.Ops)
			if mouseClicked {
				ui.SelectedAsm = highlightAsmIndex
				ui.SelectedView = ViewGoAsm
				ui.SelectedFile = ""
				ui.SelectedLine = 0
				ui.TryOpen(gtx, ix.Call)
			}
		} else if mouseClicked && ix.Text != "" {
			// Spacer rows (empty synthetic instructions) have no inline
			// editor; focusing it would swallow subsequent typing.
			ui.SelectedAsm = highlightAsmIndex
			ui.SelectedFile = ""
			ui.SelectedLine = 0
			if ui.ShowNative && c.native.Contains(mousePosition.X) {
				ui.SelectedView = ViewNativeAsm
			} else {
				ui.SelectedView = ViewGoAsm
			}
			if ui.doubleClicked(gtx, fmt.Sprint(ui.SelectedView, highlightAsmIndex)) {
				ui.editComment(gtx)
			}
		}
		if ix.Call == "" && ix.RefOffset != 0 {
			pointer.CursorPointer.Add(gtx.Ops)
			if activateClicked {
				// TODO: smooth scroll
				// highlightAsmIndex -= ix.RefOffset
				ui.asm.Anim.Start(gtx, ui.asm.Offset, ui.asm.Offset-float32(ix.RefOffset*lineHeight), 150*time.Millisecond)
			}
		}
	}
	if ui.SelectedAsm < 0 || ui.SelectedAsm >= len(ui.Code.Insts) {
		ui.SelectedAsm = -1
	}

	return codeHover{
		position: mousePosition,
		inAsm:    mouseInAsm,
		inSource: mouseInSource,
		asmIndex: highlightAsmIndex,
	}
}

func (ui Style) callTargetHit(gtx layout.Context, inst disasm.Inst, left int, x float32) bool {
	if inst.Call == "" {
		return false
	}
	start := strings.Index(inst.Text, inst.Call)
	if start < 0 {
		return false
	}
	// A hovered call line is drawn bold and italic; measure with the
	// same style, otherwise the hitbox drifts from the visible text
	// whenever the fallback font is proportional.
	f := font.Font{Typeface: gui.Monospace, Weight: font.Black, Style: font.Italic}
	end := start + len(inst.Call)
	targetLeft := left + ui.measureAsmTextWidth(gtx, f, inst.Text[:start])
	targetRight := left + ui.measureAsmTextWidth(gtx, f, inst.Text[:end])
	return float32(targetLeft) <= x && x <= float32(targetRight)
}

func (ui Style) measureAsmTextWidth(gtx layout.Context, f font.Font, text string) int {
	if text == "" {
		return 0
	}
	gtx.Constraints.Min = image.Point{}
	gtx.Constraints.Max = image.Pt(gui.MaxLineWidth, gui.MaxLineWidth)

	macro := op.Record(gtx.Ops)
	dims := widget.Label{MaxLines: 1}.Layout(gtx, ui.Theme.Shaper, f, ui.TextHeight, text, op.CallOp{})
	_ = macro.Stop()
	return dims.Size.X
}

// alignCounterpart scrolls the other side so the counterpart of the row
// at position lines up with it: the source line of an instruction, or
// the instructions of a source line. A line compiled into several runs
// of instructions steps to the next run on each press.
func (ui Style) alignCounterpart(gtx layout.Context, c codeColumns, position f32.Point) {
	lineHeight := c.lineHeight
	duration := 150 * time.Millisecond
	if c.asm.Contains(position.X) || ui.ShowNative && c.native.Contains(position.X) {
		i := int(position.Y-ui.asm.Offset) / lineHeight
		if !ui.ShowSource || position.Y < ui.asm.Offset || i >= len(ui.Code.Insts) {
			return
		}
		row := ui.sourceLineRow(ui.Code.Insts[i].File, ui.Code.Insts[i].Line)
		if row < 0 {
			return
		}
		top := ui.asm.Offset + float32(i*lineHeight)
		ui.src.Anim.Start(gtx, ui.src.Offset, top-float32(row*lineHeight), duration)
		return
	}
	if !c.source.Contains(position.X) || !(ui.ShowGo || ui.ShowNative) {
		return
	}
	row := sourceRowAtY(ui.Code, ui.noteRows(), ui.src.Offset, lineHeight, position.Y)
	file, line, ok := ui.sourceRowLine(row)
	if !ok {
		return
	}
	top := ui.src.Offset + float32(row*lineHeight)
	// The instruction now beside the line. When it is in one of the
	// line's runs, step to the next run; otherwise go to the nearest.
	beside := int(math.Floor(float64((top - ui.asm.Offset) / float32(lineHeight))))
	insts := ui.Code.Insts
	at := func(i int) bool { return 0 <= i && i < len(insts) && insts[i].File == file && insts[i].Line == line }
	var starts []int
	for i := range insts {
		if at(i) && !at(i-1) {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		return
	}
	next := starts[0]
	if at(beside) {
		for _, i := range starts {
			if i > beside {
				next = i
				break
			}
		}
	} else {
		for _, i := range starts {
			if abs(i-beside) < abs(next-beside) {
				next = i
			}
		}
	}
	ui.asm.Anim.Start(gtx, ui.asm.Offset, top-float32(next*lineHeight), duration)
}

// sourceLineRow is the source row index of line in file, or -1.
func (ui *UI) sourceLineRow(file string, line int) int {
	row := 0
	for r := range sourceRows(ui.Code, ui.noteRows()) {
		if r.kind == sourceRowLine {
			src := ui.Code.Source[r.file]
			if src.File == file && src.Blocks[r.block].From+r.off == line {
				return row
			}
		}
		row++
	}
	return -1
}

// sourceRowLine is the file and line shown on source row index, if it
// shows one.
func (ui *UI) sourceRowLine(index int) (string, int, bool) {
	row := 0
	for r := range sourceRows(ui.Code, ui.noteRows()) {
		if row == index {
			if r.kind != sourceRowLine {
				return "", 0, false
			}
			src := ui.Code.Source[r.file]
			return src.File, src.Blocks[r.block].From + r.off, true
		}
		row++
	}
	return "", 0, false
}

func abs(x int) int { return max(x, -x) }
