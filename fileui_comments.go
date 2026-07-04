package main

import (
	"fmt"
	"loov.dev/lensm/internal/disasm"
	"os"
	"loov.dev/lensm/internal/comments"
)

func (ui *FileUI) loadCommentsForPath(exePath string) {
	// Write out anything buffered for the previous binary first.
	if err := ui.Comments.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "unable to save comments: %v\n", err)
	}
	commentsPath := ui.Config.CommentsPath
	if commentsPath == "" {
		commentsPath = comments.DefaultPath(exePath)
	}
	store, err := comments.Open(commentsPath, exePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "unable to load comments from %q: %v\n", commentsPath, err)
		store, _ = comments.Open("", exePath)
	}
	ui.Comments = store
}

func (ui *FileUI) commentKeyFor(inst disasm.Inst) string {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return ""
	}
	return ui.commentKeyForCode(code.Code, inst)
}

func (ui *FileUI) commentKeyForCode(code *disasm.Code, inst disasm.Inst) string {
	if code == nil {
		return ""
	}
	// The view is prefixed by layoutInlineAsmCommentEditor.
	return code.Name + ":" + comments.FormatPC(inst.PC)
}

func (ui *FileUI) commentFor(inst disasm.Inst) string {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return ""
	}
	return ui.Comments.ForAsm(code.Name, comments.ViewGoAsm, inst.PC)
}

func (ui *FileUI) nativeCommentFor(inst disasm.Inst) string {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return ""
	}
	return ui.Comments.ForAsm(code.Name, comments.ViewNativeAsm, inst.PC)
}

func (ui *FileUI) sourceCommentFor(file string, line int) string {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return ""
	}
	return ui.Comments.ForSource(code.Name, file, line)
}

func (ui *FileUI) setCommentForInst(inst disasm.Inst, text string) {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return
	}
	ui.setBufferedComment(comments.Coord{
		Function: code.Name,
		View:     comments.ViewGoAsm,
		PC:       inst.PC,
	}, text)
}

// setBufferedComment records the comment in memory and schedules the
// disk write, so typing doesn't rewrite the sidecar per keystroke.
func (ui *FileUI) setBufferedComment(coord comments.Coord, text string) {
	if err := ui.Comments.SetBuffered(coord, text); err != nil {
		ui.saveError = "comment not saved: " + err.Error()
		fmt.Fprintln(os.Stderr, err)
		return
	}
	ui.scheduleFlush()
}

func (ui *FileUI) setNativeCommentForInst(inst disasm.Inst, text string) {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return
	}
	ui.setBufferedComment(comments.Coord{
		Function: code.Name,
		View:     comments.ViewNativeAsm,
		PC:       inst.PC,
	}, text)
}

func (ui *FileUI) setSourceCommentForLine(file string, line int, text string) {
	code := ui.activeCode()
	if code == nil || !code.Loaded() {
		return
	}
	ui.setBufferedComment(comments.Coord{
		Function: code.Name,
		View:     comments.ViewSource,
		File:     file,
		Line:     line,
	}, text)
}
