package main

import (
	"fmt"
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
