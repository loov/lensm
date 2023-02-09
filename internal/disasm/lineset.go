package disasm

import (
	"sort"

	"golang.org/x/exp/slices"
)

// LineSet represents a set of needed lines.
type LineSet struct {
	list []int
}

// Add adds line to the needed set.
func (rs *LineSet) Add(line int) {
	if len(rs.list) == 0 {
		rs.list = append(rs.list, line)
		return
	}
	at := sort.SearchInts(rs.list, line)
	if at >= len(rs.list) {
		rs.list = append(rs.list, line)
	} else if rs.list[at] != line {
		rs.list = slices.Insert(rs.list, at, line)
	}
}

// Ranges converts line set to line ranges and adds context for extra information.
func (rs *LineSet) Ranges(context int) []LineRange {
	if len(rs.list) == 0 {
		return nil
	}

	var all []LineRange

	current := LineRange{From: rs.list[0] - context, To: rs.list[0] + context + 1}
	if current.From < 1 {
		current.From = 1
	}
	for _, line := range rs.list {
		if line-context <= current.To {
			current.To = line + context + 1
		} else {
			all = append(all, current)
			current = LineRange{From: line - context, To: line + context + 1}
		}
	}
	all = append(all, current)

	return all
}

// RangesZero returns a ranges without expanding by context.
func (rs *LineSet) RangesZero() []LineRange {
	if len(rs.list) == 0 {
		return nil
	}

	var all []LineRange

	current := LineRange{From: rs.list[0], To: rs.list[0] + 1}
	for _, line := range rs.list {
		if line <= current.To {
			current.To = line + 1
		} else {
			all = append(all, current)
			current = LineRange{From: line, To: line + 1}
		}
	}
	all = append(all, current)

	return all
}
