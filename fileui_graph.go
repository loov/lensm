package main

import (
	"cmp"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
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
	focus      string
	prof       *pprof.Profile
	textSize   unit.Sp
	builtShare float64
	// full lays out the whole profile, as pprof's graph does, instead
	// of the calls around focus.
	full bool

	graph  *glayout.Graph
	layout *glayout.Layout
	// bounds is the extent of the layout, in dp.
	bounds glayout.Vector
	min    glayout.Vector

	// calls indexes the calls of the profile.
	calls   *pprof.Calls
	callsOf *pprof.Profile

	offset f32.Point // pan, in px
	hover  int
	pan    gui.PanZoom
	// zoom multiplies the fitted scale; 0 is 1.
	zoom float32

	// minShare is the least share of the samples a function in the
	// whole graph needs; 0 is fullNodeShare.
	minShare float64
	// matches are the functions found by the search, drawn marked.
	matches map[string]bool
	// target is a function to scroll to, other than focus.
	target string
	// center asks draw to centre the focus; reveal only when it is
	// out of view, so a node clicked in the graph stays put.
	center, reveal bool
}

// build lays out the graph around focus.
func (g *callGraph) build(gtx layout.Context, th *gui.Theme, prof *pprof.Profile, focus string) {
	if g.prof == prof && g.textSize == th.TextSize && g.builtShare == g.minShare && (g.focus == focus || g.full && g.layout != nil) {
		// The whole graph doesn't depend on focus; only bring it into
		// view, if it isn't already.
		g.reveal = g.reveal || g.focus != focus
		g.focus = focus
		return
	}
	g.focus, g.prof, g.textSize, g.builtShare = focus, prof, th.TextSize, g.minShare
	g.layout, g.offset, g.hover, g.center, g.zoom = nil, f32.Point{}, -1, true, 0
	if g.callsOf != prof {
		g.calls, g.callsOf = prof.CallIndex(), prof
	}

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
		maxFlat = max(maxFlat, abs(f.Flat))
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
			n.Label = pkg + rest + "\n" + prof.Signed(f.Cum, pprof.Percent(f.Cum, prof.Total))
			return n
		}
		// As pprof does, the text grows with the function's own samples.
		n.FontSize = fontSize * glayout.Length(1+1.5*math.Sqrt(float64(abs(f.Flat))/float64(max(maxFlat, 1))))
		lines := []string{strings.TrimSuffix(pkg, "."), rest}
		if f.Flat == f.Cum {
			lines = append(lines, prof.Label(f.Flat))
		} else {
			lines = append(lines, prof.Label(f.Flat), "of "+prof.Label(f.Cum))
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
		v := prof.Edges[e]
		le.Label = prof.Signed(v, pprof.Percent(v, prof.Total))
		if g.full {
			le.Label = prof.Signed(v, prof.Format(v))
		}
		le.FontSize = fontSize * 0.9
		graph.AddEdge(le)
	}
	var edges []pprof.Edge
	if g.full {
		var names []string
		names, edges = g.calls.Hottest(g.share(), fullMaxNodes)
		for _, name := range names {
			node(name)
		}
	} else {
		node(focus)
		edges = g.calls.Around(focus, graphCallers, graphCallees, graphCallees2, graphMinShare)
	}
	for _, e := range edges {
		edge(e)
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

// Limits on the whole profile's graph, pprof's defaults.
const (
	fullMaxNodes  = 80
	fullNodeShare = 0.005
)

// heatColor is the colour of a function with v samples: the profile's
// heat, or in a difference red where it grew and green where it shrank.
func heatColor(c gui.UIColors, diff bool, v int64) color.NRGBA {
	switch {
	case diff && v > 0:
		return c.Marks.BoundsCheck
	case diff && v < 0:
		return c.Marks.Good
	}
	return c.Marks.Alloc
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// share is the least share of the samples a function needs to be in
// the whole graph.
func (g *callGraph) share() float64 { return cmp.Or(g.minShare, fullNodeShare) }

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

// layoutGraphPanel draws the call graph of the open function; a click
// on a function opens it.
func (ui *FileUI) layoutGraphPanel(gtx layout.Context) layout.Dimensions {
	th := ui.Theme
	tab := ui.activeTab()
	prof := ui.graphProfile()
	var empty string
	switch {
	case ui.Profile == nil:
		empty = "Open a profile to see the calls around a function."
	case tab == nil:
		empty = "Open a function to see its calls."
	case prof.Funcs[tab.Name].Cum == 0:
		empty = "The profile has no samples in this function."
	}
	return ui.panelFrame(gtx, "CALL GRAPH", nil, func(gtx layout.Context) layout.Dimensions {
		if empty != "" {
			return layout.Inset{Left: 16, Right: 16}.Layout(gtx, th.Muted(empty, 0.9).Layout)
		}
		g := &ui.Graph
		g.build(gtx, th, prof, tab.Name)
		if g.layout == nil {
			return layout.Dimensions{}
		}
		if name, ok := g.draw(gtx, th, prof); ok && name != tab.Name {
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
	prof := ui.graphProfile()
	g := &ui.ProfileGraph
	g.full = true
	g.minShare = ui.GraphTools.share
	g.build(gtx, th, prof, focus)
	for ui.GraphFullBtn.Clicked(gtx) {
		ui.GraphFull = !ui.GraphFull
	}
	size := gtx.Constraints.Max
	// The sidebar's darker tone and a shadow along the left edge set
	// the pane apart from the code.
	paint.FillShape(gtx.Ops, th.Colors.Sidebar, clip.Rect{Max: size}.Op())
	gtx.Constraints = layout.Exact(size)
	layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return ui.layoutGraphTools(gtx, g)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if ui.GraphTools.Flame {
				ui.Flame.up = ui.Settings.FlameUp
				if name, ok := ui.Flame.layout(gtx, th, prof, ui.GraphTools.text, focus); ok {
					ui.openTab(ui.findFunc(name), true)
					gtx.Execute(op.InvalidateCmd{})
				}
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}
			if g.layout != nil {
				if name, ok := g.draw(gtx, th, prof); ok {
					ui.openTab(ui.findFunc(name), true)
					gtx.Execute(op.InvalidateCmd{})
				}
			}
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
	)
	shadow := gtx.Dp(10)
	// A graph below the code has its shadow along the top.
	edge, stop := image.Pt(shadow, size.Y), f32.Pt(float32(shadow), 0)
	if ui.graphBelow() && !ui.GraphFull {
		edge, stop = image.Pt(size.X, shadow), f32.Pt(0, float32(shadow))
	}
	stack := clip.Rect{Max: edge}.Push(gtx.Ops)
	paint.LinearGradientOp{
		Stop1: f32.Pt(0, 0), Color1: color.NRGBA{A: 0x22},
		Stop2: stop, Color2: color.NRGBA{},
	}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	stack.Pop()
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
	if g.target != "" {
		g.center = true
	}
	if g.center || g.reveal {
		want := cmp.Or(g.target, g.focus)
		g.target = ""
		for i, n := range g.graph.Nodes {
			if n.ID != want {
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
	g.pan.Add(gtx.Ops)
	for {
		e, ok := g.pan.Update(gtx)
		if !ok {
			break
		}
		at := func() int { return g.nodeAt(e.Position.Sub(origin).Sub(g.offset), scale, pad) }
		switch e.Kind {
		case gui.Pan:
			g.offset = g.offset.Add(e.Pan)
		case gui.Zoom:
			// Keep the point under the pointer in place.
			p := e.Position.Sub(origin).Sub(g.offset).Sub(f32.Pt(pad, pad)).Mul(1 / scale)
			g.zoom = min(max(cmp.Or(g.zoom, 1)*e.Zoom, 0.2), 4)
			rescale()
			g.offset = e.Position.Sub(origin).Sub(f32.Pt(pad, pad)).Sub(p.Mul(scale))
		case gui.Hover:
			g.hover = at()
		case gui.Leave:
			g.hover = -1
		case gui.Click:
			if i := at(); i >= 0 {
				clicked = g.graph.Nodes[i].ID
			}
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

	// The call under the pointer, when no function is.
	hoverEdge := -1
	if g.pan.Inside && g.hover < 0 {
		best := float32(gtx.Dp(5))
		for i, e := range g.layout.Edges {
			for k := 1; k < len(e.Path); k++ {
				if d := segmentDistance(g.pan.Pointer, at(e.Path[k-1]), at(e.Path[k])); d < best {
					best, hoverEdge = d, i
				}
			}
		}
	}

	// Edges, thicker for more samples, under the nodes.
	for i, e := range g.layout.Edges {
		if len(e.Path) < 2 {
			continue
		}
		ge := g.graph.Edges[i]
		share := float32(abs(prof.Edges[pprof.Edge{Caller: ge.From.ID, Callee: ge.To.ID}])) / float32(max(prof.Total, 1))
		width := float32(gtx.Metric.Dp(1)) * (1 + 3*min(share*4, 1))
		col := c.MutedText
		if i == hoverEdge || g.hover >= 0 && (ge.From == g.graph.Nodes[g.hover] || ge.To == g.graph.Nodes[g.hover]) {
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
		cum := prof.Funcs[name].Cum
		share := float32(abs(cum)) / float32(max(prof.Total, 1))
		r := image.Rectangle{
			Min: at(n.Center.Sub(n.Size.Scale(0.5))).Round(),
			Max: at(n.Center.Add(n.Size.Scale(0.5))).Round(),
		}
		radius := gtx.Metric.Dp(5)
		paint.FillShape(gtx.Ops, c.Background, clip.UniformRRect(r, radius).Op(gtx.Ops))
		// A difference is red where it grew and green where it shrank.
		heat := heatColor(c, prof.Diff, cum)
		heat.A = uint8(0x14 + 0x80*min(share*2, 1))
		paint.FillShape(gtx.Ops, heat, clip.UniformRRect(r, radius).Op(gtx.Ops))
		border, bw := c.Divider, float32(gtx.Metric.Dp(1))
		switch {
		case g.matches[name]:
			// Orange, as found text, to tell from the open function.
			border, bw = c.Marks.Spill, float32(gtx.Metric.Dp(3))
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

	// Details of what is under the pointer.
	var tip []string
	switch {
	case g.hover >= 0:
		tip = g.nodeDetails(prof, g.graph.Nodes[g.hover].ID)
	case hoverEdge >= 0:
		ge := g.graph.Edges[hoverEdge]
		v := prof.Edges[pprof.Edge{Caller: ge.From.ID, Callee: ge.To.ID}]
		tip = []string{ge.From.ID, "calls " + ge.To.ID, prof.Label(v)}
	}
	if len(tip) > 0 {
		gui.PointerTip(gtx, th, g.pan.Pointer, tip)
	}
	return clicked, clicked != ""
}

// graphTipCalls is how many callers and callees a function's details
// list.
const graphTipCalls = 4

// nodeDetails describes a function for its tooltip: its name, its own
// and cumulative samples and its hottest callers and callees.
func (g *callGraph) nodeDetails(prof *pprof.Profile, name string) []string {
	f := prof.Funcs[name]
	lines := []string{name, "self " + prof.Label(f.Flat), "total " + prof.Label(f.Cum)}
	list := func(title string, edges []pprof.Edge, other func(pprof.Edge) string) {
		if len(edges) == 0 {
			return
		}
		lines = append(lines, "", title)
		for _, e := range edges[:min(len(edges), graphTipCalls)] {
			lines = append(lines, "  "+other(e)+"  "+prof.Format(prof.Edges[e]))
		}
		if len(edges) > graphTipCalls {
			lines = append(lines, fmt.Sprintf("  and %d more", len(edges)-graphTipCalls))
		}
	}
	list("Called by", g.calls.In[name], func(e pprof.Edge) string { return e.Caller })
	list("Calls", g.calls.Out[name], func(e pprof.Edge) string { return e.Callee })
	return lines
}

// segmentDistance is how far p is from the segment a to b.
func segmentDistance(p, a, b f32.Point) float32 {
	d := b.Sub(a)
	t := float32(0)
	if l := d.X*d.X + d.Y*d.Y; l > 0 {
		t = min(max(((p.X-a.X)*d.X+(p.Y-a.Y)*d.Y)/l, 0), 1)
	}
	q := a.Add(d.Mul(t)).Sub(p)
	return float32(math.Hypot(float64(q.X), float64(q.Y)))
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
