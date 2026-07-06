package codeview

import (
	"strings"

	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
)

type View uint8

const (
	ViewNone View = iota
	ViewGoAsm
	ViewNativeAsm
	ViewSource
)

// CommentView maps a pane to its comment-store view. ok is false for
// CodeViewNone, which has no comments.
func (v View) CommentView() (view comments.View, ok bool) {
	switch v {
	case ViewGoAsm:
		return comments.ViewGoAsm, true
	case ViewNativeAsm:
		return comments.ViewNativeAsm, true
	case ViewSource:
		return comments.ViewSource, true
	default:
		return "", false
	}
}

type TextSelection struct {
	View   View
	Anchor int
	Head   int
	Active bool
}

func (s *TextSelection) Clear() {
	*s = TextSelection{}
}

func (s *TextSelection) Begin(view View, line int, extend bool) {
	if view == ViewNone || line < 0 {
		s.Clear()
		return
	}
	if !extend || !s.Active || s.View != view {
		s.View = view
		s.Anchor = line
	}
	s.Head = line
	s.Active = true
}

func (s *TextSelection) Extend(view View, line int) {
	if !s.Active || s.View != view || line < 0 {
		return
	}
	s.Head = line
}

func (s TextSelection) Range() (from, to int, ok bool) {
	if !s.Active {
		return 0, 0, false
	}
	from, to = s.Anchor, s.Head
	if from > to {
		from, to = to, from
	}
	return from, to, true
}

func (s TextSelection) Contains(view View, line int) bool {
	if s.View != view {
		return false
	}
	from, to, ok := s.Range()
	return ok && from <= line && line <= to
}

type sourceTextRow struct {
	Text string
}

func sourceTextRows(code *disasm.Code) []sourceTextRow {
	if code == nil {
		return nil
	}
	var rows []sourceTextRow
	for sourceIndex, source := range code.Source {
		if sourceIndex > 0 {
			rows = append(rows, sourceTextRow{})
		}
		rows = append(rows, sourceTextRow{Text: "// " + source.File})
		for blockIndex, block := range source.Blocks {
			if blockIndex > 0 {
				rows = append(rows, sourceTextRow{})
			}
			for _, line := range block.Lines {
				rows = append(rows, sourceTextRow{Text: line})
			}
		}
	}
	return rows
}

// sourceRowCount mirrors the rows produced by sourceTextRows without
// building them; it runs on every pointer event.
func sourceRowCount(code *disasm.Code) int {
	if code == nil {
		return 0
	}
	count := 0
	for sourceIndex, source := range code.Source {
		if sourceIndex > 0 {
			count++
		}
		count++
		for blockIndex, block := range source.Blocks {
			if blockIndex > 0 {
				count++
			}
			count += len(block.Lines)
		}
	}
	return count
}

func sourceRowAtY(code *disasm.Code, scroll float32, lineHeight int, y float32) int {
	if code == nil || lineHeight <= 0 {
		return -1
	}
	relative := y - scroll
	if relative < 0 {
		return -1
	}
	row := int(relative / float32(lineHeight))
	if row < 0 || row >= sourceRowCount(code) {
		return -1
	}
	return row
}

func (s TextSelection) Text(code *disasm.Code) string {
	if code == nil {
		return ""
	}
	from, to, ok := s.Range()
	if !ok {
		return ""
	}

	var lines []string
	switch s.View {
	case ViewGoAsm, ViewNativeAsm:
		if from < 0 {
			from = 0
		}
		if to >= len(code.Insts) {
			to = len(code.Insts) - 1
		}
		for i := from; i <= to; i++ {
			text := code.Insts[i].Text
			if s.View == ViewNativeAsm {
				text = strings.ToUpper(code.Insts[i].NativeText)
			}
			lines = append(lines, text)
		}
	case ViewSource:
		rows := sourceTextRows(code)
		if from < 0 {
			from = 0
		}
		if to >= len(rows) {
			to = len(rows) - 1
		}
		for i := from; i <= to; i++ {
			lines = append(lines, rows[i].Text)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
