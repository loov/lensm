package wasmobj

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"loov.dev/lensm/internal/disasm"
)

func TestLoadRendersInstructions(t *testing.T) {
	file, err := Load(filepath.Join("..", "..", "testdata", "c-wasm", "example.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range file.Funcs() {
		if fn.Name() != "add" {
			continue
		}
		code, err := fn.Load(disasm.Options{})
		if err != nil {
			t.Fatal(err)
		}
		texts := make([]string, len(code.Insts))
		for i, in := range code.Insts {
			texts[i] = in.Text
		}
		if !slices.Contains(texts, "i32.add") {
			t.Fatalf("add has no i32.add:\n%s", strings.Join(texts, "\n"))
		}
		return
	}
	t.Fatal("function add not found")
}

// TestLoadGoWasm builds testdata/testprog for wasip1 and checks that
// names come through and calls resolve to other functions.
func TestLoadGoWasm(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a wasm binary")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "testprog.wasm")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/testprog")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	file, err := Load(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range file.Funcs() {
		if !strings.HasPrefix(fn.Name(), "main.main") {
			continue
		}
		code, err := fn.Load(disasm.Options{})
		if err != nil {
			t.Fatal(err)
		}
		for _, in := range code.Insts {
			if in.Call != "" {
				return
			}
		}
		t.Fatalf("%s has no resolved call", fn.Name())
	}
	t.Fatal("main.main not found")
}

// TestSourceMapping checks that instructions carry source positions from
// the module's Go line table and that the loaded source relates back to
// them. The mapping is per resume point, so a function maps to a handful
// of lines rather than one per instruction.
func TestSourceMapping(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a wasm binary")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "testprog.wasm")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/testprog")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	file, err := Load(bin)
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range file.Funcs() {
		if fn.Name() != "main.sumInts" {
			continue
		}
		code, err := fn.Load(disasm.Options{Context: 1})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(code.File, "main.go") {
			t.Errorf("File = %q", code.File)
		}
		lines := map[int]bool{}
		for _, in := range code.Insts {
			if in.Line != 0 {
				lines[in.Line] = true
			}
		}
		if len(lines) < 2 {
			t.Errorf("instructions map to %d distinct lines, want several: %v", len(lines), lines)
		}
		related := 0
		for _, src := range code.Source {
			for _, block := range src.Blocks {
				for _, ranges := range block.Related {
					related += len(ranges)
				}
			}
		}
		if related == 0 {
			t.Error("no source line relates back to the instructions")
		}
		return
	}
	t.Fatal("main.sumInts not found")
}
