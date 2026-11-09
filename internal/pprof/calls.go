package pprof

import (
	"cmp"
	"slices"
)

// Calls are a profile's calls from and to each function, hottest
// first, without a function's calls to itself.
type Calls struct {
	prof    *Profile
	Out, In map[string][]Edge
}

// CallIndex indexes the calls of p by caller and by callee.
func (p *Profile) CallIndex() *Calls {
	c := &Calls{prof: p, Out: map[string][]Edge{}, In: map[string][]Edge{}}
	for e := range p.Edges {
		if e.Caller == e.Callee {
			continue
		}
		c.Out[e.Caller] = append(c.Out[e.Caller], e)
		c.In[e.Callee] = append(c.In[e.Callee], e)
	}
	hottest := func(a, b Edge) int {
		return cmp.Or(cmp.Compare(abs(p.Edges[b]), abs(p.Edges[a])), cmp.Compare(a.Caller+a.Callee, b.Caller+b.Callee))
	}
	for _, es := range c.Out {
		slices.SortFunc(es, hottest)
	}
	for _, es := range c.In {
		slices.SortFunc(es, hottest)
	}
	return c
}

// Around returns the calls near focus: its hottest callers and
// callees, and the hottest calls of each callee that have at least
// minShare of the samples.
func (c *Calls) Around(focus string, callers, callees, callees2 int, minShare float64) []Edge {
	var edges []Edge
	edges = append(edges, c.In[focus][:min(len(c.In[focus]), callers)]...)
	least := int64(float64(c.prof.Total) * minShare)
	for _, e := range c.Out[focus][:min(len(c.Out[focus]), callees)] {
		edges = append(edges, e)
		// A call back to focus doesn't take one of the places.
		shown := 0
		for _, e2 := range c.Out[e.Callee] {
			if shown == callees2 || abs(c.prof.Edges[e2]) < least {
				break
			}
			if e2.Callee != focus {
				edges = append(edges, e2)
				shown++
			}
		}
	}
	return edges
}

// Hottest returns at most limit functions with at least minShare of the
// samples below them, the most first, and the calls between them with
// at least a fifth of that.
func (c *Calls) Hottest(minShare float64, limit int) ([]string, []Edge) {
	p := c.prof
	var names []string
	for name, f := range p.Funcs {
		if float64(abs(f.Cum)) >= minShare*float64(p.Total) {
			names = append(names, name)
		}
	}
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(cmp.Compare(abs(p.Funcs[b].Cum), abs(p.Funcs[a].Cum)), cmp.Compare(a, b))
	})
	names = names[:min(len(names), limit)]
	keep := map[string]bool{}
	for _, name := range names {
		keep[name] = true
	}
	var edges []Edge
	for _, name := range names {
		for _, e := range c.Out[name] {
			if keep[e.Callee] && float64(abs(p.Edges[e])) >= minShare/5*float64(p.Total) {
				edges = append(edges, e)
			}
		}
	}
	return names, edges
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
