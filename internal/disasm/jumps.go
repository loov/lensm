package disasm

import (
	"cmp"
	"slices"
)

// FarJump is the length in rows above which a jump is drawn as two
// stubs, for a view whose height is not known yet.
const FarJump = 40

// LayoutJumps gives each jump a column, so jumps whose spans overlap get
// separate lines. Near jumps to one target share a column and join into
// one arrow. A jump over more than far rows gets a stub at each end
// instead, as its line could not show both ends, and the far jumps to
// one target share the stub there. It does nothing when the code is
// already laid out for far.
func (code *Code) LayoutJumps(far int) {
	if code.jumpsFar == far {
		return
	}
	code.jumpsFar = far
	// A span covers rows in thirds: a jump leaves its row two thirds
	// down and arrives one third down.
	type span struct {
		min, max int
		insts    []*Inst
		in       bool // the incoming stub of far jumps
	}
	var spans, stubs []*span
	near := map[int]*span{}
	in := map[int]*span{}
	grow := func(spans map[int]*span, target, lo, hi int, ix *Inst, isIn bool) *span {
		s, ok := spans[target]
		if !ok {
			s = &span{min: lo, max: hi, in: isIn}
			spans[target] = s
		}
		s.min, s.max = min(s.min, lo), max(s.max, hi)
		if ix != nil {
			s.insts = append(s.insts, ix)
		}
		return s
	}
	for i := range code.Insts {
		ix := &code.Insts[i]
		ix.RefFar = false
		if ix.RefOffset == 0 {
			continue
		}
		target := i + ix.RefOffset
		if target < 0 || target >= len(code.Insts) {
			// The target was trimmed with the function's padding.
			ix.RefOffset = 0
			continue
		}
		from, to := 3*i+2, 3*target+1
		if abs(ix.RefOffset) <= far {
			grow(near, target, min(from, to), max(from, to), ix, false)
			continue
		}
		ix.RefFar = true
		if ix.RefOffset > 0 {
			stubs = append(stubs, &span{min: from, max: from + 2, insts: []*Inst{ix}})
			grow(in, target, to-2, to, &code.Insts[target], true)
		} else {
			stubs = append(stubs, &span{min: from - 2, max: from, insts: []*Inst{ix}})
			grow(in, target, to, to+2, &code.Insts[target], true)
		}
	}
	for _, s := range near {
		spans = append(spans, s)
	}
	for _, s := range in {
		stubs = append(stubs, s)
	}
	byStart := func(a, b *span) int {
		return cmp.Or(cmp.Compare(a.min, b.min), cmp.Compare(b.max, a.max))
	}
	slices.SortFunc(spans, byStart)
	slices.SortFunc(stubs, byStart)

	// Taking spans by their start and the first free column gives the
	// fewest columns. Any order of the columns works after that, so the
	// ones with the shortest spans go next to the code.
	type column struct {
		end, longest int
		spans        []*span
	}
	var columns []*column
	for _, s := range spans {
		k := slices.IndexFunc(columns, func(c *column) bool { return c.end < s.min })
		if k < 0 {
			k = len(columns)
			columns = append(columns, &column{})
		}
		c := columns[k]
		c.end = s.max
		c.longest = max(c.longest, s.max-s.min)
		c.spans = append(c.spans, s)
	}
	slices.SortStableFunc(columns, func(a, b *column) int { return cmp.Compare(a.longest, b.longest) })

	// Stubs go in the innermost column free at their rows, so their
	// line out of the code stays short. One that fits nowhere gets a
	// new column next to the code.
	for _, s := range stubs {
		k := slices.IndexFunc(columns, func(c *column) bool {
			return !slices.ContainsFunc(c.spans, func(o *span) bool { return o.min <= s.max && s.min <= o.max })
		})
		if k < 0 {
			k = 0
			columns = slices.Insert(columns, 0, &column{})
		}
		columns[k].spans = append(columns[k].spans, s)
	}

	for i := range code.Insts {
		code.Insts[i].RefStack, code.Insts[i].InStack = 0, 0
	}
	for k, c := range columns {
		for _, s := range c.spans {
			for _, ix := range s.insts {
				if s.in {
					ix.InStack = k + 1
				} else {
					ix.RefStack = k + 1
				}
			}
		}
	}
	// Without jumps the code keeps the gap of one column.
	code.MaxJump = max(len(columns), 1)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
