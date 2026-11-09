package main

import (
	"cmp"
	"fmt"
	"image"
	"image/color"
	"math"
	"slices"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	glayout "github.com/loov/layout"

	"loov.dev/lensm/internal/gui"
	"loov.dev/lensm/internal/pprof"
)

// Limits on the call graph, so that it stays readable in the sidebar.
const (
	graphCallers  = 5
	graphCallees  = 6
	graphCallees2 = 3
	// graphMinShare is the least share of the samples a second level
	// call needs to be shown.
	graphMinShare = 0.005
)

// callGraph is the profile's call graph around the open function: its
// hottest callers above and its hottest callees, two calls deep, below.
type callGraph struct {
	// The graph is laid out for focus in prof, with text measured at
	// textSize.
	focus    string
	prof     *pprof.Profile
	textSize unit.Sp
	// full lays out the whole profile, as pprof's graph does, instead
	// of the calls around focus.
	full bool

	graph  *glayout.Graph
	layout *glayout.Layout
	// bounds is the extent of the layout, in dp.
	bounds glayout.Vector
	min    glayout.Vector

	// out and in are the profile's calls from and to each function,
	// hottest first.
	out, in map[string][]pprof.Edge
	outFor  *pprof.Profile

	offset f32.Point // pan, in px
	hover  int
	press  bool
	// drag is where a press started, and moved whether it has moved
	// far enough to pan instead of click.
	drag  f32.Point
	moved bool
	// zoom multiplies the fitted scale; 0 is 1.
	zoom float32
	// center asks draw to centre the focus; reveal only when it is
	// out of view, so a node clicked in the graph stays put.
	center, reveal bool
}

// calls indexes the edges of prof by caller and by callee.
func (g *callGraph) calls(prof *pprof.Profile) {
	if g.outFor == prof {
		return
	}
	g.outFor = prof
	g.out, g.in = map[string][]pprof.Edge{}, map[string][]pprof.Edge{}
	for e := range prof.Edges {
		if e.Caller == e.Callee {
			continue
		}
		g.out[e.Caller] = append(g.out[e.Caller], e)
		g.in[e.Callee] = append(g.in[e.Callee], e)
	}
	hottest := func(a, b pprof.Edge) int {
		return cmp.Or(cmp.Compare(prof.Edges[b], prof.Edges[a]), cmp.Compare(a.Caller+a.Callee, b.Caller+b.Callee))
	}
	for _, es := range g.out {
		slices.SortFunc(es, hottest)
	}
	for _, es := range g.in {
		slices.SortFunc(es, hottest)
	}
}

