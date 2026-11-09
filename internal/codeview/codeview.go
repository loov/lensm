package codeview

import (
	"time"

	"image"
	"strings"

	"gioui.org/f32"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"

	"loov.dev/lensm/internal/asmhelp"
	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/syntax"
)

type UI struct {
	*disasm.Code

	asm scrollRegion
	src scrollRegion

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

	// lastClickRow and lastClickAt detect a double click, which opens
	// the comment editor on the row.
	lastClickRow string
	lastClickAt  time.Time

	// reveal is the row to scroll to on the next layout.
	reveal *comments.Coord

	// Notes are the kinds of compiler notes shown below source lines.
	Notes disasm.NoteKind
	// notePlaces caches where the shown notes go for noteKey.
	notePlaces map[*disasm.Note]notePlace
	noteKey    notePlaceKey
}

func (ui *UI) noteRows() noteLayout {
	return noteLayout{kinds: ui.Notes, places: ui.notePlaces}
}

// SelectedText is the selected rows as text.
func (ui *UI) SelectedText() string {
	return ui.Selection.Text(ui.Code, ui.noteRows())
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
	// perf is the per-instruction uops.info measurement; ok is false when
	// there is no data for the instruction (always on non-x86).
	perf []instPerf
	// source is indexed by source file, block, and line within the block.
	source [][][][]syntax.Span
	// lineMarks counts the marks of the instructions of each source line.
	lineMarks map[sourcePos]*lineMarks
}

// lineMarks are the marks found on one source line.
type lineMarks struct {
	marks  disasm.Mark
	counts markCounts
}

// markCounts counts each mark, in disasm.Marks order.
type markCounts [len(disasm.Marks)]int

type sourcePos struct {
	file string
	line int
}

