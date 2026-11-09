package main

import (
	"fmt"
	"os"
	"path/filepath"

	"gioui.org/layout"
	"gioui.org/op"

	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/gui"
)

func (ui *FileUI) loadCommentsForPath(exePath string) {
	// Write out anything buffered for the previous binary first.
	if err := ui.Comments.Flush(); err != nil {
		ui.saveError = "comments for the previous binary not saved: " + err.Error()
		fmt.Fprintln(os.Stderr, ui.saveError)
	}
	// On error the store still works in memory and refuses to overwrite
	// the unreadable file; say so, since edits won't reach disk.
	store, err := comments.Open(ui.Config.CommentsPath, exePath)
	if err != nil {
		ui.saveError = "comments not loaded, edits won't be saved: " + err.Error()
		fmt.Fprintln(os.Stderr, ui.saveError)
	}
	ui.Comments = store
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

func (ui *FileUI) layoutCommentsPanel(gtx layout.Context) layout.Dimensions {
	records := ui.Comments.All()
	if i, ok := ui.CommentsList.Clicked(gtx); ok && i < len(records) {
		if tab := ui.openTab(ui.findFunc(records[i].Function), false); tab != nil {
			tab.Code.Reveal(records[i].Coord)
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	// Clearing takes two clicks; moving off the button cancels.
	if !ui.ClearComments.Hovered() {
		ui.clearCommentsArmed = false
	}
	for ui.ClearComments.Clicked(gtx) {
		if ui.clearCommentsArmed {
			for _, rec := range records {
				ui.setBufferedComment(rec.Coord, "")
			}
			records = nil
		}
		ui.clearCommentsArmed = !ui.clearCommentsArmed
	}
	var clear layout.Widget
	if len(records) > 0 {
		text := "Clear all"
		if ui.clearCommentsArmed {
			text = "Click again to clear"
		}
		clear = gui.Button{Theme: ui.Theme, Clickable: &ui.ClearComments, Text: text}.Layout
	}
	return gui.PanelFrame{Theme: ui.Theme, Title: "COMMENTS", Action: clear}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		if len(records) == 0 {
			return gui.PanelNote(ui.Theme, "No comments yet. Double-click a line in the code to add one.")(gtx)
		}
		return ui.CommentsList.Layout(gtx, ui.Theme, len(records), -1, func(i int) (string, string) {
			rec := records[i]
			_, name := gui.SplitPackage(rec.Function)
			where := name
			switch rec.View {
			case comments.ViewSource:
				where = fmt.Sprintf("%s  %s:%d", name, filepath.Base(rec.File), rec.Line)
			default:
				where = fmt.Sprintf("%s  %s", name, comments.FormatPC(rec.PC))
			}
			return rec.Text, where
		})
	})
}