// build lays out the graph around focus.
func (g *callGraph) build(gtx layout.Context, th *gui.Theme, prof *pprof.Profile, focus string) {
	if g.prof == prof && g.textSize == th.TextSize && (g.focus == focus || g.full && g.layout != nil) {
		// The whole graph doesn't depend on focus; only bring it into
		// view, if it isn't already.
		g.reveal = g.reveal || g.focus != focus
		g.focus = focus
		return
	}
	g.focus, g.prof, g.textSize = focus, prof, th.TextSize
	g.layout, g.offset, g.hover, g.center, g.zoom = nil, f32.Point{}, -1, true, 0
	g.calls(prof)

	graph := glayout.NewDigraph()
	fontSize := glayout.Length(float32(th.TextSize) * 0.85)
	graph.FontSize = fontSize
	graph.LineHeight = fontSize * 1.3
	graph.Shape = glayout.Box
	graph.NodePadding = fontSize
	graph.RowPadding = fontSize * 2.5
	graph.MeasureText = func(line, _ string, size glayout.Length) glayout.Length {
		return glayout.Length(measureDp(gtx, th, line, unit.Sp(size)))
	}
	var maxFlat int64
	for _, f := range prof.Funcs {
		maxFlat = max(maxFlat, f.Flat)
	}
	node := func(name string) *glayout.Node {
		if n, ok := graph.NodeByID[name]; ok {
			return n
		}
		n := graph.Node(name)
		f := prof.Funcs[name]
		// Keep the last element of the package path: runtime.main.
		pkg, rest := gui.SplitPackage(name)
		pkg = pkg[strings.LastIndexByte(pkg, '/')+1:]
		if !g.full {
			n.Label = pkg + rest + "\n" + percentLabel(f.Cum, prof.Total)
			return n
		}
		// As pprof does, the text grows with the function's own samples.
		n.FontSize = fontSize * glayout.Length(1+1.5*math.Sqrt(float64(f.Flat)/float64(max(maxFlat, 1))))
		lines := []string{strings.TrimSuffix(pkg, "."), rest}
		if f.Flat == f.Cum {
			lines = append(lines, valueLabel(prof, f.Flat))
		} else {
			lines = append(lines, valueLabel(prof, f.Flat), "of "+valueLabel(prof, f.Cum))
		}
		if pkg == "" {
			lines = lines[1:]
		}
		n.Label = strings.Join(lines, "\n")
		// The layout uses one line height for every node; make room for
		// the larger text.
		n.MinSize.Y = n.FontSize * (1.3*glayout.Length(len(lines)) + 1)
		return n
	}
	seen := map[pprof.Edge]bool{}
	edge := func(e pprof.Edge) {
		if seen[e] {
			return
		}
		seen[e] = true
		le := glayout.NewEdge(node(e.Caller), node(e.Callee))
		le.Directed = true
		le.Label = percentLabel(prof.Edges[e], prof.Total)
		if g.full {
			le.Label = valueText(prof, prof.Edges[e])
		}
		le.FontSize = fontSize * 0.9
		graph.AddEdge(le)
	}
	if g.full {
		g.pickAll(prof, node, edge)
	} else {
		g.pickAround(prof, focus, node, edge)
	}

	l, err := glayout.Hierarchical(graph, glayout.Options{})
	if err != nil {
		return
	}
	g.graph, g.layout = graph, l
	lo := glayout.Vector{X: math.MaxFloat32, Y: math.MaxFloat32}
	hi := glayout.Vector{X: -math.MaxFloat32, Y: -math.MaxFloat32}
	grow := func(p glayout.Vector) {
		lo.X, lo.Y = min(lo.X, p.X), min(lo.Y, p.Y)
		hi.X, hi.Y = max(hi.X, p.X), max(hi.Y, p.Y)
	}
	for _, n := range l.Nodes {
		grow(n.Center.Sub(n.Size.Scale(0.5)))
		grow(n.Center.Add(n.Size.Scale(0.5)))
	}
	for _, e := range l.Edges {
		for _, p := range e.Path {
			grow(p)
		}
		if e.LabelSize != (glayout.Vector{}) {
			grow(e.LabelCenter.Sub(e.LabelSize.Scale(0.5)))
			grow(e.LabelCenter.Add(e.LabelSize.Scale(0.5)))
		}
	}
	g.min, g.bounds = lo, hi.Sub(lo)
}

// pickAround adds focus, its hottest callers and its hottest callees
// two calls deep.
func (g *callGraph) pickAround(prof *pprof.Profile, focus string, node func(string) *glayout.Node, edge func(pprof.Edge)) {
	node(focus)
	for _, e := range g.in[focus][:min(len(g.in[focus]), graphCallers)] {
		edge(e)
	}
	minShare := int64(float64(prof.Total) * graphMinShare)
	for _, e := range g.out[focus][:min(len(g.out[focus]), graphCallees)] {
		edge(e)
		for _, e2 := range g.out[e.Callee][:min(len(g.out[e.Callee]), graphCallees2)] {
			if prof.Edges[e2] >= minShare && e2.Callee != focus {
				edge(e2)
			}
		}
	}
}

// Limits on the whole profile's graph, pprof's defaults.
const (
	fullMaxNodes  = 80
	fullNodeShare = 0.005
	fullEdgeShare = 0.001
)

