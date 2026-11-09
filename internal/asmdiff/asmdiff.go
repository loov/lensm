// Package asmdiff aligns the instructions of two builds of a function.
package asmdiff

import (
	"regexp"
	"strings"
)

// Op says what a row of a diff holds.
type Op uint8

const (
	// Same rows have an instruction in both builds.
	Same Op = iota
	// Removed rows have one only in the old build.
	Removed
	// Added rows have one only in the new build.
	Added
)

// Row is one aligned row: indices into the old and new instructions,
// -1 on the side that has none.
type Row struct {
	Op       Op
	Old, New int
}

// numbers are the long digit runs in addresses, offsets from the pc,
// and branch distances such as 24(PC), which all shift between builds
// when nothing else changed.
var numbers = regexp.MustCompile(`(0[xX])?[0-9a-fA-F]*[0-9][0-9a-fA-F]{4,}|[+-]0[xX][0-9a-fA-F]+|-?[0-9]+\(PC\)`)

// Normalize drops what differs between builds of the same code: those
// numbers and the symbol notes after branches, such as <main.f+0x24>.
func Normalize(text string) string {
	if i := strings.IndexByte(text, '<'); i >= 0 {
		text = strings.TrimSpace(text[:i])
	}
	return numbers.ReplaceAllString(text, "#")
}

// maxEdits bounds the work: the steps cost memory growing with the
// square of the edits, and builds that far apart align poorly anyway.
const maxEdits = 2000

// replaceAll is the diff that removes all n old rows and adds all m new.
func replaceAll(n, m int) []Row {
	rows := make([]Row, 0, n+m)
	for i := range n {
		rows = append(rows, Row{Removed, i, -1})
	}
	for j := range m {
		rows = append(rows, Row{Added, -1, j})
	}
	return rows
}

// Diff aligns a and b with the fewest added and removed rows, by
// Myers' algorithm, comparing the normalized text.
func Diff(a, b []string) []Row {
	na, nb := make([]string, len(a)), make([]string, len(b))
	for i, s := range a {
		na[i] = Normalize(s)
	}
	for i, s := range b {
		nb[i] = Normalize(s)
	}
	n, m := len(na), len(nb)
	offset := n + m + 1
	v := make([]int, 2*offset+1)
	// trace[d] is the part of v that step d reads, k in [-d-1, d+1].
	var trace [][]int
	found := false
	for d := 0; d <= n+m && !found; d++ {
		if d > maxEdits {
			return replaceAll(n, m)
		}
		trace = append(trace, append([]int(nil), v[offset-d-1:offset+d+2]...))
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && na[x] == nb[y] {
				x, y = x+1, y+1
			}
			v[offset+k] = x
			if x >= n && y >= m {
				found = true
				break
			}
		}
	}

	// Walk back from the end through the saved steps.
	var rows []Row
	x, y := n, m
	for d := len(trace) - 1; d >= 0 && (x > 0 || y > 0); d-- {
		at := func(k int) int { return trace[d][k+d+1] }
		k := x - y
		var prevK int
		if k == -d || (k != d && at(k-1) < at(k+1)) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := at(prevK)
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x, y = x-1, y-1
			rows = append(rows, Row{Same, x, y})
		}
		if d == 0 {
			break
		}
		if x == prevX {
			y--
			rows = append(rows, Row{Added, -1, y})
		} else {
			x--
			rows = append(rows, Row{Removed, x, -1})
		}
	}
	for x > 0 && y > 0 {
		x, y = x-1, y-1
		rows = append(rows, Row{Same, x, y})
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return rows
}
