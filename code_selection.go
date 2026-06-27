package main

import (
	"strings"

	"loov.dev/lensm/internal/disasm"
)

type CodeView uint8

const (
	CodeViewNone CodeView = iota
	CodeViewGoAsm
	CodeViewNativeAsm
	CodeViewSource
)

type TextSelection struct {
	View   CodeView
	Anchor int
	Head   int
	Active bool
}

func (selection *TextSelection) Clear() {
	*selection = TextSelection{}
}

func (selection *TextSelection) Begin(view CodeView, line int, extend bool) {
	if view == CodeViewNone || line < 0 {
		selection.Clear()
		return
	}
	if !extend || !selection.Active || selection.View != view {
		selection.View = view
		selection.Anchor = line
	}
	selection.Head = line
	selection.Active = true
}

func (selection *TextSelection) Extend(view CodeView, line int) {
	if !selection.Active || selection.View != view || line < 0 {
		return
	}
	selection.Head = line
}

func (selection TextSelection) Range() (from, to int, ok bool) {
	if !selection.Active {
		return 0, 0, false
	}
	from, to = selection.Anchor, selection.Head
	if from > to {
		from, to = to, from
	}
	return from, to, true
}

func (selection TextSelection) Contains(view CodeView, line int) bool {
	if selection.View != view {
		return false
	}
	from, to, ok := selection.Range()
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

func sourceRowAtY(code *disasm.Code, scroll float32, lineHeight int, y float32) int {
	if code == nil || lineHeight <= 0 {
		return -1
	}
	relative := y - scroll
	if relative < 0 {
		return -1
	}
	row := int(relative / float32(lineHeight))
	if row < 0 || row >= len(sourceTextRows(code)) {
		return -1
	}
	return row
}

func (selection TextSelection) Text(code *disasm.Code) string {
	if code == nil {
		return ""
	}
	from, to, ok := selection.Range()
	if !ok {
		return ""
	}

	var lines []string
	switch selection.View {
	case CodeViewGoAsm, CodeViewNativeAsm:
		if from < 0 {
			from = 0
		}
		if to >= len(code.Insts) {
			to = len(code.Insts) - 1
		}
		for i := from; i <= to; i++ {
			text := code.Insts[i].Text
			if selection.View == CodeViewNativeAsm {
				text = strings.ToUpper(code.Insts[i].NativeText)
			}
			lines = append(lines, text)
		}
	case CodeViewSource:
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
