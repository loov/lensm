package pprof

import (
	"fmt"
	"slices"
	"testing"
)

func TestHottestDropsSmallFunctions(t *testing.T) {
	prof := &Profile{Total: 1000, Funcs: map[string]FuncSamples{}, Edges: map[Edge]int64{}}
	add := func(name string, cum int64) { prof.Funcs[name] = FuncSamples{Cum: cum} }
	add("main", 1000)
	add("hot", 600)
	add("warm", 300)
	add("tiny", 4) // below 0.5%
	prof.Edges[Edge{Caller: "main", Callee: "hot"}] = 600
	prof.Edges[Edge{Caller: "main", Callee: "warm"}] = 300
	prof.Edges[Edge{Caller: "main", Callee: "tiny"}] = 4
	prof.Edges[Edge{Caller: "hot", Callee: "warm"}] = 0 // below 0.1%
	prof.Edges[Edge{Caller: "hot", Callee: "hot"}] = 50

	nodes, edges := prof.CallIndex().Hottest(0.005, 80)
	if want := []string{"main", "hot", "warm"}; !slices.Equal(nodes, want) {
		t.Errorf("nodes = %v, want %v", nodes, want)
	}
	want := []Edge{{Caller: "main", Callee: "hot"}, {Caller: "main", Callee: "warm"}}
	if !slices.Equal(edges, want) {
		t.Errorf("edges = %v, want %v", edges, want)
	}
}

func TestHottestKeepsTheLimit(t *testing.T) {
	prof := &Profile{Total: 1000, Funcs: map[string]FuncSamples{}, Edges: map[Edge]int64{}}
	for i := range 100 {
		prof.Funcs[fmt.Sprintf("f%03d", i)] = FuncSamples{Cum: int64(1000 - i)}
	}
	nodes, _ := prof.CallIndex().Hottest(0.005, 80)
	if len(nodes) != 80 || nodes[0] != "f000" || nodes[79] != "f079" {
		t.Errorf("got %d nodes from %v to %v", len(nodes), nodes[0], nodes[len(nodes)-1])
	}
}

func TestAroundLimitsTheNeighbourhood(t *testing.T) {
	const callers, callees, callees2 = 5, 6, 3
	prof := &Profile{Total: 10000, Edges: map[Edge]int64{}}
	for i := range callers + 2 {
		prof.Edges[Edge{Caller: fmt.Sprintf("caller%d", i), Callee: "focus"}] = int64(100 - i)
	}
	for i := range callees + 2 {
		callee := fmt.Sprintf("callee%d", i)
		prof.Edges[Edge{Caller: "focus", Callee: callee}] = int64(1000 - i)
		for k := range callees2 + 2 {
			prof.Edges[Edge{Caller: callee, Callee: fmt.Sprintf("%s.%d", callee, k)}] = int64(500 - k)
		}
	}
	// Too small to show, and a call back to the focus.
	prof.Edges[Edge{Caller: "callee0", Callee: "small"}] = 49
	prof.Edges[Edge{Caller: "callee1", Callee: "focus"}] = 900

	var in, out, second int
	for _, e := range prof.CallIndex().Around("focus", callers, callees, callees2, 0.005) {
		switch {
		case e.Callee == "focus":
			// callee1 calls back, which counts as one of the callers.
			in++
			if e.Caller == fmt.Sprintf("caller%d", callers) {
				t.Errorf("%v is beyond the hottest callers", e)
			}
		case e.Caller == "focus":
			out++
		default:
			second++
			if e.Callee == "small" {
				t.Errorf("%v is below the least share", e)
			}
		}
	}
	if in != callers || out != callees || second != callees*callees2 {
		t.Errorf("callers %d, callees %d, second level %d", in, out, second)
	}
}