type instPerf struct {
	asmhelp.Perf
	ok bool
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
	hl.perf = make([]instPerf, len(code.Insts))
	for i := range code.Insts {
		ix := &code.Insts[i]
		hl.asm[i] = syntax.HighlightAsm(ix.Text, ix.Call, palette)
		hl.nativeText[i] = strings.ToUpper(ix.NativeText)
		hl.native[i] = syntax.HighlightAsm(hl.nativeText[i], "", palette)
		if ix.NativeText != "" {
			p, ok := asmhelp.PerfForNative(code.Arch, ix.Mnemonic, ix.NativeText)
			hl.perf[i] = instPerf{p, ok}
		}
	}

	hl.lineMarks = map[sourcePos]*lineMarks{}
	for _, ix := range code.Insts {
		if ix.Marks == 0 {
			continue
		}
		pos := sourcePos{ix.File, ix.Line}
		line := hl.lineMarks[pos]
		if line == nil {
			line = &lineMarks{}
			hl.lineMarks[pos] = line
		}
		line.marks |= ix.Marks
		counted := ix.CountedMarks()
		for i, m := range disasm.Marks {
			if counted&m != 0 {
				line.counts[i]++
			}
		}
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
	ui.asm.Offset = 100000
	ui.src.Offset = 100000
}

// KeepView carries the scroll positions and selected row over from
// prev, the same function before the binary was reloaded.
func (ui *UI) KeepView(prev *UI) {
	ui.asm.Offset, ui.src.Offset = prev.asm.Offset, prev.src.Offset
	ui.SelectedAsm, ui.SelectedView = prev.SelectedAsm, prev.SelectedView
	ui.SelectedFile, ui.SelectedLine = prev.SelectedFile, prev.SelectedLine
}

// Reveal selects the row at c and scrolls it to the middle of the view
// on the next layout, which may come after the code finishes loading.
func (ui *UI) Reveal(c comments.Coord) {
	ui.reveal = &c
}

func (ui *UI) applyReveal(gtx layout.Context, lineHeight int) {
	c := ui.reveal
	ui.reveal = nil
	middle := func(region *scrollRegion, row, rows int) {
		region.Anim.Stop()
		region.Offset = float32(gtx.Constraints.Max.Y/2 - row*lineHeight - lineHeight/2)
		region.Clamp(gtx, 0, float32(rows*lineHeight))
	}
	if c.View == ViewSource {
		if row := ui.sourceLineRow(c.File, c.Line); row >= 0 {
			rows := sourceRowCount(ui.Code, ui.noteRows())
			ui.SelectedAsm, ui.SelectedView = -1, ViewSource
			ui.SelectedFile, ui.SelectedLine = c.File, c.Line
			middle(&ui.src, row, rows)
		}
		return
	}
	for i, ix := range ui.Code.Insts {
		if ix.PC == c.PC {
			ui.SelectedAsm, ui.SelectedView = i, c.View
			middle(&ui.asm, i, len(ui.Code.Insts))
			return
		}
	}
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
	// flush scheduling), and CommentEditor is the single shared inline
	// editor.
	Comments      *comments.Store
	SetComment    func(comments.Coord, string)
	CommentEditor *CommentEditor
	Theme         *gui.Theme
	Syntax        syntax.Palette

	// ShowGo, ShowNative and ShowSource pick the columns to draw.
	ShowGo     bool
	ShowNative bool
	ShowSource bool
	ShowHelp   bool
	// SyncScroll scrolls the assembly and the source together.
	SyncScroll bool
	// Marks picks the run time cost marks to show next to the assembly.
	Marks disasm.Mark
	// HoverLinksOnly draws only the hovered line's band between source
	// and assembly.
	HoverLinksOnly bool
	TextHeight     unit.Sp
}

func (ui Style) Layout(gtx layout.Context) layout.Dimensions {
	gtx.Constraints = layout.Exact(gtx.Constraints.Max)
	if ui.Code == nil {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	}
	ui.UI.hl.update(ui.Code, ui.Syntax)

	paint.FillShape(gtx.Ops, ui.Theme.Colors.Background, clip.Rect{Max: gtx.Constraints.Max}.Op())
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()

	// A jump longer than the view could not show both its ends, so it
	// is drawn as stubs.
	ui.Code.LayoutJumps(max(gtx.Constraints.Max.Y/codeLineHeightPx(gtx, ui.TextHeight)-1, 8))
	c := ui.columns(gtx)
	ui.placeNotes(gtx, c)
	if ui.reveal != nil {
		ui.applyReveal(gtx, c.lineHeight)
	}
	mouseClicked := ui.handleInput(gtx, c)

	// draw gutter
	paint.FillShape(gtx.Ops, ui.Theme.Colors.Gutter, clip.Rect{
		Min: image.Pt(int(c.gutter.Min), 0),
		Max: image.Pt(int(c.gutter.Max), gtx.Constraints.Max.Y),
	}.Op())
	if scroll, ok := ui.asm.Anim.Update(gtx); ok {
		ui.asm.Offset = scroll
	}
	if scroll, ok := ui.src.Anim.Update(gtx); ok {
		ui.src.Offset = scroll
	}

	hover := ui.resolveHover(gtx, c, mouseClicked)
	var highlightRanges []disasm.LineRange
	if ui.ShowSource && (ui.ShowGo || ui.ShowNative) {
		highlightRanges = ui.layoutRelations(gtx, c, hover)
	}
	if ui.ShowGo || ui.ShowNative {
		ui.layoutAssembly(gtx, c, hover, highlightRanges)
	}
	sourceContentHeight := 0
	if ui.ShowSource {
		sourceContentHeight = ui.layoutSource(gtx, c, hover, mouseClicked)
	}
	ui.layoutScrollbars(gtx, c, sourceContentHeight)
	ui.layoutSelectionPerf(gtx, c)
	ui.layoutHelp(gtx, c, hover)

	return layout.Dimensions{Size: gtx.Constraints.Max}
}
