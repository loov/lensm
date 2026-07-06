package codeview

import (
	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/widget/material"
	"image"
	"loov.dev/lensm/internal/asmhelp"
	"loov.dev/lensm/internal/gui"
	"strings"
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
		ui.layoutAssemblyHelp(gtx, help, hover.position)
	}
}

func (ui Style) layoutAssemblyHelp(gtx layout.Context, help asmhelp.Help, position f32.Point) {
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
				label := material.Body1(ui.Theme, help.Mnemonic+" — "+help.Description)
				label.Font.Weight = font.Bold
				label.Color = ui.Colors.Text
				label.TextSize = ui.TextHeight * 9 / 10
				return label.Layout(gtx)
			}),
		}
		if help.Explanation != "" {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme, help.Explanation)
				label.Font.Typeface = "override-monospace,Go,monospace"
				label.Color = ui.Syntax.Plain
				label.TextSize = ui.TextHeight * 9 / 10
				return layout.Inset{Top: 5}.Layout(gtx, label.Layout)
			}))
		}
		if len(help.Ports) > 0 {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme, "ports: "+strings.Join(help.Ports, ", "))
				label.Font.Typeface = "override-monospace,Go,monospace"
				label.Color = ui.Syntax.Comment
				label.TextSize = ui.TextHeight * 8 / 10
				return layout.Inset{Top: 5}.Layout(gtx, label.Layout)
			}))
		}
		if help.Note != "" {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				label := material.Body1(ui.Theme, help.Note)
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
	paint.FillShape(gtx.Ops, ui.Colors.SecondaryBackground, clip.UniformRRect(image.Rectangle{Max: dims.Size}, 5).Op(gtx.Ops))
	paint.FillShape(gtx.Ops, ui.Colors.Splitter, clip.Stroke{
		Path:  clip.UniformRRect(image.Rectangle{Max: dims.Size}, 5).Path(gtx.Ops),
		Width: 1,
	}.Op())
	call.Add(gtx.Ops)
	stack.Pop()
}
