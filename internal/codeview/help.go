package codeview

import (
	"fmt"
	"image"
	"strconv"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget/material"

	"loov.dev/lensm/internal/asmhelp"
	"loov.dev/lensm/internal/gui"
)

// layoutHelp draws the instruction help tooltip for the hovered assembly
// line, when help is enabled and the user is not selecting or editing.
func (ui Style) layoutHelp(gtx layout.Context, c codeColumns, hover codeHover) {
	commentEditing := ui.CommentEditor != nil && gtx.Focused(ui.CommentEditor)
	if !ui.ShowHelp || ui.selecting || commentEditing || !gui.InRange(hover.asmIndex, len(ui.Code.Insts)) {
		return
	}
	inst := ui.Code.Insts[hover.asmIndex]
	nativeHovered := ui.ShowNative && c.native.Contains(hover.position.X)
	var help asmhelp.Help
	var ok bool
	if nativeHovered {
		help, ok = asmhelp.ForNative(ui.Code.Arch, inst.Mnemonic, inst.NativeText)
	} else {
		help, ok = asmhelp.ForInstruction(ui.Code.Arch, inst.Mnemonic, inst.Text)
	}
	if ok {
		ui.layoutAssemblyHelp(gtx, help, ui.UI.hl.perf[hover.asmIndex], hover.position)
	}
}

func (ui Style) layoutAssemblyHelp(gtx layout.Context, help asmhelp.Help, perf instPerf, position f32.Point) {
	maxWidth := gtx.Metric.Dp(460)
	if maxWidth > gtx.Constraints.Max.X-16 {
		maxWidth = max(0, gtx.Constraints.Max.X-16)
	}
	if maxWidth == 0 {
		return
	}

	contentContext := gtx
	contentContext.Constraints.Min = image.Point{}
	contentContext.Constraints.Max = image.Pt(maxWidth, gtx.Metric.Dp(140))
	macro := op.Record(gtx.Ops)
	dims := layout.UniformInset(8).Layout(contentContext, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme.Theme, help.Mnemonic+" — "+help.Description)
				label.Font.Weight = font.Bold
				label.Color = ui.Theme.Colors.Text
				label.TextSize = ui.TextHeight * 9 / 10
				return label.Layout(gtx)
			}),
		}
		if help.Explanation != "" {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme.Theme, help.Explanation)
				label.Font.Typeface = "override-monospace,Go,monospace"
				label.Color = ui.Syntax.Plain
				label.TextSize = ui.TextHeight * 9 / 10
				return layout.Inset{Top: 5}.Layout(gtx, label.Layout)
			}))
		}
		ports := strings.Join(help.Ports, ", ")
		if perf.ok {
			ports = perf.Ports
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme.Theme, perfLine(perf.Perf))
				label.Font.Typeface = "override-monospace,Go,monospace"
				label.Color = ui.Syntax.Comment
				label.TextSize = ui.TextHeight * 8 / 10
				return layout.Inset{Top: 5}.Layout(gtx, label.Layout)
			}))
		}
		if ports != "" {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme.Theme, "ports: "+ports)
				label.Font.Typeface = "override-monospace,Go,monospace"
				label.Color = ui.Syntax.Comment
				label.TextSize = ui.TextHeight * 8 / 10
				return layout.Inset{Top: 5}.Layout(gtx, label.Layout)
			}))
		}
		if help.Note != "" {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme.Theme, help.Note)
				label.Font.Style = font.Italic
				label.Color = ui.Syntax.Comment
				label.TextSize = ui.TextHeight * 8 / 10
				return layout.Inset{Top: 5}.Layout(gtx, label.Layout)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
	call := macro.Stop()

	left := int(position.X) + gtx.Metric.Dp(12)
	top := int(position.Y) + gtx.Metric.Dp(18)
	if left+dims.Size.X > gtx.Constraints.Max.X-4 {
		left = gtx.Constraints.Max.X - dims.Size.X - 4
	}
	if top+dims.Size.Y > gtx.Constraints.Max.Y-4 {
		top = int(position.Y) - dims.Size.Y - gtx.Metric.Dp(8)
	}
	left = max(4, left)
	top = max(4, top)

	stack := op.Offset(image.Pt(left, top)).Push(gtx.Ops)
	paint.FillShape(gtx.Ops, ui.Theme.Colors.SecondaryBackground, clip.UniformRRect(image.Rectangle{Max: dims.Size}, 5).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, ui.Theme.Colors.Splitter, clip.Stroke{
		Path:  clip.UniformRRect(image.Rectangle{Max: dims.Size}, 5).Path(gtx.Ops),
		Width: 1,
	}.Op())
	call.Add(gtx.Ops)
	stack.Pop()
}

