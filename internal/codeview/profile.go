package codeview

import (
	"fmt"
	"image"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
)

// profileShown reports whether any instruction has samples, so that
// the profile lanes take room.
func (ui Style) profileShown() bool {
	if ui.Profile == nil {
		return false
	}
	for _, ix := range ui.Code.Insts {
		if ix.Text != "" && ui.Profile[ix.PC] > 0 {
			return true
		}
	}
	return false
}

// profileLines sums the samples of each source line's instructions,
// and returns the most on one instruction and on one line.
func (ui Style) profileLines() (lines map[sourcePos]int64, maxInst, maxLine int64) {
	lines = map[sourcePos]int64{}
	for _, ix := range ui.Code.Insts {
		if ix.Text == "" {
			continue
		}
		v := ui.Profile[ix.PC]
		if v == 0 {
			continue
		}
		maxInst = max(maxInst, v)
		pos := sourcePos{ix.File, ix.Line}
		lines[pos] += v
		maxLine = max(maxLine, lines[pos])
	}
	return lines, maxInst, maxLine
}

// layoutHeat draws v as a bar growing left from the lane's right edge,
// as long as v is of most, with v's share of the profile over it.
func (ui Style) layoutHeat(gtx layout.Context, lane bounds, top, height int, v, most int64) {
	if v <= 0 || most <= 0 || ui.ProfileTotal <= 0 {
		return
	}
	frac := float32(v) / float32(most)
	col := ui.Theme.Colors.Marks.Alloc
	col.A = uint8(0x18 + 0x50*frac)
	inset := height / 10
	right := int(lane.Max)
	left := right - max(int(float32(lane.Width())*frac), 2)
	paint.FillShape(gtx.Ops, col, clip.Rect{Min: image.Pt(left, top+inset), Max: image.Pt(right, top+height-inset)}.Op())

	text := sourceLine{Text: percent(v, ui.ProfileTotal), TextHeight: ui.TextHeight * 0.85, Color: ui.Theme.Colors.Text}
	text.TopLeft = image.Pt(right-glyphWidth(gtx, ui.Theme.Theme, text)-height/4, top+height/12)
	text.Layout(ui.Theme.Theme, gtx)
}

// percent spells v of total for a narrow lane.
func percent(v, total int64) string {
	p := 100 * float64(v) / float64(total)
	if p < 0.1 {
		return "<0.1%"
	}
	return fmt.Sprintf("%.1f%%", p)
}

// layoutSourceHeat draws the samples of each source line in the lane
// left of the source.
func (ui Style) layoutSourceHeat(gtx layout.Context, c codeColumns, lines map[sourcePos]int64, most int64) {
	if c.sourceProfile.Width() == 0 {
		return
	}
	top := int(ui.src.Offset)
	for row := range sourceRows(ui.Code, ui.noteRows()) {
		rowTop := top
		top += c.lineHeight
		if row.kind != sourceRowLine || rowTop+c.lineHeight < 0 || rowTop > gtx.Constraints.Max.Y {
			continue
		}
		src := ui.Code.Source[row.file]
		ui.layoutHeat(gtx, c.sourceProfile, rowTop, c.lineHeight, lines[sourcePos{src.File, src.Blocks[row.block].From + row.off}], most)
	}
}
