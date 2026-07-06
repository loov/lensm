package codeview

import (
	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"image"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
	"math"
	"strings"
	"time"
)

// handleInput processes pointer and keyboard events for the frame,
// updating selection and scroll state, and reports whether the release
// was a click (as opposed to a drag).
func (ui Style) handleInput(gtx layout.Context, c codeColumns) (mouseClicked bool) {
	lineHeight := c.lineHeight
	event.Op(gtx.Ops, ui.UI)
	selectionAt := func(position f32.Point) (View, int, bool) {
		if c.asm.Contains(position.X) {
			relative := position.Y - ui.asm.scroll
			if relative < 0 {
				return ViewNone, -1, false
			}
			line := int(relative) / lineHeight
			return ViewGoAsm, line, gui.InRange(line, len(ui.Code.Insts))
		}
		if ui.ShowNative && c.native.Contains(position.X) {
			relative := position.Y - ui.asm.scroll
			if relative < 0 {
				return ViewNone, -1, false
			}
			line := int(relative) / lineHeight
			return ViewNativeAsm, line, gui.InRange(line, len(ui.Code.Insts))
		}
		if c.source.Contains(position.X) {
			line := sourceRowAtY(ui.Code, ui.src.scroll, lineHeight, position.Y)
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
			line = int(position.Y-ui.asm.scroll) / lineHeight
		case ViewSource:
			count = sourceRowCount(ui.Code)
			line = int(position.Y-ui.src.scroll) / lineHeight
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
			ScrollY: pointer.ScrollRange{
				Min: int(ui.asm.scroll) - lineHeight,
				Max: len(ui.Code.Insts)*lineHeight + lineHeight - int(ui.asm.scroll),
			},
		})
		if !ok {
			break
		}
		if ev, ok := ev.(pointer.Event); ok {
			switch ev.Kind {
			case pointer.Move:
				ui.mousePosition = ev.Position
			case pointer.Leave:
				if !ui.selecting {
					ui.mousePosition = f32.Pt(-1, -1)
				}
			case pointer.Press:
				ui.mousePosition = ev.Position
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
				switch {
				case c.asm.Contains(ev.Position.X):
					ui.asm.scroll -= ev.Scroll.Y
				case ui.ShowNative && c.native.Contains(ev.Position.X):
					ui.asm.scroll -= ev.Scroll.Y
				case c.source.Contains(ev.Position.X):
					ui.src.scroll -= ev.Scroll.Y
				}
			}
		}
	}
	selectionFocus := event.Tag(ui.UI)
	commentFocused := ui.CommentEditor != nil && gtx.Focused(ui.CommentEditor)
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
			if text := ui.Selection.Text(ui.Code); text != "" && ui.CopyText != nil {
				ui.CopyText(gtx, text)
			}
		}
	}
	for {
		ev, ok := gtx.Event(
			key.FocusFilter{Target: ui.UI},
			key.Filter{Focus: selectionFocus, Required: key.ModShortcut, Name: key.Name("A")},
			key.Filter{Focus: selectionFocus, Name: key.NameEscape},
		)
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
				lineCount = sourceRowCount(ui.Code)
			}
			if lineCount > 0 {
				ui.Selection = TextSelection{View: view, Anchor: 0, Head: lineCount - 1, Active: true}
			}
		case key.NameEscape:
			ui.Selection.Clear()
		}
	}
	return mouseClicked
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
	if relative := mousePosition.Y - ui.asm.scroll; mouseInAsm && relative >= 0 {
		highlightAsmIndex = int(relative) / lineHeight
	}

	if gui.InRange(highlightAsmIndex, len(ui.Code.Insts)) {
		activateClicked := mouseClicked && ui.SelectedAsm == highlightAsmIndex
		ix := &ui.Code.Insts[highlightAsmIndex]
		callTargetHovered := ui.TryOpen != nil &&
			ix.Call != "" &&
			c.asm.Contains(mousePosition.X) &&
			mousePosition.X <= float32(c.goTextLeft+c.goInstructionWidth) &&
			ui.callTargetHit(gtx, *ix, c.goTextLeft, mousePosition.X)
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
			if ui.CommentEditor != nil {
				gtx.Execute(key.FocusCmd{Tag: ui.CommentEditor})
			}
		}
		if ix.Call == "" && ix.RefOffset != 0 {
			pointer.CursorPointer.Add(gtx.Ops)
			if activateClicked {
				// TODO: smooth scroll
				// highlightAsmIndex -= ix.RefOffset
				ui.asm.anim.Start(gtx, ui.asm.scroll, ui.asm.scroll-float32(ix.RefOffset*lineHeight), 150*time.Millisecond)
			}
		}
	}
	if !gui.InRange(ui.SelectedAsm, len(ui.Code.Insts)) {
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
	f := font.Font{Typeface: "override-monospace,Go,monospace", Weight: font.Black, Style: font.Italic}
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
