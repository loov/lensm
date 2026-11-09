package codeview

import (
	"math"

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

// followRow maps row y of the leading column to the other column.
// Each anchor says how many rows the other column is off from this
// one; the anchors near y are averaged with a Gaussian weight of width
// sigma rows, so the result moves smoothly as y does and one odd pair
// doesn't jerk it. Far from every anchor the nearest one decides.
func followRow(anchors []scrollAnchor, lead, follow func(scrollAnchor) int, y, sigma float32) float32 {
	if len(anchors) == 0 {
		return y
	}
	var sum, weights float64
	nearest, nearestDist := anchors[0], math.Inf(1)
	for _, a := range anchors {
		d := float64(lead(a)) - float64(y)
		w := math.Exp(-d * d / (2 * float64(sigma*sigma)))
		sum += w * float64(follow(a)-lead(a))
		weights += w
		if math.Abs(d) < nearestDist {
			nearest, nearestDist = a, math.Abs(d)
		}
	}
	if weights < 1e-9 {
		return y + float32(follow(nearest)-lead(nearest))
	}
	return y + float32(sum/weights)
}

func anchorAsm(a scrollAnchor) int { return a.asm }
func anchorSrc(a scrollAnchor) int { return a.src }

// syncScroll scrolls the column that did not move so the rows a third
// down both views, where people tend to read, were compiled from each
// other. fromAsm says the assembly moved.
func (ui Style) syncScroll(gtx layout.Context, c codeColumns, fromAsm bool) {
	if !ui.SyncScroll || !ui.ShowSource || !(ui.ShowGo || ui.ShowNative) {
		return
	}
	lh := float32(c.lineHeight)
	focus := float32(gtx.Constraints.Max.Y) / 3
	// Average over about a third of the view.
	sigma := max(float32(gtx.Constraints.Max.Y)/lh/6, 2)
	anchors := ui.scrollAnchors()
	overflow := lh
	if fromAsm {
		y := followRow(anchors, anchorAsm, anchorSrc, (focus-ui.asm.Offset)/lh, sigma)
		ui.src.Anim.Stop()
		ui.src.Offset = focus - y*lh
		ui.src.Clamp(gtx, -overflow, float32(sourceRowCount(ui.Code, ui.noteRows()))*lh+overflow)
	} else {
		y := followRow(anchors, anchorSrc, anchorAsm, (focus-ui.src.Offset)/lh, sigma)
		ui.asm.Anim.Stop()
		ui.asm.Offset = focus - y*lh
		ui.asm.Clamp(gtx, -overflow, float32(len(ui.Code.Insts))*lh+overflow)
	}
}
