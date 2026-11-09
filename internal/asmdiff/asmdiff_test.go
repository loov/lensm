package asmdiff

import (
	"math/rand/v2"
	"strings"
	"testing"
)

func TestDiff(t *testing.T) {
	old := []string{"MOVD 16(R28), R16", "CMP R16, RSP", "CALL runtime.panicIndex(SB)", "RET"}
	new := []string{"MOVD 16(R28), R16", "CMP R16, RSP", "ADD $1, R0", "RET"}
	got := Diff(old, new)
	var b strings.Builder
	for _, r := range got {
		b.WriteString([]string{"=", "-", "+"}[r.Op])
	}
	if b.String() != "==-+=" && b.String() != "==+-=" {
		t.Errorf("ops = %s", b.String())
	}
	for _, r := range got {
		if r.Op == Same && old[r.Old] != new[r.New] {
			t.Errorf("same row pairs %q with %q", old[r.Old], new[r.New])
		}
	}
	if len(Diff(nil, new)) != 4 || len(Diff(old, nil)) != 4 || len(Diff(nil, nil)) != 0 {
		t.Error("diff against nothing")
	}
}

func TestNormalize(t *testing.T) {
	for _, c := range [][2]string{
		{"ADRP 1159168(PC), R27", "ADRP #(PC), R27"},
		{"B .+0X14 <MAIN.SPIN+0X60>", "B .#"},
		{"BLS 24(PC)", "BLS #"},
		{"BHI -7(PC)", "BHI #"},
		{"MOVD 16(R28), R16", "MOVD 16(R28), R16"},
		{"CALL runtime.morestack(SB)", "CALL runtime.morestack(SB)"},
	} {
		if got := Normalize(c[0]); got != c[1] {
			t.Errorf("Normalize(%q) = %q, want %q", c[0], got, c[1])
		}
	}
}

// TestDiffRandom checks that the rows cover both sides in order and pair
// only equal rows.
func TestDiffRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 200 {
		gen := func() []string {
			s := make([]string, r.IntN(30))
			for i := range s {
				s[i] = string(rune('A' + r.IntN(4)))
			}
			return s
		}
		a, b := gen(), gen()
		i, j := 0, 0
		for _, row := range Diff(a, b) {
			switch row.Op {
			case Same:
				if row.Old != i || row.New != j || a[i] != b[j] {
					t.Fatalf("%q %q: bad same row %+v at %d,%d", a, b, row, i, j)
				}
				i, j = i+1, j+1
			case Removed:
				if row.Old != i {
					t.Fatalf("%q %q: bad removed row %+v at %d", a, b, row, i)
				}
				i++
			case Added:
				if row.New != j {
					t.Fatalf("%q %q: bad added row %+v at %d", a, b, row, j)
				}
				j++
			}
		}
		if i != len(a) || j != len(b) {
			t.Fatalf("%q %q: rows end at %d,%d", a, b, i, j)
		}
		same := 0
		for _, row := range Diff(a, b) {
			if row.Op == Same {
				same++
			}
		}
		if want := lcs(a, b); same != want {
			t.Fatalf("%q %q: %d same rows, longest common is %d", a, b, same, want)
		}
	}
}

// lcs is the length of the longest common subsequence, by table.
func lcs(a, b []string) int {
	t := make([][]int, len(a)+1)
	for i := range t {
		t[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				t[i][j] = t[i+1][j+1] + 1
			} else {
				t[i][j] = max(t[i+1][j], t[i][j+1])
			}
		}
	}
	return t[0][0]
}
