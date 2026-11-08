package codeview

import (
	"iter"
	"strings"

	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
)

// View is the pane a row belongs to. It is the comment store's view, so
// a selection maps to a comment without translation.
type View = comments.View

const (
	ViewNone      View = ""
	ViewGoAsm          = comments.ViewGoAsm
	ViewNativeAsm      = comments.ViewNativeAsm
	ViewSource         = comments.ViewSource
)

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
// file/block/line index into code.Source for header and line rows, and
// for a note row the line it is about. A long note wraps onto several
// rows, each with its part in noteText; the first draws the connector.
type sourceRow struct {
	kind             sourceRowKind
	file, block, off int
	text             string

	note      *disasm.Note
	noteText  string
	noteX     int
	noteFirst bool
}

// noteLayout picks the notes that get rows. places holds where each
// note starts and how it wraps, measured by Layout; a note without one
// gets a single row.
type noteLayout struct {
	kinds  disasm.NoteKind
	places map[*disasm.Note]notePlace
}

// notePlace is where a note's connector sits, in pixels from the left
// of the source column, and the note's text broken into rows.
type notePlace struct {
	x    int
	rows []string
}

// noteColumn is the 0-based column in the drawn line of the compiler's
// 1-based column col. The source has each tab expanded to four spaces,
// the compiler counts it once. gofmt indents with tabs and aligns with
// spaces, so only the indentation needs converting.
func noteColumn(line string, col int) int {
	col--
	tabs := (len(line) - len(strings.TrimLeft(line, " "))) / 4
	return max(min(col+3*min(tabs, max(col, 0)), len(line)), 0)
}

// wrapText breaks s at spaces into the longest rows that fit, or
// mid-word when a word alone does not fit.
func wrapText(s string, fits func(string) bool) []string {
	var rows []string
	r := []rune(s)
	for len(r) > 0 && !fits(string(r)) {
		// Binary search the longest prefix that fits, at least one
		// rune; measuring is the slow part.
		n, hi := 1, len(r)-1
		for n < hi {
			mid := (n + hi + 1) / 2
			if fits(string(r[:mid])) {
				n = mid
			} else {
				hi = mid - 1
			}
		}
		cut, next := n, n
		if n < len(r) {
			for i := n; i > 0; i-- {
				if r[i] == ' ' {
					cut, next = i, i+1
					break
				}
			}
		}
		rows = append(rows, string(r[:cut]))
		r = r[next:]
	}
	if len(r) > 0 || len(rows) == 0 {
		rows = append(rows, string(r))
	}
	return rows
}

type sourceRowKind int

const (
	sourceRowGap    sourceRowKind = iota // blank row between files or blocks
	sourceRowHeader                      // "// path/to/file.go"
	sourceRowLine                        // a source line
	sourceRowNote                        // a compiler note below its line
)

// sourceRows enumerates the rows of the source column in display order:
// a gap between files, the file header, a gap between blocks, then the
// lines of each block, each followed by its notes of the kinds in notes.
// Every consumer of the source column — drawing,
// hit testing, copying — must agree on this order, so it lives here once.
func sourceRows(code *disasm.Code, notes noteLayout) iter.Seq[sourceRow] {
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
					if off >= len(block.Notes) {
						continue
					}
					indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
					for n := range block.Notes[off] {
						note := &block.Notes[off][n]
						if note.Kind()&notes.kinds == 0 {
							continue
						}
						place, ok := notes.places[note]
						if !ok {
							place.rows = []string{note.Text()}
						}
						for part, text := range place.rows {
							row := sourceRow{
								kind: sourceRowNote, file: i, block: k, off: off,
								text: indent + "// " + text,
								note: note, noteText: text, noteX: place.x, noteFirst: part == 0,
							}
							if !yield(row) {
								return
							}
						}
					}
				}
			}
		}
	}
}

func sourceTextRows(code *disasm.Code, notes noteLayout) []string {
	var rows []string
	for row := range sourceRows(code, notes) {
		rows = append(rows, row.text)
	}
	return rows
}

// sourceRowCount runs on every pointer event; it walks the structure
// without building any strings.
func sourceRowCount(code *disasm.Code, notes noteLayout) int {
	count := 0
	for range sourceRows(code, notes) {
		count++
	}
	return count
}

func sourceRowAtY(code *disasm.Code, notes noteLayout, scroll float32, lineHeight int, y float32) int {
	if code == nil || lineHeight <= 0 {
		return -1
	}
	relative := y - scroll
	if relative < 0 {
		return -1
	}
	row := int(relative / float32(lineHeight))
	if row < 0 || row >= sourceRowCount(code, notes) {
		return -1
	}
	return row
}

func (s TextSelection) Text(code *disasm.Code, notes noteLayout) string {
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
			text := strings.Repeat(" ", code.Insts[i].Indent) + code.Insts[i].Text
			if s.View == ViewNativeAsm {
				text = strings.ToUpper(code.Insts[i].NativeText)
			}
			lines = append(lines, text)
		}
	case ViewSource:
		rows := sourceTextRows(code, notes)
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
