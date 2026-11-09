package pprof

import (
	"maps"
	"strings"
	"testing"
)

func TestFilter(t *testing.T) {
	p := &Profile{Total: 10, Stacks: []Stack{
		{Funcs: []string{"runtime.memmove", "main.copy", "main.main"}, Value: 4},
		{Funcs: []string{"main.sum", "main.main"}, Value: 3},
		{Funcs: []string{"main.rec", "main.rec", "main.main"}, Value: 2},
		{Funcs: []string{"runtime.gc"}, Value: 1},
	}}
	p.Funcs, p.Edges = aggregate(p.Stacks, nil)
	if f := p.Funcs["main.main"]; f.Cum != 9 || f.Flat != 0 {
		t.Errorf("main.main = %+v", f)
	}
	if f := p.Funcs["main.rec"]; f.Cum != 2 || f.Flat != 2 || p.Edges[Edge{"main.rec", "main.rec"}] != 2 {
		t.Errorf("recursion counted twice: %+v, self edge %d", f, p.Edges[Edge{"main.rec", "main.rec"}])
	}

	focused := p.Filter("main.copy", nil)
	if focused.Total != 10 || !maps.Equal(focused.Funcs, map[string]FuncSamples{
		"runtime.memmove": {4, 4}, "main.copy": {0, 4}, "main.main": {0, 4},
	}) {
		t.Errorf("focus on main.copy = %v of %d", focused.Funcs, focused.Total)
	}

	runtime := func(name string) bool { return strings.HasPrefix(name, "runtime.") }
	hidden := p.Filter("", runtime)
	if f := hidden.Funcs["main.copy"]; f.Flat != 4 || f.Cum != 4 {
		t.Errorf("main.copy without runtime = %+v, want memmove's samples", f)
	}
	if _, ok := hidden.Funcs["runtime.gc"]; ok {
		t.Error("runtime.gc is not hidden")
	}
	if p.Funcs["main.copy"].Flat != 0 {
		t.Error("Filter changed the profile it filtered")
	}
}
