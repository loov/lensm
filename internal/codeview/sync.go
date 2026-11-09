package codeview

import (
	"sort"

	"gioui.org/layout"
)

// scrollAnchor pairs an instruction row with the source row it was
// compiled from.
type scrollAnchor struct{ asm, src int }

// scrollAnchors pair each source line with the first run of its
// instructions. Later runs of a line, and pairs that would move one
// column up while the other moves down, are left out, since following
// them makes the source jump back and forth while the assembly scrolls
// down.
func (ui *UI) scrollAnchors() []scrollAnchor {
	rows := map[sourcePos]int{}
	row := 0
	for r := range sourceRows(ui.Code, ui.noteRows()) {
		if r.kind == sourceRowLine {
			src := ui.Code.Source[r.file]
			rows[sourcePos{src.File, src.Blocks[r.block].From + r.off}] = row
		}
		row++
	}
	seen := map[sourcePos]bool{}
	var anchors []scrollAnchor
	for i, ix := range ui.Code.Insts {
		pos := sourcePos{ix.File, ix.Line}
		if seen[pos] {
			continue
		}
		seen[pos] = true
		s, ok := rows[pos]
		if !ok {
			continue
		}
		if n := len(anchors); n > 0 && s <= anchors[n-1].src {
			continue
		}
		anchors = append(anchors, scrollAnchor{i, s})
	}
	return anchors
}

// followRow maps row y of the leading column to the other column,
// linearly between anchors and one to one past either end. lead and
// follow pick the anchor's coordinate in each column.
func followRow(anchors []scrollAnchor, lead, follow func(scrollAnchor) int, y float32) float32 {
	if len(anchors) == 0 {
		return y
	}
	i := sort.Search(len(anchors), func(i int) bool { return float32(lead(anchors[i])) > y })
	if i == 0 || i == len(anchors) {
		a := anchors[max(i-1, 0)]
		return y - float32(lead(a)) + float32(follow(a))
	}
	a, b := anchors[i-1], anchors[i]
	t := (y - float32(lead(a))) / float32(lead(b)-lead(a))
	return float32(follow(a)) + t*float32(follow(b)-follow(a))
}

func anchorAsm(a scrollAnchor) int { return a.asm }
func anchorSrc(a scrollAnchor) int { return a.src }

// syncScroll scrolls the column that did not move so the rows at the
// top of both views were compiled from each other. fromAsm says the
// assembly moved.
func (ui Style) syncScroll(gtx layout.Context, c codeColumns, fromAsm bool) {
	if !ui.SyncScroll || !ui.ShowSource || !(ui.ShowGo || ui.ShowNative) {
		return
	}
	lh := float32(c.lineHeight)
	anchors := ui.scrollAnchors()
	overflow := lh
	if fromAsm {
		y := followRow(anchors, anchorAsm, anchorSrc, -ui.asm.Offset/lh)
		ui.src.Anim.Stop()
		ui.src.Offset = -y * lh
		ui.src.Clamp(gtx, -overflow, float32(sourceRowCount(ui.Code, ui.noteRows()))*lh+overflow)
	} else {
		y := followRow(anchors, anchorSrc, anchorAsm, -ui.src.Offset/lh)
		ui.asm.Anim.Stop()
		ui.asm.Offset = -y * lh
		ui.asm.Clamp(gtx, -overflow, float32(len(ui.Code.Insts))*lh+overflow)
	}
}
