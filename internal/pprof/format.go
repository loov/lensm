package pprof

import (
	"fmt"
	"strings"
)

// Percent spells v as a share of total.
func Percent(v, total int64) string {
	if total <= 0 {
		return ""
	}
	if v < 0 {
		return "-" + Percent(-v, total)
	}
	return PercentText(100 * float64(v) / float64(total))
}

// PercentText spells a percentage, to one decimal below 10%.
func PercentText(p float64) string {
	switch {
	case p < 0.1:
		return "<0.1%"
	case p < 10:
		return fmt.Sprintf("%.1f%%", p)
	}
	return fmt.Sprintf("%.0f%%", p)
}

// Label spells v and its share of the samples: 2.43s (18.9%).
func (prof *Profile) Label(v int64) string {
	if v == 0 {
		return "0"
	}
	if prof.Diff && v > 0 {
		// A difference shows its sign either way.
		return "+" + prof.Format(v) + " (" + Percent(v, prof.Total) + ")"
	}
	return prof.Format(v) + " (" + Percent(v, prof.Total) + ")"
}

// Signed puts a plus on s, the spelling of v, when prof is a
// difference and v grew.
func (prof *Profile) Signed(v int64, s string) string {
	if prof.Diff && v > 0 {
		return "+" + s
	}
	return s
}

// Format spells v in the profile's unit.
func (prof *Profile) Format(v int64) string {
	if v < 0 {
		return "-" + prof.Format(-v)
	}
	unit := prof.Type[strings.LastIndexByte(prof.Type, ' ')+1:]
	f := float64(v)
	switch unit {
	case "nanoseconds":
		switch {
		case f >= 1e9:
			return trimZeros("%.2fs", f/1e9)
		case f >= 1e6:
			return trimZeros("%.2fms", f/1e6)
		}
		return fmt.Sprintf("%.0fµs", f/1e3)
	case "bytes":
		switch {
		case f >= 1<<30:
			return trimZeros("%.2fGB", f/(1<<30))
		case f >= 1<<20:
			return trimZeros("%.2fMB", f/(1<<20))
		case f >= 1<<10:
			return trimZeros("%.2fkB", f/(1<<10))
		}
		return fmt.Sprintf("%dB", v)
	}
	return fmt.Sprint(v)
}

// trimZeros formats a number and drops the zeros after its point:
// 40.00ms is 40ms.
func trimZeros(format string, v float64) string {
	s := fmt.Sprintf(format, v)
	unit := strings.TrimLeft(s, "0123456789.")
	num := s[:len(s)-len(unit)]
	if strings.Contains(num, ".") {
		num = strings.TrimRight(strings.TrimRight(num, "0"), ".")
	}
	return num + unit
}
