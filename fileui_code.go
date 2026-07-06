package main

import (
	"image"
	"image/color"
	"math"
	"strconv"
	"strings"
	"time"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/asmhelp"
	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/f32color"
	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/syntax"
)

type CodeUI struct {
	*disasm.Code

	asm struct {
		scroll  float32
		gesture gesture.Scroll
		bar     widget.Scrollbar
		anim    gui.ScrollAnimation
	}
	src struct {
		scroll  float32
		gesture gesture.Scroll
		bar     widget.Scrollbar
	}

	hl highlightCache

	mousePosition f32.Point
	SelectedAsm   int
	SelectedView  CodeView
	SelectedFile  string
	SelectedLine  int
	Selection     TextSelection

	selecting        bool
	selectionPointer pointer.ID
	selectionStart   f32.Point
	selectionMoved   bool
}

// highlightCache holds the syntax highlight spans for a Code. Layout
// runs every frame; highlighting is a pure function of the immutable
// code and the palette, so it is rebuilt only when either changes —
// re-highlighting per frame allocates a go/scanner per source line.
type highlightCache struct {
	code   *disasm.Code
	syntax syntax.Palette

	asm        [][]syntax.Span
	nativeText []string
	native     [][]syntax.Span
	// source is indexed by source file, block, and line within the block.
	source [][][][]syntax.Span
}

func (hl *highlightCache) update(code *disasm.Code, palette syntax.Palette) {
	if hl.code == code && hl.syntax == palette {
		return
	}
	hl.code = code
	hl.syntax = palette

	hl.asm = make([][]syntax.Span, len(code.Insts))
	hl.nativeText = make([]string, len(code.Insts))
	hl.native = make([][]syntax.Span, len(code.Insts))
	for i := range code.Insts {
		ix := &code.Insts[i]
		hl.asm[i] = syntax.HighlightAsm(ix.Text, ix.Call, palette)
		hl.nativeText[i] = strings.ToUpper(ix.NativeText)
		hl.native[i] = syntax.HighlightAsm(hl.nativeText[i], "", palette)
	}

	hl.source = make([][][][]syntax.Span, len(code.Source))
	for i, src := range code.Source {
		blocks := make([][][]syntax.Span, len(src.Blocks))
		for j, block := range src.Blocks {
			lines := make([][]syntax.Span, len(block.Lines))
			for k, line := range block.Lines {
				lines[k] = syntax.HighlightSource(block.From+k, line, palette)
			}
			blocks[j] = lines
		}
		hl.source[i] = blocks
	}
}

func (ui *CodeUI) Loaded() bool {
	return ui.Code != nil
}

func (ui *CodeUI) ResetScroll() {
	ui.asm.scroll = 100000
	ui.src.scroll = 100000
}

type CodeUIStyle struct {
	*CodeUI

	TryOpen  func(gtx layout.Context, funcname string)
	CopyText func(gtx layout.Context, text string)
	// OnInteract fires on a primary press in the content, used to keep a
	// preview tab open once the user acts on it.
	OnInteract func()

	// Comments backs inline comment display and editing. Reads go through
	// the store directly; SetComment records an edit (buffered write plus
	// flush scheduling), and CommentKey/CommentEditor drive the single
	// shared inline editor.
	Comments      *comments.Store
	SetComment    func(comments.Coord, string)
	CommentKey    *string
	CommentEditor *widget.Editor
	Theme         *material.Theme
	Colors        gui.UIColors
	Syntax        syntax.Palette

	ShowNative bool
	ShowHelp   bool
	TextHeight unit.Sp
}

