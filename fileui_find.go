package main

import (
	"fmt"
	"strings"

	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/widget"

	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
)

// findBar searches the open function's shown columns.
type findBar struct {
	Open   bool
	Editor widget.Editor
	// at is the match last revealed, -1 before the first.
	at int
	// matches is for query in code with the columns in cols.
	matches []comments.Coord
	query   string
	code    *disasm.Code
	cols    [3]bool
}

// findMatches lists the rows of code that contain query, ignoring
// case: the instructions first, in the shown syntaxes, then source.
func findMatches(code *disasm.Code, query string, goAsm, native, source bool) []comments.Coord {
	query = strings.ToLower(query)
	if code == nil || query == "" {
		return nil
	}
	has := func(s string) bool { return strings.Contains(strings.ToLower(s), query) }
	var matches []comments.Coord
	for _, ix := range code.Insts {
		switch {
		case goAsm && has(ix.Text):
			matches = append(matches, comments.Coord{View: comments.ViewGoAsm, PC: ix.PC})
		case native && has(ix.NativeText):
			matches = append(matches, comments.Coord{View: comments.ViewNativeAsm, PC: ix.PC})
		}
	}
	if source {
		for _, src := range code.Source {
			for _, block := range src.Blocks {
				for i, line := range block.Lines {
					if has(line) {
						matches = append(matches, comments.Coord{View: comments.ViewSource, File: src.File, Line: block.From + i})
					}
				}
			}
		}
	}
	return matches
}

func (ui *FileUI) openFind(gtx layout.Context) {
	f := &ui.Find
	f.Open = true
	f.Editor.SingleLine = true
	f.Editor.Submit = true
	f.Editor.SetCaret(f.Editor.Len(), 0)
	gtx.Execute(key.FocusCmd{Tag: &f.Editor})
}

// stepFind reveals the next match, or the previous one for dir -1.
func (ui *FileUI) stepFind(dir int) {
	f := &ui.Find
	code := ui.activeCode()
	if !f.Open || code == nil || len(f.matches) == 0 {
		return
	}
	n := len(f.matches)
	if f.at < 0 && dir < 0 {
		f.at = 0
	}
	f.at = ((f.at+dir)%n + n) % n
	code.Reveal(f.matches[f.at])
}

// layoutFind draws the find bar under the crumbs while it is open.
func (ui *FileUI) layoutFind(gtx layout.Context) layout.Dimensions {
	f := &ui.Find
	code := ui.activeCode()
	if !f.Open || code == nil {
		return layout.Dimensions{}
	}
	for {
		ev, ok := gtx.Event(
			key.Filter{Focus: &f.Editor, Name: key.NameEscape},
			key.Filter{Focus: &f.Editor, Name: key.NameReturn, Required: key.ModShift},
		)
		if !ok {
			break
		}
		if ev, ok := ev.(key.Event); ok && ev.State == key.Press {
			switch ev.Name {
			case key.NameEscape:
				f.Open = false
				gtx.Execute(key.FocusCmd{Tag: code})
				return layout.Dimensions{}
			case key.NameReturn:
				ui.stepFind(-1)
			}
		}
	}
	for {
		ev, ok := f.Editor.Update(gtx)
		if !ok {
			break
		}
		if _, ok := ev.(widget.SubmitEvent); ok {
			ui.stepFind(1)
		}
	}
	cols := [3]bool{ui.ShowGoAsm.Value, ui.ShowNativeAsm.Value, ui.ShowSource.Value}
	if query := f.Editor.Text(); query != f.query || code.Code != f.code || cols != f.cols {
		f.query, f.code, f.cols = query, code.Code, cols
		f.matches = findMatches(code.Code, query, cols[0], cols[1], cols[2])
		f.at = -1
		// Show the first match while typing.
		ui.stepFind(1)
	}

	th := ui.Theme
	count := "No matches"
	switch {
	case f.query == "":
		count = ""
	case len(f.matches) > 0:
		count = fmt.Sprintf("%d of %d", max(f.at, 0)+1, len(f.matches))
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 6, Bottom: 6, Left: 14, Right: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = min(gtx.Metric.Dp(320), gtx.Constraints.Max.X)
						gtx.Constraints.Max.X = gtx.Constraints.Min.X
						return gui.SearchField(gtx, th, gtx.Focused(&f.Editor), gui.Editor(th, &f.Editor, "Find in function"))
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: 10}.Layout(gtx, th.Muted(count, 0.88).Layout)
					}),
				)
			})
		}),
		layout.Rigid(gui.HorizontalLine{Height: 1, Color: th.Colors.Divider}.Layout),
	)
}