// pickAll adds the functions with the most samples below them and the
// calls between them, dropping the ones too small to matter.
func (g *callGraph) pickAll(prof *pprof.Profile, node func(string) *glayout.Node, edge func(pprof.Edge)) {
	var names []string
	for name, f := range prof.Funcs {
		if float64(f.Cum) >= fullNodeShare*float64(prof.Total) {
			names = append(names, name)
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(cmp.Compare(prof.Funcs[b].Cum, prof.Funcs[a].Cum), cmp.Compare(a, b))
	})
	names = names[:min(len(names), fullMaxNodes)]
	keep := map[string]bool{}
	for _, name := range names {
		keep[name] = true
		node(name)
	}
	for _, name := range names {
		for _, e := range g.out[name] {
			if keep[e.Callee] && float64(prof.Edges[e]) >= fullEdgeShare*float64(prof.Total) {
				edge(e)
			}
		}
	}
}

// measureDp is the width of line in dp at size.
func measureDp(gtx layout.Context, th *gui.Theme, line string, size unit.Sp) float32 {
	label := th.Label(line, 1)
	label.TextSize = size
	label.MaxLines = 1
	gtx.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	macro := op.Record(gtx.Ops)
	dims := label.Layout(gtx)
	macro.Stop()
	return float32(dims.Size.X) / gtx.Metric.PxPerDp
}

// percentLabel spells v as a share of total.
func percentLabel(v, total int64) string {
	if total <= 0 {
		return ""
	}
	return percentText(100 * float64(v) / float64(total))
}

func percentText(p float64) string {
	switch {
	case p < 0.1:
		return "<0.1%"
	case p < 10:
		return fmt.Sprintf("%.1f%%", p)
	}
	return fmt.Sprintf("%.0f%%", p)
}

// valueLabel spells v and its share of the samples: 2.43s (18.9%).
func valueLabel(prof *pprof.Profile, v int64) string {
	if v == 0 {
		return "0"
	}
	return valueText(prof, v) + " (" + percentLabel(v, prof.Total) + ")"
}

// valueText spells v in the profile's unit.
func valueText(prof *pprof.Profile, v int64) string {
	unit := prof.Type[strings.LastIndexByte(prof.Type, ' ')+1:]
	f := float64(v)
	switch unit {
	case "nanoseconds":
		switch {
		case f >= 1e9:
			return trimZeros("%.2fs", f/1e9)
		case f >= 1e6:
			return trimZeros("%.2fms", f/1e6)
		}
		return fmt.Sprintf("%.0fµs", f/1e3)
	case "bytes":
		switch {
		case f >= 1<<30:
			return trimZeros("%.2fGB", f/(1<<30))
		case f >= 1<<20:
			return trimZeros("%.2fMB", f/(1<<20))
		case f >= 1<<10:
			return trimZeros("%.2fkB", f/(1<<10))
		}
		return fmt.Sprintf("%dB", v)
	}
	return fmt.Sprint(v)
}

// trimZeros formats a number and drops the zeros after its point:
// 40.00ms is 40ms.
func trimZeros(format string, v float64) string {
	s := fmt.Sprintf(format, v)
	unit := strings.TrimLeft(s, "0123456789.")
	num := s[:len(s)-len(unit)]
	if strings.Contains(num, ".") {
		num = strings.TrimRight(strings.TrimRight(num, "0"), ".")
	}
	return num + unit
}

// layoutGraphPanel draws the call graph of the open function; a click
// on a function opens it.
func (ui *FileUI) layoutGraphPanel(gtx layout.Context) layout.Dimensions {
	th := ui.Theme
	tab := ui.activeTab()
	var empty string
	switch {
	case ui.Profile == nil:
		empty = "Open a profile to see the calls around a function."
	case tab == nil:
		empty = "Open a function to see its calls."
	case ui.Profile.Funcs[tab.Name].Cum == 0:
		empty = "The profile has no samples in this function."
	}
	return ui.panelFrame(gtx, "CALL GRAPH", nil, func(gtx layout.Context) layout.Dimensions {
		if empty != "" {
			return layout.Inset{Left: 16, Right: 16}.Layout(gtx, th.Muted(empty, 0.9).Layout)
		}
		g := &ui.Graph
		g.build(gtx, th, ui.Profile, tab.Name)
		if g.layout == nil {
			return layout.Dimensions{}
		}
		if name, ok := g.draw(gtx, th, ui.Profile); ok && name != tab.Name {
			ui.openTab(ui.findFunc(name), true)
			gtx.Execute(op.InvalidateCmd{})
		}
		return layout.Dimensions{Size: gtx.Constraints.Max}
	})
}

