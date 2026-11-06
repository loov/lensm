package disasm

import "slices"

// LineSet represents a set of needed lines.
type LineSet struct {
	list []int
}

// Add adds line to the needed set.
func (rs *LineSet) Add(line int) {
	if at, found := slices.BinarySearch(rs.list, line); !found {
		rs.list = slices.Insert(rs.list, at, line)
	}
}

// Ranges merges the set into ranges, each widened by context on both
// sides. The first range can start below the smallest member, at zero
// or below; callers holding line numbers clamp it.
func (rs *LineSet) Ranges(context int) []LineRange {
	if len(rs.list) == 0 {
		return nil
	}

	var all []LineRange

	current := LineRange{From: rs.list[0] - context, To: rs.list[0] + context + 1}
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
