package main

import (
	"fmt"
	"os"

	"loov.dev/lensm/internal/comments"
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