func (ui CodeUIStyle) Layout(gtx layout.Context) layout.Dimensions {
	gtx.Constraints = layout.Exact(gtx.Constraints.Max)
	if ui.Code == nil {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	if ui.Colors.Background == (color.NRGBA{}) {
		ui.Colors = gui.ApplyTheme(ui.Theme, false)
	}
	if ui.Syntax.Plain == (color.NRGBA{}) {
		ui.Syntax = syntax.PaletteFor(syntax.StyleGoLand, ui.Colors.SyntaxColors())
	}
	ui.CodeUI.hl.update(ui.Code, ui.Syntax)

	paint.FillShape(gtx.Ops, ui.Colors.Background, clip.Rect{Max: gtx.Constraints.Max}.Op())
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()

	c := ui.columns(gtx)
	mouseClicked := ui.handleInput(gtx, c)

	// draw gutter
	paint.FillShape(gtx.Ops, ui.Colors.Gutter, clip.Rect{
		Min: image.Pt(int(c.gutter.Min), 0),
		Max: image.Pt(int(c.gutter.Max), gtx.Constraints.Max.Y),
	}.Op())
	if scroll, ok := ui.asm.anim.Update(gtx); ok {
		ui.asm.scroll = scroll
	}

	hover := ui.resolveHover(gtx, c, mouseClicked)
	highlightRanges := ui.layoutRelations(gtx, c, hover)
	ui.layoutAssembly(gtx, c, hover, highlightRanges)
	sourceContentHeight := ui.layoutSource(gtx, c, hover, mouseClicked)
	ui.layoutScrollbars(gtx, c, sourceContentHeight)
	ui.layoutHelp(gtx, c, hover)

	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// codeColumns is the horizontal geometry of the code view: the pixel
// bounds of each column and the derived text/comment sub-regions. It all
// derives from the viewport width, line height, and whether the native
// assembly column is shown.
type codeColumns struct {
	lineHeight int
	pad        int
	jumpStep   int

	jump   gui.Bounds
	asm    gui.Bounds
	native gui.Bounds
	gutter gui.Bounds
	source gui.Bounds

	goTextLeft         int
	goInstructionWidth int
	commentLeft        int
	commentWidth       int

	nativeTextLeft         int
	nativeTextWidth        int
	nativeCommentLeft      int
	nativeCommentWidth     int
	nativeInstructionWidth int

	sourceTextLeft     int
	sourceTextWidth    int
	sourceCommentLeft  int
	sourceCommentWidth int
	sourceCodeWidth    int
}

// codeHover is the transient pointer state for one frame: where the mouse
// is and which instruction, if any, it hovers.
type codeHover struct {
	position f32.Point
	inAsm    bool
	inSource bool
	asmIndex int
}

func (ui CodeUIStyle) columns(gtx layout.Context) codeColumns {
	// The layout has the following sections:
	// pad | Jump | pad/2 | Go asm | pad | Native asm | pad | Gutter | pad | Source | pad
	lineHeight := gui.CodeLineHeightPx(gtx, ui.TextHeight)
	pad := lineHeight
	jumpStep := lineHeight / 2
	jumpWidth := jumpStep * ui.Code.MaxJump
	gutterWidth := lineHeight * 8
	fixedWidth := gutterWidth + jumpWidth + 4*pad + pad/2
	if ui.ShowNative {
		fixedWidth += pad
	}
	blocksWidth := max(0, gtx.Constraints.Max.X-fixedWidth)

	jump := gui.BoundsWidth(pad, jumpWidth)
	asmWidth := blocksWidth * 40 / 100
	if ui.ShowNative {
		asmWidth = blocksWidth * 28 / 100
	}
	asm := gui.BoundsWidth(int(jump.Max)+pad/2, asmWidth)
	native := gui.BoundsWidth(int(asm.Max), 0)
	gutter := gui.BoundsWidth(int(asm.Max)+pad, gutterWidth)
	sourceWidth := blocksWidth - int(asm.Width())
	if ui.ShowNative {
		native = gui.BoundsWidth(int(asm.Max)+pad, blocksWidth*28/100)
		gutter = gui.BoundsWidth(int(native.Max)+pad, gutterWidth)
		sourceWidth -= int(native.Width())
	}
	source := gui.BoundsWidth(int(gutter.Max)+pad, max(0, sourceWidth))

	c := codeColumns{
		lineHeight: lineHeight,
		pad:        pad,
		jumpStep:   jumpStep,
		jump:       jump,
		asm:        asm,
		native:     native,
		gutter:     gutter,
		source:     source,
	}
	minimumCommentWidth := lineHeight * 4

	c.sourceTextLeft = int(source.Min)
	c.sourceTextWidth = max(int(source.Max)-c.sourceTextLeft, 0)
	c.sourceCommentLeft = c.sourceTextLeft + c.sourceTextWidth*70/100
	c.sourceCommentWidth = int(source.Max) - c.sourceCommentLeft
	c.sourceCodeWidth = c.sourceCommentLeft - c.sourceTextLeft - pad/2
	if c.sourceCodeWidth < 0 || c.sourceCommentWidth < minimumCommentWidth {
		c.sourceCodeWidth = c.sourceTextWidth
		c.sourceCommentWidth = 0
	}

	c.goTextLeft = int(asm.Min) + pad/2
	goTextWidth := max(int(asm.Max)-c.goTextLeft, 0)
	c.nativeTextLeft = int(native.Min)
	c.nativeTextWidth = max(int(native.Max)-c.nativeTextLeft, 0)
	c.nativeCommentLeft = c.nativeTextLeft + c.nativeTextWidth*62/100
	c.nativeCommentWidth = int(native.Max) - c.nativeCommentLeft
	c.nativeInstructionWidth = c.nativeCommentLeft - c.nativeTextLeft - pad/2
	if c.nativeInstructionWidth < 0 || c.nativeCommentWidth < minimumCommentWidth {
		c.nativeInstructionWidth = c.nativeTextWidth
		c.nativeCommentWidth = 0
	}
	c.commentLeft = max(c.goTextLeft+goTextWidth*62/100, c.goTextLeft)
	c.commentWidth = max(int(asm.Max)-c.commentLeft, 0)
	c.goInstructionWidth = c.commentLeft - c.goTextLeft - pad/2
	if c.goInstructionWidth < 0 || c.commentWidth < minimumCommentWidth {
		c.goInstructionWidth = goTextWidth
		c.commentWidth = 0
	}

	return c
}

// handleInput processes pointer and keyboard events for the frame,
// updating selection and scroll state, and reports whether the release
// was a click (as opposed to a drag).
func (ui CodeUIStyle) handleInput(gtx layout.Context, c codeColumns) (mouseClicked bool) {
	lineHeight := c.lineHeight
	event.Op(gtx.Ops, ui.CodeUI)
	selectionAt := func(position f32.Point) (CodeView, int, bool) {
		if c.asm.Contains(position.X) {
			relative := position.Y - ui.asm.scroll
			if relative < 0 {
				return CodeViewNone, -1, false
			}
			line := int(relative) / lineHeight
			return CodeViewGoAsm, line, gui.InRange(line, len(ui.Code.Insts))
		}
		if ui.ShowNative && c.native.Contains(position.X) {
			relative := position.Y - ui.asm.scroll
			if relative < 0 {
				return CodeViewNone, -1, false
			}
			line := int(relative) / lineHeight
			return CodeViewNativeAsm, line, gui.InRange(line, len(ui.Code.Insts))
		}
		if c.source.Contains(position.X) {
			line := sourceRowAtY(ui.Code, ui.src.scroll, lineHeight, position.Y)
			return CodeViewSource, line, line >= 0
		}
		return CodeViewNone, -1, false
	}
	// selectionDragLine clamps a drag position to the selection view's
	// content, so a fast drag past an edge selects through to the first
	// or last line instead of stopping at the last in-range sample.
	selectionDragLine := func(view CodeView, position f32.Point) (int, bool) {
		var line, count int
		switch view {
		case CodeViewGoAsm, CodeViewNativeAsm:
			count = len(ui.Code.Insts)
			line = int(position.Y-ui.asm.scroll) / lineHeight
		case CodeViewSource:
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
			Target: ui.CodeUI,
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
						gtx.Execute(pointer.GrabCmd{Tag: ui.CodeUI, ID: ev.PointerID})
						gtx.Execute(key.FocusCmd{Tag: ui.CodeUI})
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
	selectionFocus := event.Tag(ui.CodeUI)
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
			key.FocusFilter{Target: ui.CodeUI},
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
			if view == CodeViewNone {
				view = CodeViewGoAsm
			}
			lineCount := len(ui.Code.Insts)
			if view == CodeViewSource {
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
func (ui CodeUIStyle) resolveHover(gtx layout.Context, c codeColumns, mouseClicked bool) codeHover {
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
				ui.SelectedView = CodeViewGoAsm
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
				ui.SelectedView = CodeViewNativeAsm
			} else {
				ui.SelectedView = CodeViewGoAsm
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

// layoutRelations draws the curved bands linking source lines to the
// assembly ranges they compiled to, and returns the assembly ranges under
// the pointer so the jump lines can highlight them.
func (ui CodeUIStyle) layoutRelations(gtx layout.Context, c codeColumns, hover codeHover) []disasm.LineRange {
	lineHeight := c.lineHeight
	gutter, source, asm := c.gutter, c.source, c.asm
	mousePosition := hover.position
	mouseInAsm, mouseInSource := hover.inAsm, hover.inSource

	var highlightRanges []disasm.LineRange
	top := int(ui.src.scroll)
	var highlightPaths []clip.PathSpec
	relationStroke := ui.Colors.RelationStroke
	relationFill := relationStroke
	relationFill.A /= 2
	for i, src := range ui.Code.Source {
		if i > 0 {
			top += lineHeight
		}
		top += lineHeight
		for i, block := range src.Blocks {
			if i > 0 {
				top += lineHeight
			}
			for _, ranges := range block.Related {
				if len(ranges) > 0 {
					highlight := mouseInSource && float32(top) <= mousePosition.Y && mousePosition.Y < float32(top+lineHeight)
					if !highlight && mouseInAsm {
						for _, r := range ranges {
							if float32(r.From*lineHeight)+ui.asm.scroll <= mousePosition.Y && mousePosition.Y < float32(r.To*lineHeight)+ui.asm.scroll {
								highlight = true
								break
							}
						}
					}
					if highlight {
						highlightRanges = ranges

						var p clip.Path
						p.Begin(gtx.Ops)
						p.MoveTo(f32.Pt(gutter.Max, float32(top+lineHeight)))
						p.LineTo(f32.Pt(source.Max, float32(top+lineHeight)))
						p.LineTo(f32.Pt(source.Max, float32(top)))
						p.LineTo(f32.Pt(gutter.Max, float32(top)))
						pin := float32(top)
						for i, r := range ranges {
							const S = 0.1
							p.CubeTo(
								f32.Pt(gutter.Lerp(0.5-S), pin),
								f32.Pt(gutter.Lerp(0.5+S), float32(r.From*lineHeight)+ui.asm.scroll),
								f32.Pt(gutter.Min, float32(r.From*lineHeight)+ui.asm.scroll))
							p.LineTo(f32.Pt(asm.Min, float32(r.From*lineHeight)+ui.asm.scroll))
							p.LineTo(f32.Pt(asm.Min, float32(r.To*lineHeight)+ui.asm.scroll))
							p.LineTo(f32.Pt(gutter.Min, float32(r.To*lineHeight)+ui.asm.scroll))
							pin = float32(top) + float32(lineHeight)*float32(i+1)/float32(len(ranges))
							p.CubeTo(
								f32.Pt(gutter.Lerp(0.5+S), float32(r.To*lineHeight)+ui.asm.scroll),
								f32.Pt(gutter.Lerp(0.5-S), pin),
								f32.Pt(gutter.Max, pin))
						}
						highlightPaths = append(highlightPaths, p.End())
					}
				}
				top += lineHeight
			}
		}
	}
	for _, path := range highlightPaths {
		paint.FillShape(gtx.Ops, relationFill, clip.Outline{Path: path}.Op())
		paint.FillShape(gtx.Ops, relationStroke, clip.Stroke{Path: path, Width: 1}.Op())
	}
	return highlightRanges
}

// layoutAssembly draws the Go and native assembly columns: selection
// highlights, instruction text, inline comments, and jump lines.
func (ui CodeUIStyle) layoutAssembly(gtx layout.Context, c codeColumns, hover codeHover, highlightRanges []disasm.LineRange) {
	hl := &ui.CodeUI.hl
	lineHeight := c.lineHeight
	pad, jumpStep := c.pad, c.jumpStep
	jump, asm, native, gutter := c.jump, c.asm, c.native, c.gutter
	highlightAsmIndex := hover.asmIndex

	asmClip := clip.Rect{
		Min: image.Pt(int(jump.Min), 0),
		Max: image.Pt(int(gutter.Min), gtx.Constraints.Max.Y),
	}.Push(gtx.Ops)
	if ui.ShowNative {
		paint.FillShape(gtx.Ops, ui.Colors.Splitter, clip.Rect{
			Min: image.Pt(int(native.Min)-pad/2, 0),
			Max: image.Pt(int(native.Min)-pad/2+1, gtx.Constraints.Max.Y),
		}.Op())
	}
	for i, ix := range ui.Code.Insts {
		if ui.Selection.Contains(CodeViewGoAsm, i) {
			paint.FillShape(gtx.Ops, ui.Colors.Selection, clip.Rect{
				Min: image.Pt(int(asm.Min), i*lineHeight+int(ui.asm.scroll)),
				Max: image.Pt(int(asm.Max), (i+1)*lineHeight+int(ui.asm.scroll)),
			}.Op())
		}
		if ui.ShowNative && ui.Selection.Contains(CodeViewNativeAsm, i) {
			paint.FillShape(gtx.Ops, ui.Colors.Selection, clip.Rect{
				Min: image.Pt(int(native.Min), i*lineHeight+int(ui.asm.scroll)),
				Max: image.Pt(int(native.Max), (i+1)*lineHeight+int(ui.asm.scroll)),
			}.Op())
		}
		if ui.SelectedAsm == i {
			paint.FillShape(gtx.Ops, ui.Colors.Selection, clip.Rect{
				Min: image.Pt(int(asm.Min), i*lineHeight+int(ui.asm.scroll)),
				Max: image.Pt(int(gutter.Min), (i+1)*lineHeight+int(ui.asm.scroll)),
			}.Op())
		}
		gui.SourceLine{
			TopLeft:    image.Pt(c.goTextLeft, i*lineHeight+int(ui.asm.scroll)),
			Width:      c.goInstructionWidth,
			Text:       ix.Text,
			Spans:      hl.asm[i],
			TextHeight: ui.TextHeight,
			Italic:     ix.Call != "",
			Bold:       highlightAsmIndex == i || ui.SelectedAsm == i,
			Color:      ui.Syntax.Plain,
		}.Layout(ui.Theme, gtx)
		if c.commentWidth > 0 && ix.Text != "" {
			comment := ui.Comments.Get(ui.asmCoord(CodeViewGoAsm, ix))
			if ui.SelectedAsm == i && ui.SelectedView == CodeViewGoAsm {
				ui.layoutInlineCommentEditor(gtx, ui.asmCoord(CodeViewGoAsm, ix), ";", i*lineHeight+int(ui.asm.scroll), c.commentLeft, c.commentWidth, lineHeight)
			} else if comment != "" {
				gui.SourceLine{
					TopLeft:    image.Pt(c.commentLeft, i*lineHeight+int(ui.asm.scroll)),
					Width:      c.commentWidth,
					Text:       "; " + comment,
					TextHeight: ui.TextHeight,
					Italic:     true,
					Color:      ui.Colors.MutedText,
				}.Layout(ui.Theme, gtx)
			}
		}
		if ui.ShowNative {
			nativeComment := ui.Comments.Get(ui.asmCoord(CodeViewNativeAsm, ix))
			width := c.nativeTextWidth
			if (nativeComment != "" || (ui.SelectedAsm == i && ui.SelectedView == CodeViewNativeAsm)) && c.nativeCommentWidth > 0 {
				width = c.nativeInstructionWidth
			}
			gui.SourceLine{
				TopLeft:    image.Pt(c.nativeTextLeft, i*lineHeight+int(ui.asm.scroll)),
				Width:      width,
				Text:       hl.nativeText[i],
				Spans:      hl.native[i],
				TextHeight: ui.TextHeight,
				Bold:       highlightAsmIndex == i || ui.SelectedAsm == i,
				Color:      ui.Syntax.Plain,
			}.Layout(ui.Theme, gtx)
			if ui.SelectedAsm == i && ui.SelectedView == CodeViewNativeAsm && c.nativeCommentWidth > 0 {
				ui.layoutInlineCommentEditor(gtx, ui.asmCoord(CodeViewNativeAsm, ix), ";", i*lineHeight+int(ui.asm.scroll), c.nativeCommentLeft, c.nativeCommentWidth, lineHeight)
			} else if nativeComment != "" && c.nativeCommentWidth > 0 {
				gui.SourceLine{
					TopLeft:    image.Pt(c.nativeCommentLeft, i*lineHeight+int(ui.asm.scroll)),
					Width:      c.nativeCommentWidth,
					Text:       "; " + nativeComment,
					TextHeight: ui.TextHeight,
					Italic:     true,
					Color:      ui.Colors.MutedText,
				}.Layout(ui.Theme, gtx)
			}
		}

		// jump line
		if ix.RefOffset != 0 {
			lineWidth := gtx.Metric.Dp(1)
			align := float32(lineWidth%2) / 2
			stack := op.Affine(f32.Affine2D{}.Offset(
				f32.Pt(jump.Max+align, float32(i*lineHeight)+align+ui.asm.scroll))).Push(gtx.Ops)

			var path clip.Path
			path.Begin(gtx.Ops)
			path.MoveTo(f32.Pt(float32(pad/2), float32(lineHeight*2/3)))
			path.LineTo(f32.Pt(float32(-jumpStep*ix.RefStack), float32(lineHeight*2/3)))
			path.LineTo(f32.Pt(float32(-jumpStep*ix.RefStack), float32(lineHeight/3+ix.RefOffset*lineHeight)))
			path.LineTo(f32.Pt(float32(-jumpStep/2), float32(lineHeight/3+ix.RefOffset*lineHeight)))
			// draw arrow
			path.Line(f32.Pt(0, float32(lineHeight/4)))
			path.Line(f32.Pt(float32(lineHeight/3), float32(-lineHeight/4)))
			path.Line(f32.Pt(float32(-lineHeight/3), float32(-lineHeight/4)))
			path.Line(f32.Pt(0, float32(lineHeight/4)))

			width := float32(lineWidth)
			alpha := float32(0.7)
			if highlightAsmIndex >= 0 && (highlightAsmIndex == i || highlightAsmIndex == i+ix.RefOffset) {
				width *= 3
				alpha = 1
			} else if disasm.LineRangesContain(highlightRanges, i, i+ix.RefOffset) {
				width *= 3
			}
			jumpColor := f32color.HSLA(float32(math.Mod(float64(ix.PC)*math.Phi, 1)), 0.8, 0.4, alpha)
			paint.FillShape(gtx.Ops, jumpColor, clip.Stroke{Path: path.End(), Width: width}.Op())

			stack.Pop()
		}
	}
	asmClip.Pop()
}

// layoutSource draws the source column: file headers, source lines,
// selection highlights, and inline comments. It returns the total pixel
// height of the source content for the scrollbar.
func (ui CodeUIStyle) layoutSource(gtx layout.Context, c codeColumns, hover codeHover, mouseClicked bool) int {
	hl := &ui.CodeUI.hl
	lineHeight := c.lineHeight
	source := c.source
	mousePosition := hover.position
	mouseInSource := hover.inSource

	sourceClip := clip.Rect{
		Min: image.Pt(int(source.Min), 0),
		Max: image.Pt(int(source.Max), gtx.Constraints.Max.Y),
	}.Push(gtx.Ops)
	top := int(ui.src.scroll)
	sourceRow := 0
	paintSourceSelection := func(row, rowTop int) {
		if ui.Selection.Contains(CodeViewSource, row) {
			paint.FillShape(gtx.Ops, ui.Colors.Selection, clip.Rect{
				Min: image.Pt(int(source.Min), rowTop),
				Max: image.Pt(int(source.Max), rowTop+lineHeight),
			}.Op())
		}
	}
	for i, src := range ui.Code.Source {
		if i > 0 {
			paintSourceSelection(sourceRow, top)
			top += lineHeight
			sourceRow++
		}
		paintSourceSelection(sourceRow, top)
		gui.SourceLine{
			TopLeft:    image.Pt(int(source.Min), top),
			Text:       src.File,
			TextHeight: ui.TextHeight,
			Bold:       hover.asmIndex == i,
			Color:      ui.Colors.MutedText,
		}.Layout(ui.Theme, gtx)
		top += lineHeight
		sourceRow++
		for blockIndex, block := range src.Blocks {
			if blockIndex > 0 {
				paintSourceSelection(sourceRow, top)
				top += lineHeight
				sourceRow++
			}
			for off := range block.Lines {
				paintSourceSelection(sourceRow, top)
				highlight := mouseInSource && float32(top) <= mousePosition.Y && mousePosition.Y < float32(top+lineHeight)
				lineNo := block.From + off
				if highlight && mouseClicked {
					ui.SelectedAsm = -1
					ui.SelectedView = CodeViewSource
					ui.SelectedFile = src.File
					ui.SelectedLine = lineNo
					if ui.CommentEditor != nil {
						gtx.Execute(key.FocusCmd{Tag: ui.CommentEditor})
					}
				}
				sourceComment := ui.Comments.Get(ui.sourceCoord(src.File, lineNo))
				width := c.sourceTextWidth
				selectedSource := ui.SelectedView == CodeViewSource && ui.SelectedFile == src.File && ui.SelectedLine == lineNo
				if (sourceComment != "" || selectedSource) && c.sourceCommentWidth > 0 {
					width = c.sourceCodeWidth
				}
				gui.SourceLine{
					TopLeft:    image.Pt(int(source.Min), top),
					Width:      width,
					Spans:      hl.source[i][blockIndex][off],
					TextHeight: ui.TextHeight,
					Bold:       highlight,
					Color:      ui.Syntax.Plain,
				}.Layout(ui.Theme, gtx)
				if selectedSource && c.sourceCommentWidth > 0 {
					ui.layoutInlineCommentEditor(gtx, ui.sourceCoord(src.File, lineNo), "//", top, c.sourceCommentLeft, c.sourceCommentWidth, lineHeight)
				} else if sourceComment != "" && c.sourceCommentWidth > 0 {
					gui.SourceLine{
						TopLeft:    image.Pt(c.sourceCommentLeft, top),
						Width:      c.sourceCommentWidth,
						Text:       "// " + sourceComment,
						TextHeight: ui.TextHeight,
						Italic:     true,
						Color:      ui.Colors.MutedText,
					}.Layout(ui.Theme, gtx)
				}
				top += lineHeight
				sourceRow++
			}
		}
	}
	sourceClip.Pop()
	return top - int(ui.src.scroll)
}

// layoutScrollbars draws and services the assembly and source scrollbars,
// clamping each column's scroll offset to its content.
func (ui CodeUIStyle) layoutScrollbars(gtx layout.Context, c codeColumns, sourceContentHeight int) {
	lineHeight := c.lineHeight
	pad := c.pad
	jump, gutter, source := c.jump, c.gutter, c.source

	{
		stack := clip.Rect{
			Min: image.Pt(int(jump.Min)-pad, 0),
			Max: image.Pt(int(gutter.Min), gtx.Constraints.Max.Y),
		}.Push(gtx.Ops)

		// overflow := gtx.Constraints.Max.Y / 3
		overflow := lineHeight
		contentTop := float32(-overflow)
		contentBot := float32(len(ui.Code.Insts)*lineHeight + overflow)
		viewTop := -ui.asm.scroll
		viewBot := -ui.asm.scroll + float32(gtx.Constraints.Max.Y)

		{
			stack := op.Offset(image.Pt(int(jump.Min)-pad, 0)).Push(gtx.Ops)
			gtx := gtx
			gtx.Constraints = layout.Exact(image.Pt(pad, gtx.Constraints.Max.Y))
			material.Scrollbar(ui.Theme, &ui.asm.bar).Layout(gtx, layout.Vertical,
				(viewTop-contentTop)/(contentBot-contentTop),
				(viewBot-contentTop)/(contentBot-contentTop),
			)
			stack.Pop()
		}

		if distance := ui.asm.bar.ScrollDistance(); distance != 0 {
			ui.asm.scroll -= distance * (contentBot - contentTop)
		}

		if distance := ui.asm.gesture.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Vertical,
			pointer.ScrollRange{},
			pointer.ScrollRange{Min: -1000, Max: 1000},
		); distance != 0 {
			ui.asm.scroll -= float32(distance)
		}

		if -ui.asm.scroll < contentTop {
			ui.asm.scroll = -contentTop
			ui.asm.anim.Stop()
		}
		if -ui.asm.scroll+float32(gtx.Constraints.Max.Y) > contentBot {
			if contentBot < float32(gtx.Constraints.Max.Y) {
				ui.asm.scroll = -contentTop
			} else {
				ui.asm.scroll = float32(gtx.Constraints.Max.Y) - contentBot
			}
			ui.asm.anim.Stop()
		}
		stack.Pop()
	}

	{
		stack := clip.Rect{
			Min: image.Pt(int(source.Min), 0),
			Max: image.Pt(int(source.Max)+pad, gtx.Constraints.Max.Y),
		}.Push(gtx.Ops)

		// overflow := gtx.Constraints.Max.Y / 3
		overflow := lineHeight
		contentTop := float32(-overflow)
		contentBot := float32(sourceContentHeight + overflow)
		viewTop := -ui.src.scroll
		viewBot := -ui.src.scroll + float32(gtx.Constraints.Max.Y)

		{
			stack := op.Offset(image.Pt(int(source.Max), 0)).Push(gtx.Ops)
			gtx := gtx
			gtx.Constraints = layout.Exact(image.Pt(pad, gtx.Constraints.Max.Y))
			material.Scrollbar(ui.Theme, &ui.src.bar).Layout(gtx, layout.Vertical,
				(viewTop-contentTop)/(contentBot-contentTop),
				(viewBot-contentTop)/(contentBot-contentTop),
			)
			stack.Pop()
		}

		if distance := ui.src.bar.ScrollDistance(); distance != 0 {
			ui.src.scroll -= distance * (contentBot - contentTop)
		}
		if distance := ui.src.gesture.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Vertical,
			pointer.ScrollRange{},
			pointer.ScrollRange{Min: -1000, Max: 1000},
		); distance != 0 {
			ui.src.scroll -= float32(distance)
		}

		if -ui.src.scroll < contentTop {
			ui.src.scroll = -contentTop
		}
		if -ui.src.scroll+float32(gtx.Constraints.Max.Y) > contentBot {
			if contentBot < float32(gtx.Constraints.Max.Y) {
				ui.src.scroll = -contentTop
			} else {
				ui.src.scroll = float32(gtx.Constraints.Max.Y) - contentBot
			}
		}
		stack.Pop()
	}
}

// layoutHelp draws the instruction help tooltip for the hovered assembly
// line, when help is enabled and the user is not selecting or editing.
func (ui CodeUIStyle) layoutHelp(gtx layout.Context, c codeColumns, hover codeHover) {
	commentEditing := ui.CommentEditor != nil && gtx.Focused(ui.CommentEditor)
	if !ui.ShowHelp || ui.selecting || commentEditing || !gui.InRange(hover.asmIndex, len(ui.Code.Insts)) {
		return
	}
	inst := ui.Code.Insts[hover.asmIndex]
	nativeHovered := ui.ShowNative && c.native.Contains(hover.position.X)
	var help asmhelp.Help
	var ok bool
	if nativeHovered {
		help, ok = asmhelp.ForNative(ui.Code.Arch, inst.Mnemonic, inst.NativeText)
	} else {
		help, ok = asmhelp.ForInstruction(ui.Code.Arch, inst.Mnemonic, inst.Text)
	}
	if ok {
		ui.layoutAssemblyHelp(gtx, help, hover.position)
	}
}

func (ui CodeUIStyle) layoutAssemblyHelp(gtx layout.Context, help asmhelp.Help, position f32.Point) {
	maxWidth := gtx.Metric.Dp(460)
	if maxWidth > gtx.Constraints.Max.X-16 {
		maxWidth = max(0, gtx.Constraints.Max.X-16)
	}
	if maxWidth == 0 {
		return
	}

	contentContext := gtx
	contentContext.Constraints.Min = image.Point{}
	contentContext.Constraints.Max = image.Pt(maxWidth, gtx.Metric.Dp(140))
	macro := op.Record(gtx.Ops)
	dims := layout.UniformInset(8).Layout(contentContext, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme, help.Mnemonic+" — "+help.Description)
				label.Font.Weight = font.Bold
				label.Color = ui.Colors.Text
				label.TextSize = ui.TextHeight * 9 / 10
				return label.Layout(gtx)
			}),
		}
		if help.Explanation != "" {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme, help.Explanation)
				label.Font.Typeface = "override-monospace,Go,monospace"
				label.Color = ui.Syntax.Plain
				label.TextSize = ui.TextHeight * 9 / 10
				return layout.Inset{Top: 5}.Layout(gtx, label.Layout)
			}))
		}
		if len(help.Ports) > 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme, "ports: "+strings.Join(help.Ports, ", "))
				label.Font.Typeface = "override-monospace,Go,monospace"
				label.Color = ui.Syntax.Comment
				label.TextSize = ui.TextHeight * 8 / 10
				return layout.Inset{Top: 5}.Layout(gtx, label.Layout)
			}))
		}
		if help.Note != "" {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme, help.Note)
				label.Font.Style = font.Italic
				label.Color = ui.Syntax.Comment
				label.TextSize = ui.TextHeight * 8 / 10
				return layout.Inset{Top: 5}.Layout(gtx, label.Layout)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
	call := macro.Stop()

	left := int(position.X) + gtx.Metric.Dp(12)
	top := int(position.Y) + gtx.Metric.Dp(18)
	if left+dims.Size.X > gtx.Constraints.Max.X-4 {
		left = gtx.Constraints.Max.X - dims.Size.X - 4
	}
	if top+dims.Size.Y > gtx.Constraints.Max.Y-4 {
		top = int(position.Y) - dims.Size.Y - gtx.Metric.Dp(8)
	}
	left = max(4, left)
	top = max(4, top)

	stack := op.Offset(image.Pt(left, top)).Push(gtx.Ops)
	paint.FillShape(gtx.Ops, ui.Colors.SecondaryBackground, clip.UniformRRect(image.Rectangle{Max: dims.Size}, 5).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, ui.Colors.Splitter, clip.Stroke{
		Path:  clip.UniformRRect(image.Rectangle{Max: dims.Size}, 5).Path(gtx.Ops),
		Width: 1,
	}.Op())
	call.Add(gtx.Ops)
	stack.Pop()
}

