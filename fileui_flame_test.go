package main

import (
	"slices"
	"testing"

	"loov.dev/lensm/internal/pprof"
)

func TestFlameTree(t *testing.T) {
	prof := &pprof.Profile{Stacks: []pprof.Stack{
		{Funcs: []string{"main.b", "main.main"}, Value: 3},
		{Funcs: []string{"main.a", "main.main"}, Value: 2},
		{Funcs: []string{"main.main"}, Value: 1},
		// A frame without a name is skipped, and base samples of a
		// difference count in delta only.
		{Funcs: []string{"main.a", "", "main.main"}, Value: -4},
	}}
	root := flameTree(prof)
	if root.value != 6 || root.delta != 2 || len(root.children) != 1 {
		t.Fatalf("root = %d, delta %d, %d children", root.value, root.delta, len(root.children))
	}
	main := root.children[0]
	var names []string
	for _, c := range main.children {
		names = append(names, c.name)
	}
	if main.name != "main.main" || main.value != 6 || !slices.Equal(names, []string{"main.a", "main.b"}) {
		t.Errorf("main = %s %d, children %q", main.name, main.value, names)
	}
	if a := main.children[0]; a.value != 2 || a.delta != -2 || !slices.Equal(a.path(), []string{"all", "main.main", "main.a"}) {
		t.Errorf("main.a = %d, delta %d, path %q", a.value, a.delta, a.path())
	}
}
