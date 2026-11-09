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

func TestMinus(t *testing.T) {
	p := &Profile{Total: 10, Type: "cpu nanoseconds", Stacks: []Stack{
		{Funcs: []string{"main.slow", "main.main"}, Value: 7},
		{Funcs: []string{"main.fast", "main.main"}, Value: 3},
	}}
	base := &Profile{Total: 8, Type: "cpu nanoseconds", Stacks: []Stack{
		{Funcs: []string{"main.slow", "main.main"}, Value: 2},
		{Funcs: []string{"main.fast", "main.main"}, Value: 5},
		{Funcs: []string{"main.gone", "main.main"}, Value: 1},
	}}
	d, err := p.Minus(base)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]FuncSamples{
		"main.slow": {5, 5}, "main.fast": {-2, -2}, "main.gone": {-1, -1}, "main.main": {0, 2},
	}
	if !d.Diff || d.Total != 10 || !maps.Equal(d.Funcs, want) {
		t.Errorf("diff = %v of %d", d.Funcs, d.Total)
	}
	if e := d.Edges[Edge{"main.main", "main.fast"}]; e != -2 {
		t.Errorf("edge main.main → main.fast = %d, want -2", e)
	}
	if _, err := p.Minus(&Profile{Type: "alloc_space bytes"}); err == nil {
		t.Error("a base of another value type compares")
	}
}