// layoutProfileGraph draws the whole profile's graph beside the code;
// a click on a function opens it.
func (ui *FileUI) layoutProfileGraph(gtx layout.Context) layout.Dimensions {
	th := ui.Theme
	focus := ""
	if tab := ui.activeTab(); tab != nil {
		focus = tab.Name
	}
	g := &ui.ProfileGraph
	g.full = true
	g.build(gtx, th, ui.Profile, focus)
	for ui.GraphFullBtn.Clicked(gtx) {
		ui.GraphFull = !ui.GraphFull
	}
	size := gtx.Constraints.Max
	// The sidebar's darker tone and a shadow along the left edge set
	// the pane apart from the code.
	paint.FillShape(gtx.Ops, th.Colors.Sidebar, clip.Rect{Max: size}.Op())
	if g.layout != nil {
		if name, ok := g.draw(gtx, th, ui.Profile); ok {
			ui.openTab(ui.findFunc(name), true)
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	shadow := gtx.Dp(10)
	stack := clip.Rect{Max: image.Pt(shadow, size.Y)}.Push(gtx.Ops)
	paint.LinearGradientOp{
		Stop1: f32.Pt(0, 0), Color1: color.NRGBA{A: 0x22},
		Stop2: f32.Pt(float32(shadow), 0), Color2: color.NRGBA{},
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()

	icon, description := FullscreenIcon, "Fill the window"
	if ui.GraphFull {
		icon, description = FullscreenExitIcon, "Back beside the code"
	}
	layout.NE.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(8).Layout(gtx, gui.IconButton{Theme: th, Clickable: &ui.GraphFullBtn, Tip: &ui.GraphFullTip, Icon: icon, Description: description}.Layout)
	})
	return layout.Dimensions{Size: size}
}

// draw draws the graph scaled to fit the width, and returns the
// function clicked, if any.
func (g *callGraph) draw(gtx layout.Context, th *gui.Theme, prof *pprof.Profile) (string, bool) {
	size := gtx.Constraints.Max
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	pad := float32(gtx.Metric.Dp(8))
	// Fit the width, but keep the text readable; the graph pans when it doesn't fit, and
	// zooms with Ctrl or Command held.
	fit, least := (float32(size.X)-2*pad)/float32(g.bounds.X), float32(0.9)
	if g.full {
		least = 0.6
	}
	fit = min(max(fit, gtx.Metric.PxPerDp*least), gtx.Metric.PxPerDp)
	var scale float32
	var content, origin f32.Point
	rescale := func() {
		scale = fit * cmp.Or(g.zoom, 1)
		content = f32.Pt(float32(g.bounds.X)*scale+2*pad, float32(g.bounds.Y)*scale+2*pad)
		origin = f32.Pt(max((float32(size.X)-content.X)/2, 0), 0)
		if g.full {
			origin.Y = max((float32(size.Y)-content.Y)/2, 0)
		}
	}
	rescale()
	if g.center || g.reveal {
		for i, n := range g.graph.Nodes {
			if n.ID != g.focus {
				continue
			}
			box := g.layout.Nodes[i]
			lo := box.Center.Sub(box.Size.Scale(0.5)).Sub(g.min)
			hi := box.Center.Add(box.Size.Scale(0.5)).Sub(g.min)
			at := func(v glayout.Vector) f32.Point {
				return f32.Pt(float32(v.X)*scale+pad, float32(v.Y)*scale+pad).Add(origin).Add(g.offset)
			}
			a, b := at(lo), at(hi)
			visible := a.X >= 0 && a.Y >= 0 && b.X <= float32(size.X) && b.Y <= float32(size.Y)
			if g.center || !visible {
				p := g.layout.Nodes[i].Center.Sub(g.min)
				g.offset.X = float32(size.X)/2 - origin.X - (float32(p.X)*scale + pad)
				if g.full {
					g.offset.Y = float32(size.Y)/2 - origin.Y - (float32(p.Y)*scale + pad)
				}
			}
		}
		g.center, g.reveal = false, false
	}

	clicked := ""
	event.Op(gtx.Ops, g)
	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  g,
			Kinds:   pointer.Move | pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Leave | pointer.Scroll,
			ScrollX: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20},
			ScrollY: pointer.ScrollRange{Min: -1 << 20, Max: 1 << 20},
		})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok {
			continue
		}
		at := func() int { return g.nodeAt(e.Position.Sub(origin).Sub(g.offset), scale, pad) }
		switch e.Kind {
		case pointer.Scroll:
			if !e.Modifiers.Contain(key.ModShortcut) {
				g.offset = g.offset.Sub(e.Scroll)
				break
			}
			// Zoom about the pointer.
			p := e.Position.Sub(origin).Sub(g.offset).Sub(f32.Pt(pad, pad)).Mul(1 / scale)
			g.zoom = min(max(cmp.Or(g.zoom, 1)*float32(math.Exp(float64(-e.Scroll.Y)/200)), 0.2), 4)
			rescale()
			g.offset = e.Position.Sub(origin).Sub(f32.Pt(pad, pad)).Sub(p.Mul(scale))
		case pointer.Move:
			g.hover = at()
		case pointer.Leave:
			g.hover = -1
		case pointer.Press:
			g.press, g.moved, g.drag = true, false, e.Position
		case pointer.Drag:
			// Drag to pan, once the pointer has moved past a click's slop.
			if d := e.Position.Sub(g.drag); g.moved || d.X*d.X+d.Y*d.Y > float32(gtx.Dp(4)*gtx.Dp(4)) {
				g.moved = true
				g.offset = g.offset.Add(d)
				g.drag = e.Position
			}
		case pointer.Release:
			if g.press && !g.moved {
				if i := at(); i >= 0 {
					clicked = g.graph.Nodes[i].ID
				}
			}
			g.press = false
		case pointer.Cancel:
			g.press = false
		}
	}
	g.offset.X = min(max(g.offset.X, min(float32(size.X)-content.X-origin.X, 0)), 0)
	g.offset.Y = min(max(g.offset.Y, min(float32(size.Y)-content.Y-origin.Y, 0)), 0)
	if g.hover >= 0 {
		pointer.CursorPointer.Add(gtx.Ops)
	}

	at := func(p glayout.Vector) f32.Point {
		return f32.Pt(float32(p.X-g.min.X)*scale+pad, float32(p.Y-g.min.Y)*scale+pad).Add(origin).Add(g.offset)
	}
	c := th.Colors

	// Edges, thicker for more samples, under the nodes.
	for i, e := range g.layout.Edges {
		if len(e.Path) < 2 {
			continue
		}
		ge := g.graph.Edges[i]
		share := float32(prof.Edges[pprof.Edge{Caller: ge.From.ID, Callee: ge.To.ID}]) / float32(max(prof.Total, 1))
		width := float32(gtx.Metric.Dp(1)) * (1 + 3*min(share*4, 1))
		col := c.MutedText
		if g.hover >= 0 && (ge.From == g.graph.Nodes[g.hover] || ge.To == g.graph.Nodes[g.hover]) {
			col = c.Accent
		}
		var path clip.Path
		path.Begin(gtx.Ops)
		path.MoveTo(at(e.Path[0]))
		for _, p := range e.Path[1:] {
			path.LineTo(at(p))
		}
		paint.FillShape(gtx.Ops, col, clip.Stroke{Path: path.End(), Width: width}.Op())
		arrowHead(gtx, at(e.Path[len(e.Path)-2]), at(e.Path[len(e.Path)-1]), float32(gtx.Metric.Dp(7))*scale/gtx.Metric.PxPerDp, col)
		if e.LabelSize != (glayout.Vector{}) {
			drawText(gtx, th, ge.Label, at(e.LabelCenter), unit.Sp(float32(e.FontSize)*scale/gtx.Metric.PxPerDp), c.Text, font.Normal)
		}
	}

	// Nodes, tinted by their share of the samples.
	for i, n := range g.layout.Nodes {
		name := g.graph.Nodes[i].ID
		share := float32(prof.Funcs[name].Cum) / float32(max(prof.Total, 1))
		r := image.Rectangle{
			Min: at(n.Center.Sub(n.Size.Scale(0.5))).Round(),
			Max: at(n.Center.Add(n.Size.Scale(0.5))).Round(),
		}
		radius := gtx.Metric.Dp(5)
		paint.FillShape(gtx.Ops, c.Background, clip.UniformRRect(r, radius).Op(gtx.Ops))
		heat := c.Marks.Alloc
		heat.A = uint8(0x14 + 0x80*min(share*2, 1))
		paint.FillShape(gtx.Ops, heat, clip.UniformRRect(r, radius).Op(gtx.Ops))
		border, bw := c.Divider, float32(gtx.Metric.Dp(1))
		switch {
		case name == g.focus:
			border, bw = c.Accent, float32(gtx.Metric.Dp(2))
		case i == g.hover:
			border = c.Accent
		}
		paint.FillShape(gtx.Ops, border, clip.Stroke{Path: clip.UniformRRect(r, radius).Path(gtx.Ops), Width: bw}.Op())

		lines := strings.Split(g.layout.Nodes[i].Label, "\n")
		size := unit.Sp(float32(n.FontSize) * scale / gtx.Metric.PxPerDp)
		lineHeight := float32(n.FontSize) * 1.3 * scale
		center := at(n.Center)
		top := center.Y - lineHeight*float32(len(lines)-1)/2
		for k, line := range lines {
			col, weight := c.Text, font.Medium
			if strings.ContainsRune(line, '%') {
				col, weight = c.Text, font.Normal
			}
			drawText(gtx, th, line, f32.Pt(center.X, top+float32(k)*lineHeight), size, col, weight)
		}
	}
	return clicked, clicked != ""
}

