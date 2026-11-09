package codeview

import (
	"fmt"
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"

	"loov.dev/lensm/internal/pprof"
)

// profileRows are the profile's samples on each instruction row:
// flat is in the instruction itself, below in what it calls.
type profileRows struct {
	flat, below []int64
	// The most of each on one row, for the bars.
	maxFlat, maxBelow int64
	// lines sums flat and below per source line, as pprof's list does,
	// and maxLine is the most on one line.
	lines   map[sourcePos]int64
	maxLine int64
}

// profileRows spreads the profile over the instructions; ok is false
// when none has samples.
func (ui Style) profileRows() (rows profileRows, ok bool) {
	if ui.Profile == nil {
		return rows, false
	}
	insts := ui.Code.Insts
	rows.flat = make([]int64, len(insts))
	rows.below = make([]int64, len(insts))
	rows.lines = map[sourcePos]int64{}
	next := uint64(0)
	for i := len(insts) - 1; i >= 0; i-- {
		ix := insts[i]
		if ix.Text == "" {
			continue
		}
		end := next
		if end <= ix.PC {
			// The last instruction: a call there is at most this long.
			end = ix.PC + 16
		}
		next = ix.PC
		rows.flat[i] = ui.Profile[ix.PC]
		// The address a stack holds for a call lies inside it.
		rows.below[i] = pprof.Sum(ui.ProfileCalls, ix.PC, end)
		rows.maxFlat = max(rows.maxFlat, rows.flat[i])
		rows.maxBelow = max(rows.maxBelow, rows.below[i])
		if v := rows.flat[i] + rows.below[i]; v > 0 {
			pos := sourcePos{ix.File, ix.Line}
			rows.lines[pos] += v
			rows.maxLine = max(rows.maxLine, rows.lines[pos])
		}
	}
	return rows, rows.maxFlat > 0 || rows.maxBelow > 0
}

// profileShown reports whether the profile lanes take room.
func (ui Style) profileShown() bool {
	_, ok := ui.profileRows()
	return ok
}

// layoutProfileRow draws row i's samples: a call's time below it in
// the accent colour, or else the time in the instruction itself.
func (ui Style) layoutProfileRow(gtx layout.Context, c codeColumns, rows profileRows, i, top int) {
	if rows.below[i] > rows.flat[i] {
		ui.layoutHeat(gtx, c.profile, top, c.lineHeight, rows.below[i], rows.maxBelow, ui.Theme.Colors.Accent, "↳")
		return
	}
	ui.layoutHeat(gtx, c.profile, top, c.lineHeight, rows.flat[i], rows.maxFlat, ui.Theme.Colors.Marks.Alloc, "")
}

// layoutHeat draws v as a bar of col growing left from the lane's
// right edge, as long as v is of most, with v's share of the profile,
// after prefix, over it.
func (ui Style) layoutHeat(gtx layout.Context, lane bounds, top, height int, v, most int64, col color.NRGBA, prefix string) {
	if v <= 0 || most <= 0 || ui.ProfileTotal <= 0 {
		return
	}
	frac := float32(v) / float32(most)
	col.A = uint8(0x18 + 0x50*frac)
	inset := height / 10
	right := int(lane.Max)
	left := right - max(int(float32(lane.Width())*frac), 2)
	paint.FillShape(gtx.Ops, col, clip.Rect{Min: image.Pt(left, top+inset), Max: image.Pt(right, top+height-inset)}.Op())

	text := sourceLine{Text: prefix + percent(v, ui.ProfileTotal), TextHeight: ui.TextHeight * 0.85, Color: ui.Theme.Colors.Text}
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

// layoutSourceHeat draws the samples of each source line, with what
// its calls take, in the lane left of the source.
func (ui Style) layoutSourceHeat(gtx layout.Context, c codeColumns, rows profileRows) {
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
		v := rows.lines[sourcePos{src.File, src.Blocks[row.block].From + row.off}]
		ui.layoutHeat(gtx, c.sourceProfile, rowTop, c.lineHeight, v, rows.maxLine, ui.Theme.Colors.Marks.Alloc, "")
	}
}
