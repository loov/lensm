package codeview

import (
	"iter"
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

// sourceRow is one row of the source column. kind tells what it shows;
// file/block/line index into code.Source for header and line rows.
type sourceRow struct {
	kind             sourceRowKind
	file, block, off int
	text             string
}

type sourceRowKind int

const (
	sourceRowGap    sourceRowKind = iota // blank row between files or blocks
	sourceRowHeader                      // "// path/to/file.go"
	sourceRowLine                        // a source line
)

// sourceRows enumerates the rows of the source column in display order:
// a gap between files, the file header, a gap between blocks, then the
// lines of each block. Every consumer of the source column — drawing,
// hit testing, copying — must agree on this order, so it lives here once.
func sourceRows(code *disasm.Code) iter.Seq[sourceRow] {
	return func(yield func(sourceRow) bool) {
		if code == nil {
			return
		}
		for i, src := range code.Source {
			if i > 0 && !yield(sourceRow{kind: sourceRowGap}) {
				return
			}
			if !yield(sourceRow{kind: sourceRowHeader, file: i, text: "// " + src.File}) {
				return
			}
			for k, block := range src.Blocks {
				if k > 0 && !yield(sourceRow{kind: sourceRowGap}) {
					return
				}
				for off, line := range block.Lines {
					if !yield(sourceRow{kind: sourceRowLine, file: i, block: k, off: off, text: line}) {
						return
					}
				}
			}
		}
	}
}

func sourceTextRows(code *disasm.Code) []string {
	var rows []string
	for row := range sourceRows(code) {
		rows = append(rows, row.text)
	}
	return rows
}

// sourceRowCount runs on every pointer event; it walks the structure
// without building any strings.
func sourceRowCount(code *disasm.Code) int {
	count := 0
	for range sourceRows(code) {
		count++
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
			lines = append(lines, rows[i])
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
