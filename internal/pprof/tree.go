package pprof

import (
	"cmp"
	"slices"
)

// TreeNode is a function at one place in the call tree: the samples
// in it and below it through the calls that led there.
type TreeNode struct {
	Name   string
	Parent *TreeNode
	// Value is the samples of the profile; Delta, of a difference, the
	// change, which can be negative while Value can't.
	Value, Delta int64
	Children     []*TreeNode
	index        map[string]*TreeNode
}

// Tree builds the call tree of prof's stacks, the callers above
// the callees, children in name order as pprof draws them.
func (prof *Profile) Tree() *TreeNode {
	root := &TreeNode{Name: "all"}
	for _, s := range prof.Stacks {
		n := root
		add := func(n *TreeNode) {
			if s.Value > 0 {
				n.Value += s.Value
			}
			n.Delta += s.Value
		}
		add(n)
		for i := len(s.Funcs) - 1; i >= 0; i-- {
			name := s.Funcs[i]
			if name == "" {
				continue
			}
			child := n.index[name]
			if child == nil {
				child = &TreeNode{Name: name, Parent: n}
				if n.index == nil {
					n.index = map[string]*TreeNode{}
				}
				n.index[name] = child
				n.Children = append(n.Children, child)
			}
			n = child
			add(n)
		}
	}
	var sortTree func(n *TreeNode)
	sortTree = func(n *TreeNode) {
		slices.SortFunc(n.Children, func(a, b *TreeNode) int { return cmp.Compare(a.Name, b.Name) })
		n.index = nil
		for _, c := range n.Children {
			sortTree(c)
		}
	}
	sortTree(root)
	return root
}

// Path names n and its callers, from the root down.
func (n *TreeNode) Path() []string {
	var p []string
	for ; n != nil; n = n.Parent {
		p = append(p, n.Name)
	}
	slices.Reverse(p)
	return p
}
