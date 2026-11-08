package codeview

import (
	"gioui.org/layout"
	"gioui.org/unit"

	"loov.dev/lensm/internal/disasm"
)

// noteScale is the size of note text relative to the code.
const noteScale = 0.9

// minNoteWidth is the room a note keeps, in line heights, moving left
// of its column when the column is far right.
const minNoteWidth = 12

// noteGap is the room for the connector before a note's text.
func noteGap(c codeColumns) int { return c.lineHeight * 3 / 4 }

type notePlaceKey struct {
	code  *disasm.Code
	kinds disasm.NoteKind
	width int
	size  unit.Sp
}

// placeNotes measures where each shown note starts and how it wraps.
// The code font need not be monospaced, so it measures the line up to
// the note's column. Hit testing between frames reuses the result.
func (ui Style) placeNotes(gtx layout.Context, c codeColumns) {
	key := notePlaceKey{ui.Code, ui.Notes, int(c.source.Width()), ui.TextHeight}
	if key == ui.noteKey && ui.notePlaces != nil {
		return
	}
	ui.noteKey = key
	ui.notePlaces = map[*disasm.Note]notePlace{}
	if ui.Code == nil || ui.Notes == 0 {
		return
	}
	th := ui.Theme.Theme
	width := int(c.source.Width())
	gap := noteGap(c)
	minWidth := minNoteWidth * c.lineHeight
	for i, src := range ui.Code.Source {
		for k, block := range src.Blocks {
			for off, notes := range block.Notes {
				line := block.Lines[off]
				var prefix string
				if spans := ui.hl.source[i][k][off]; len(spans) > 0 {
					prefix = spans[0].Text
				}
				for n := range notes {
					note := &notes[n]
					if note.Kind()&ui.Notes == 0 {
						continue
					}
					text := prefix + line[:noteColumn(line, note.Col)]
					x := glyphWidth(gtx, th, sourceLine{Text: text, TextHeight: ui.TextHeight})
					x = max(min(x, width-gap-minWidth), 0)
					room := width - x - gap
					fits := func(s string) bool {
						return glyphWidth(gtx, th, sourceLine{Text: s, TextHeight: ui.TextHeight * noteScale, Italic: true}) <= room
					}
					ui.notePlaces[note] = notePlace{x: x, rows: wrapText(note.Text(), fits)}
				}
			}
		}
	}
}
