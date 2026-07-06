package codeview

import (
	"image"
	"image/color"
	"strings"

	"gioui.org/f32"
	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/syntax"
)

type UI struct {
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
	SelectedView  View
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

func (ui *UI) Loaded() bool {
	return ui.Code != nil
}

func (ui *UI) ResetScroll() {
	ui.asm.scroll = 100000
	ui.src.scroll = 100000
}

type Style struct {
	*UI

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

func (ui Style) Layout(gtx layout.Context) layout.Dimensions {
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
	ui.UI.hl.update(ui.Code, ui.Syntax)

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
