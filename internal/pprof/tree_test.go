package pprof

import (
	"slices"
	"testing"
)

func TestTree(t *testing.T) {
	prof := &Profile{Stacks: []Stack{
		{Funcs: []string{"main.b", "main.main"}, Value: 3},
		{Funcs: []string{"main.a", "main.main"}, Value: 2},
		{Funcs: []string{"main.main"}, Value: 1},
		// A frame without a name is skipped, and base samples of a
		// difference count in Delta only.
		{Funcs: []string{"main.a", "", "main.main"}, Value: -4},
	}}
	root := prof.Tree()
	if root.Value != 6 || root.Delta != 2 || len(root.Children) != 1 {
		t.Fatalf("root = %d, delta %d, %d children", root.Value, root.Delta, len(root.Children))
	}
	main := root.Children[0]
	var names []string
	for _, c := range main.Children {
		names = append(names, c.Name)
	}
	if main.Name != "main.main" || main.Value != 6 || !slices.Equal(names, []string{"main.a", "main.b"}) {
		t.Errorf("main = %s %d, children %q", main.Name, main.Value, names)
	}
	if a := main.Children[0]; a.Value != 2 || a.Delta != -2 || !slices.Equal(a.Path(), []string{"all", "main.main", "main.a"}) {
		t.Errorf("main.a = %d, delta %d, path %q", a.Value, a.Delta, a.Path())
	}
}
