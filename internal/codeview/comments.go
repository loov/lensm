package codeview

import (
	"image"
	"strconv"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
)

func (ui Style) asmCoord(view View, inst disasm.Inst) comments.Coord {
	name := ""
	if ui.Code != nil {
		name = ui.Code.Name
	}
	cview, _ := view.CommentView()
	return comments.Coord{Function: name, View: cview, PC: inst.PC}
}

func (ui Style) sourceCoord(file string, line int) comments.Coord {
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
func (ui Style) layoutInlineCommentEditor(gtx layout.Context, coord comments.Coord, prefix string, top, left, width, lineHeight int) {
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
			Color:      ui.Theme.Colors.MutedText,
		}.Layout(ui.Theme.Theme, gtx)
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
		Color:      ui.Theme.Colors.MutedText,
	}.Layout(ui.Theme.Theme, gtx)

	editorLeft := left + prefixWidth
	editorWidth := max(width-prefixWidth, 0)
	stack := op.Offset(image.Pt(editorLeft, top)).Push(gtx.Ops)
	gtx.Constraints = layout.Exact(image.Pt(editorWidth, lineHeight))
	editor := material.Editor(ui.Theme.Theme, ui.CommentEditor, "comment")
	editor.TextSize = ui.TextHeight
	editor.Font.Typeface = "override-monospace,Go,monospace"
	editor.Color = ui.Theme.Colors.Text
	editor.Layout(gtx)
	stack.Pop()
}
