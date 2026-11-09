package main

import (
	"fmt"
	"image/color"
	"path/filepath"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget"

	"loov.dev/lensm/internal/asmdiff"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
)

// compareView shows the open function next to the same function in
// another build, with the instructions aligned.
type compareView struct {
	path string
	file disasm.File
	Stop widget.Clickable
	List widget.List

	// code is what the diff is for; old and new are the instruction
	// texts of the other build and of code.
	code     *disasm.Code
	old, new []string
	rows     []asmdiff.Row
	err      error
}

// setCompare compares with file, the binary at path, or stops for nil.
func (ui *FileUI) setCompare(path string, file disasm.File) {
	if ui.Compare.file != nil {
		_ = ui.Compare.file.Close()
	}
	ui.Compare = compareView{path: path, file: file}
}

// update diffs code, the function name, against the same function in
// the other build, once per code.
func (c *compareView) update(name string, code *disasm.Code, opts disasm.Options) {
	if c.code == code {
		return
	}
	c.code, c.old, c.new, c.rows, c.err = code, nil, nil, nil, nil
	fn := disasm.FindFunc(c.file, name)
	if fn == nil {
		c.err = fmt.Errorf("%s is not in %s", name, filepath.Base(c.path))
		return
	}
	other, err := fn.Load(opts)
	if err != nil {
		c.err = err
		return
	}
	c.old, c.new = instTexts(other), instTexts(code)
	c.rows = asmdiff.Diff(c.old, c.new)
}

// instTexts is the text of each instruction, without spacer rows.
func instTexts(code *disasm.Code) []string {
	var lines []string
	for _, ix := range code.Insts {
		if ix.Text != "" {
			lines = append(lines, ix.Text)
		}
	}
	return lines
}

// layoutCompare draws the other build's instructions left and the open
// one's right, removed rows red and added rows green.
func (ui *FileUI) layoutCompare(gtx layout.Context, tab *CodeTab) layout.Dimensions {
	c := &ui.Compare
	th := ui.Theme
	for c.Stop.Clicked(gtx) {
		ui.setCompare("", nil)
		return layout.Dimensions{}
	}
	c.update(tab.Name, tab.Code.Code, ui.loadOptions())

	removed, added := 0, 0
	for _, r := range c.rows {
		switch r.Op {
		case asmdiff.Removed:
			removed++
		case asmdiff.Added:
			added++
		}
	}
	summary := fmt.Sprintf("%s → this build: %d removed, %d added", filepath.Base(c.path), removed, added)
	if c.err != nil {
		summary = c.err.Error()
	}
	removedTint, addedTint := th.Colors.Error, th.Colors.Positive
	removedTint.A, addedTint.A = 0x30, 0x30
	c.List.Axis = layout.Vertical

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 6, Bottom: 6, Left: 14, Right: 14}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, th.Muted(summary, 0.9).Layout),
					layout.Rigid(gui.Button{Theme: th, Clickable: &c.Stop, Text: "Stop comparing"}.Layout),
				)
			})
		}),
		layout.Rigid(gui.HorizontalLine{Height: 1, Color: th.Colors.Divider}.Layout),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return c.List.Layout(gtx, len(c.rows), func(gtx layout.Context, i int) layout.Dimensions {
				r := c.rows[i]
				var left, right string
				if r.Old >= 0 {
					left = c.old[r.Old]
				}
				if r.New >= 0 {
					right = c.new[r.New]
				}
				return layout.Flex{}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return ui.compareCell(gtx, left, r.Op == asmdiff.Removed, removedTint)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return ui.compareCell(gtx, right, r.Op == asmdiff.Added, addedTint)
					}),
				)
			})
		}),
	)
}

// compareCell draws one side of a row, over tint when changed.
func (ui *FileUI) compareCell(gtx layout.Context, s string, changed bool, tint color.NRGBA) layout.Dimensions {
	label := ui.Theme.Label(s, 1)
	label.Font.Typeface = gui.Monospace
	label.MaxLines = 1
	if !changed {
		label.Color = ui.Theme.Colors.MutedText
	}
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			if changed {
				paint.FillShape(gtx.Ops, tint, clip.Rect{Max: gtx.Constraints.Min}.Op())
			}
			return layout.Dimensions{Size: gtx.Constraints.Min}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: 1, Bottom: 1, Left: 14, Right: 14}.Layout(gtx, label.Layout)
		},
	)
}
