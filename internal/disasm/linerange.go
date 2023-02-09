package disasm

// LineRange represents a list of lines.
type LineRange struct{ From, To int }

// LineRangesContain checks whether line a or line b is contained in the ranges.
func LineRangesContain(ranges []LineRange, a, b int) bool {
	for _, r := range ranges {
		if (r.From <= a && a < r.To) || (r.From <= b && b < r.To) {
			return true
		}
	}
	return false
}