// nodeAt is the index of the node under p, in px from the content's
// top left, or -1.
func (g *callGraph) nodeAt(p f32.Point, scale, pad float32) int {
	for i, n := range g.layout.Nodes {
		lo := n.Center.Sub(n.Size.Scale(0.5)).Sub(g.min)
		hi := n.Center.Add(n.Size.Scale(0.5)).Sub(g.min)
		x, y := (p.X-pad)/scale, (p.Y-pad)/scale
		if float32(lo.X) <= x && x <= float32(hi.X) && float32(lo.Y) <= y && y <= float32(hi.Y) {
			return i
		}
	}
	return -1
}

// arrowHead draws a filled arrowhead at to, pointing from from.
func arrowHead(gtx layout.Context, from, to f32.Point, size float32, col color.NRGBA) {
	d := to.Sub(from)
	l := float32(math.Hypot(float64(d.X), float64(d.Y)))
	if l == 0 {
		return
	}
	d = d.Mul(1 / l)
	n := f32.Pt(-d.Y, d.X)
	base := to.Sub(d.Mul(size))
	var path clip.Path
	path.Begin(gtx.Ops)
	path.MoveTo(to)
	path.LineTo(base.Add(n.Mul(size / 2)))
	path.LineTo(base.Sub(n.Mul(size / 2)))
	path.Close()
	paint.FillShape(gtx.Ops, col, clip.Outline{Path: path.End()}.Op())
}

// drawText draws one line centred on center.
func drawText(gtx layout.Context, th *gui.Theme, s string, center f32.Point, size unit.Sp, col color.NRGBA, weight font.Weight) {
	label := th.Label(s, 1)
	label.TextSize = size
	label.Color = col
	label.Font.Weight = weight
	label.MaxLines = 1
	gtx.Constraints = layout.Constraints{Max: image.Pt(1<<20, 1<<20)}
	macro := op.Record(gtx.Ops)
	dims := label.Layout(gtx)
	call := macro.Stop()
	off := center.Sub(f32.Pt(float32(dims.Size.X)/2, float32(dims.Size.Y)/2)).Round()
	defer op.Offset(off).Push(gtx.Ops).Pop()
	call.Add(gtx.Ops)
}
