package goobj

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
)

func TestGoArgNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.go")
	src := `package p
type T struct{}
func f(a []int, b string, c bool, d int64) (int, error) { return 0, nil }
func (t *T) m(x int32, y float64) (n int) { return 0 }
func g(s struct{}) {}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		fn   string
		want map[int64]string
	}{
		{"example.com/p.f", map[int64]string{
			0: "a.ptr", 8: "a.len", 16: "a.cap", 24: "b.ptr", 32: "b.len", 40: "c", 48: "d",
			56: "r0", 64: "r1.type", 72: "r1.data",
		}},
		{"example.com/p.(*T).m", map[int64]string{0: "t", 8: "x", 16: "y", 24: "n"}},
		{"example.com/p.g", nil},
		{"example.com/p.missing", nil},
	}
	for _, test := range tests {
		if got := goArgNames(path, test.fn); !maps.Equal(got, test.want) {
			t.Errorf("%s: got %v, want %v", test.fn, got, test.want)
		}
	}
}
