package gobuild

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRun(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a program")
	}
	dir, _ := filepath.Abs("../../testdata/testprog")
	if !IsPackage(dir) || IsPackage(filepath.Join(dir, "main.go")) {
		t.Fatal("IsPackage")
	}
	b, err := Run(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	main := filepath.Join(dir, "main.go")
	want := map[string]int{"isInBounds": 31, "escapes": 89}
	for _, d := range b.Notes {
		if line, ok := want[d.Code]; ok && d.File == main && d.Line == line {
			if d.Code == "escapes" && (len(d.Related) == 0 || d.Related[0].File != main) {
				t.Errorf("escapes without its flow: %+v", d)
			}
			delete(want, d.Code)
		}
	}
	if len(want) > 0 {
		t.Errorf("missing %v in %+v", want, b.Notes)
	}
}

func TestRunLibrary(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	if _, err := Run(context.Background(), "../disasm"); err == nil {
		t.Error("built a library package")
	}
}
