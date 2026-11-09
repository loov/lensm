package main

import (
	"fmt"
	"slices"
	"testing"

	"gioui.org/f32"

	glayout "github.com/loov/layout"

	"loov.dev/lensm/internal/pprof"
)

// picked runs a graph selection and returns the nodes and edges it
// added, in order.
func picked(t *testing.T, prof *pprof.Profile, pick func(g *callGraph, node func(string) *glayout.Node, edge func(pprof.Edge))) (nodes []string, edges []pprof.Edge) {
	t.Helper()
	var g callGraph
	g.calls(prof)
	graph := glayout.NewDigraph()
	node := func(name string) *glayout.Node {
		if n, ok := graph.NodeByID[name]; ok {
			return n
		}
		nodes = append(nodes, name)
		return graph.Node(name)
	}
	edge := func(e pprof.Edge) {
		node(e.Caller)
		node(e.Callee)
		edges = append(edges, e)
	}
	pick(&g, node, edge)
	return nodes, edges
}

func TestPickAllDropsSmallFunctions(t *testing.T) {
	prof := &pprof.Profile{Total: 1000, Funcs: map[string]pprof.FuncSamples{}, Edges: map[pprof.Edge]int64{}}
	add := func(name string, cum int64) { prof.Funcs[name] = pprof.FuncSamples{Cum: cum} }
	add("main", 1000)
	add("hot", 600)
	add("warm", 300)
	add("tiny", 4) // below 0.5%
	prof.Edges[pprof.Edge{Caller: "main", Callee: "hot"}] = 600
	prof.Edges[pprof.Edge{Caller: "main", Callee: "warm"}] = 300
	prof.Edges[pprof.Edge{Caller: "main", Callee: "tiny"}] = 4
	prof.Edges[pprof.Edge{Caller: "hot", Callee: "warm"}] = 0 // below 0.1%
	prof.Edges[pprof.Edge{Caller: "hot", Callee: "hot"}] = 50

	nodes, edges := picked(t, prof, func(g *callGraph, node func(string) *glayout.Node, edge func(pprof.Edge)) {
		g.pickAll(prof, node, edge)
	})
	if want := []string{"main", "hot", "warm"}; !slices.Equal(nodes, want) {
		t.Errorf("nodes = %v, want %v", nodes, want)
	}
	for _, e := range edges {
		if e.Callee == "tiny" || e == (pprof.Edge{Caller: "hot", Callee: "warm"}) || e.Caller == e.Callee {
			t.Errorf("edge %v should be dropped", e)
		}
	}
	if len(edges) != 2 {
		t.Errorf("edges = %v, want main to hot and warm", edges)
	}
}

func TestPickAllKeepsTheHottest(t *testing.T) {
	prof := &pprof.Profile{Total: 1000, Funcs: map[string]pprof.FuncSamples{}, Edges: map[pprof.Edge]int64{}}
	for i := range fullMaxNodes + 20 {
		prof.Funcs[fmt.Sprintf("f%03d", i)] = pprof.FuncSamples{Cum: int64(1000 - i)}
	}
	nodes, _ := picked(t, prof, func(g *callGraph, node func(string) *glayout.Node, edge func(pprof.Edge)) {
		g.pickAll(prof, node, edge)
	})
	if len(nodes) != fullMaxNodes || nodes[0] != "f000" || nodes[len(nodes)-1] != fmt.Sprintf("f%03d", fullMaxNodes-1) {
		t.Errorf("got %d nodes from %v to %v", len(nodes), nodes[0], nodes[len(nodes)-1])
	}
}

func TestPickAroundLimitsTheNeighbourhood(t *testing.T) {
	prof := &pprof.Profile{Total: 10000, Funcs: map[string]pprof.FuncSamples{}, Edges: map[pprof.Edge]int64{}}
	for i := range graphCallers + 2 {
		prof.Edges[pprof.Edge{Caller: fmt.Sprintf("caller%d", i), Callee: "focus"}] = int64(100 - i)
	}
	for i := range graphCallees + 2 {
		callee := fmt.Sprintf("callee%d", i)
		prof.Edges[pprof.Edge{Caller: "focus", Callee: callee}] = int64(1000 - i)
		for k := range graphCallees2 + 2 {
			prof.Edges[pprof.Edge{Caller: callee, Callee: fmt.Sprintf("%s.%d", callee, k)}] = int64(500 - k)
		}
	}
	// Too small to show, and a call back to the focus.
	prof.Edges[pprof.Edge{Caller: "callee0", Callee: "small"}] = 49
	prof.Edges[pprof.Edge{Caller: "callee1", Callee: "focus"}] = 900

	_, edges := picked(t, prof, func(g *callGraph, node func(string) *glayout.Node, edge func(pprof.Edge)) {
		g.pickAround(prof, "focus", node, edge)
	})
	var callers, callees, second int
	for _, e := range edges {
		switch {
		case e.Callee == "focus" && e.Caller == "callee1":
			// callee1 calls back, which counts as one of the callers.
			callers++
		case e.Callee == "focus":
			callers++
			if e.Caller == fmt.Sprintf("caller%d", graphCallers) {
				t.Errorf("%v is beyond the hottest callers", e)
			}
		case e.Caller == "focus":
			callees++
		default:
			second++
			if e.Callee == "small" {
				t.Errorf("%v is below the least share", e)
			}
		}
	}
	if callers != graphCallers || callees != graphCallees || second != graphCallees*graphCallees2 {
		t.Errorf("callers %d, callees %d, second level %d", callers, callees, second)
	}
}

func TestSegmentDistance(t *testing.T) {
	a, b := f32.Pt(0, 0), f32.Pt(10, 0)
	for _, tc := range []struct {
		p    f32.Point
		want float32
	}{
		{f32.Pt(5, 3), 3},   // beside the middle
		{f32.Pt(-4, 3), 5},  // past the start
		{f32.Pt(13, -4), 5}, // past the end
	} {
		if got := segmentDistance(tc.p, a, b); got != tc.want {
			t.Errorf("distance of %v = %v, want %v", tc.p, got, tc.want)
		}
	}
	if got := segmentDistance(f32.Pt(3, 4), a, a); got != 5 {
		t.Errorf("distance to a point = %v, want 5", got)
	}
}