// perfLine formats one instruction's measurement for the tooltip, e.g.
// "ADD (M64, R64) · 4 uops · lat 6 · tp 0.54 (ADL-P)".
func perfLine(p asmhelp.Perf) string {
	line := p.Form + " · " + uopsLabel(p.Uops)
	if p.Latency > 0 {
		line += fmt.Sprintf(" · lat %d", p.Latency)
	}
	return line + " · tp " + strconv.FormatFloat(p.TP, 'g', 3, 64) + " (" + p.Arch + ")"
}

func uopsLabel(n int) string {
	if n == 1 {
		return "1 uop"
	}
	return strconv.Itoa(n) + " uops"
}

// layoutSelectionPerf pins a summary strip to the bottom of the assembly
// columns while assembly lines are selected: instruction count, total uops,
// and the cycle bracket for the block. The sum of reciprocal throughputs is
// the lower bound (every instruction independent), the sum of latencies the
// upper bound (one serial dependency chain); real code lands in between.
func (ui Style) layoutSelectionPerf(gtx layout.Context, c codeColumns) {
	if ui.Selection.View != ViewGoAsm && ui.Selection.View != ViewNativeAsm {
		return
	}
	from, to, ok := ui.Selection.Range()
	if !ok {
		return
	}
	from = max(from, 0)
	to = min(to, len(ui.Code.Insts)-1)

	var instructions, uops, missing int
	var parallel, serial float64
	arch := ""
	for i := from; i <= to; i++ {
		if ui.Code.Insts[i].Text == "" {
			continue
		}
		instructions++
		p := ui.UI.hl.perf[i]
		if !p.ok {
			missing++
			continue
		}
		arch = p.Arch
		uops += p.Uops
		parallel += p.TP
		serial += p.SerialCycles()
	}
	if instructions == 0 || instructions == missing {
		return
	}

	text := fmt.Sprintf("%d instructions · %s · ≈%.1f cycles parallel / ≈%.1f serial · %s",
		instructions, uopsLabel(uops), parallel, serial, arch)
	if missing > 0 {
		text += fmt.Sprintf(" · %d no data", missing)
	}

	left := int(c.asm.Min)
	right := int(c.asm.Max)
	if ui.ShowNative {
		right = int(c.native.Max)
	}
	height := c.lineHeight + c.pad/2
	top := gtx.Constraints.Max.Y - height
	paint.FillShape(gtx.Ops, ui.Theme.Colors.SecondaryBackground, clip.Rect{
		Min: image.Pt(left, top),
		Max: image.Pt(right, gtx.Constraints.Max.Y),
	}.Op())
	paint.FillShape(gtx.Ops, ui.Theme.Colors.Splitter, clip.Rect{
		Min: image.Pt(left, top),
		Max: image.Pt(right, top+1),
	}.Op())
	gui.SourceLine{
		TopLeft:    image.Pt(left+c.pad/2, top+c.pad/4),
		Width:      right - left - c.pad,
		Text:       text,
		TextHeight: ui.TextHeight,
		Color:      ui.Theme.Colors.Text,
	}.Layout(ui.Theme.Theme, gtx)
}
