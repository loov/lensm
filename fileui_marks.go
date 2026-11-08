package main

import (
	"fmt"
	"path/filepath"
	"strconv"

	"gioui.org/layout"

	"loov.dev/lensm/internal/codeview"
	"loov.dev/lensm/internal/comments"
	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gui"
)

// markTitles name the marks in disasm.Marks order.
var markTitles = [...]string{"Spills", "Reloads", "Bounds checks", "Allocations", "Write barriers"}

// layoutMarksPanel lists the run time cost marks of the open function,
// with a switch per kind for showing it in the code view.
func (ui *FileUI) layoutMarksPanel(gtx layout.Context) layout.Dimensions {
	th := ui.Theme
	code := ui.activeCode()
	shown := disasm.AllMarks &^ ui.Settings.HiddenMarks

	for i, m := range disasm.Marks {
		ui.MarkToggles[i].Value = shown&m != 0
		if ui.MarkToggles[i].Update(gtx) {
			s := ui.Settings
			s.HiddenMarks ^= m
			ui.applySettings(s)
			shown = disasm.AllMarks &^ s.HiddenMarks
		}
	}

	var counts [len(markTitles)]int
	var found []int
	if code != nil && code.Code != nil {
		for i, ix := range code.Code.Insts {
			for k, m := range disasm.Marks {
				if ix.Marks&m != 0 {
					counts[k]++
				}
			}
			if ix.Marks&shown != 0 {
				found = append(found, i)
			}
		}
	}

	if i, ok := ui.MarksList.Clicked(gtx); ok && i < len(found) && code != nil && code.Code != nil {
		view := comments.ViewGoAsm
		if !ui.ShowGoAsm.Value {
			view = comments.ViewNativeAsm
		}
		code.Reveal(comments.Coord{View: view, PC: code.Code.Insts[found[i]].PC})
		ui.keepActiveTab()
	}

	return ui.panelFrame(gtx, "DIAGNOSTICS", nil, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{}
		for i, m := range disasm.Marks {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return layout.Inset{Left: 16, Right: 16}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Alignment: layout.Middle, Spacing: layout.SpaceBetween}.Layout(gtx,
						layout.Rigid(gui.Checkbox{Theme: th, Value: &ui.MarkToggles[i], Label: markTitles[i]}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							label := th.Label(strconv.Itoa(counts[i]), 0.9)
							label.Color = codeview.MarkColor(th.Colors.Marks, m)
							return label.Layout(gtx)
						}),
					)
				})
			}))
		}
		children = append(children,
			layout.Rigid(layout.Spacer{Height: 8}.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				if code == nil || code.Code == nil {
					return layout.Inset{Left: 16, Right: 16}.Layout(gtx, th.Muted("Open a function to see its spills, bounds checks and allocations.", 0.9).Layout)
				}
				return ui.MarksList.Layout(gtx, th, len(found), -1, func(i int) (string, string) {
					ix := code.Code.Insts[found[i]]
					var kinds string
					for _, m := range disasm.Marks {
						if ix.Marks&shown&m != 0 {
							if kinds != "" {
								kinds += ", "
							}
							kinds += m.String()
						}
					}
					return ix.Text, fmt.Sprintf("%s  %s:%d", kinds, filepath.Base(ix.File), ix.Line)
				})
			}),
		)
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}
