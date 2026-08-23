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