func (ui CodeUIStyle) callTargetHit(gtx layout.Context, inst disasm.Inst, left int, x float32) bool {
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

func (ui CodeUIStyle) measureAsmTextWidth(gtx layout.Context, f font.Font, text string) int {
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

func (ui CodeUIStyle) asmCoord(view CodeView, inst disasm.Inst) comments.Coord {
	name := ""
	if ui.Code != nil {
		name = ui.Code.Name
	}
	cview, _ := view.CommentView()
	return comments.Coord{Function: name, View: cview, PC: inst.PC}
}

func (ui CodeUIStyle) sourceCoord(file string, line int) comments.Coord {
	name := ""
	if ui.Code != nil {
		name = ui.Code.Name
	}
	return comments.Coord{Function: name, View: comments.ViewSource, File: file, Line: line}
}

// commentEditKey identifies which comment the shared inline editor is
// bound to, so moving to a different line reloads the editor text.
func commentEditKey(coord comments.Coord) string {
	if coord.View == comments.ViewSource {
		return string(coord.View) + ":" + coord.Function + ":" + coord.File + ":" + strconv.Itoa(coord.Line)
	}
	return string(coord.View) + ":" + coord.Function + ":" + comments.FormatPC(coord.PC)
}

// layoutInlineCommentEditor draws the comment for coord, either as an
// editable field (when the line is selected and editing is wired up) or
// read-only. prefix is the leading marker: ";" for assembly, "//" for
// source.
func (ui CodeUIStyle) layoutInlineCommentEditor(gtx layout.Context, coord comments.Coord, prefix string, top, left, width, lineHeight int) {
	if ui.CommentEditor == nil || ui.CommentKey == nil || ui.SetComment == nil {
		comment := ui.Comments.Get(coord)
		if comment == "" {
			return
		}
		gui.SourceLine{
			TopLeft:    image.Pt(left, top),
			Width:      width,
			Text:       prefix + " " + comment,
			TextHeight: ui.TextHeight,
			Italic:     true,
			Color:      ui.Colors.MutedText,
		}.Layout(ui.Theme, gtx)
		return
	}

	key := commentEditKey(coord)
	if key != *ui.CommentKey {
		*ui.CommentKey = key
		ui.CommentEditor.SetText(ui.Comments.Get(coord))
	}

	changed := false
	for {
		ev, ok := ui.CommentEditor.Update(gtx)
		if !ok {
			break
		}
		switch ev.(type) {
		case widget.ChangeEvent, widget.SubmitEvent:
			changed = true
		}
	}
	if changed {
		ui.SetComment(coord, ui.CommentEditor.Text())
	}

	prefixWidth := lineHeight
	gui.SourceLine{
		TopLeft:    image.Pt(left, top),
		Width:      prefixWidth,
		Text:       prefix,
		TextHeight: ui.TextHeight,
		Color:      ui.Colors.MutedText,
	}.Layout(ui.Theme, gtx)

	editorLeft := left + prefixWidth
	editorWidth := max(width-prefixWidth, 0)
	stack := op.Offset(image.Pt(editorLeft, top)).Push(gtx.Ops)
	gtx.Constraints = layout.Exact(image.Pt(editorWidth, lineHeight))
	editor := material.Editor(ui.Theme, ui.CommentEditor, "comment")
	editor.TextSize = ui.TextHeight
	editor.Font.Typeface = "override-monospace,Go,monospace"
	editor.Color = ui.Colors.Text
	editor.Layout(gtx)
	stack.Pop()
}
