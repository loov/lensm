package objfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUnjoinCompDir(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "prog.c")
	if err := os.WriteFile(real, []byte("int main(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// What debug/dwarf hands over for an absolute name recorded with a
	// compilation directory: the two pasted together.
	joined := filepath.Join(dir, "build") + real
	if got := unjoinCompDir(filepath.Join(dir, "build"), joined); got != real {
		t.Errorf("unjoinCompDir(joined) = %q, want %q", got, real)
	}
	// A name that really is under the compilation directory is left be.
	if got := unjoinCompDir(dir, real); got != real {
		t.Errorf("unjoinCompDir(real) = %q, want it unchanged", got)
	}
	// Nothing to check against on disk: keep what dwarf gave.
	missing := filepath.Join(dir, "build", "nowhere", "x.c")
	if got := unjoinCompDir(filepath.Join(dir, "build"), missing); got != missing {
		t.Errorf("unjoinCompDir(missing) = %q, want it unchanged", got)
	}
}

func TestDemangle(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"_Z8sum_intsRKSt6vectorIlSaIlEE", "sum_ints(std::vector<long, std::allocator<long> > const&)"},
		{"_ZN3foo3barEi", "foo::bar(int)"},
		// Rust, in both of its mangling schemes.
		{"_ZN4prog8sum_ints17h8ff3155123bef508E", "prog::sum_ints"},
		{"_RNvCshN3ET9YTcYm_4prog8sum_ints", "prog::sum_ints"},
		// Not mangled: Go, C and assembly symbols pass through.
		{"main.sumInts", "main.sumInts"},
		{"sum_ints", "sum_ints"},
		{"_main", "_main"},
		// Mangled-looking but not decodable.
		{"_Znope", "_Znope"},
	} {
		if got := Demangle(test.in); got != test.want {
			t.Errorf("Demangle(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}
