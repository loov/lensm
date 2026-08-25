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
		// The fixture is checked in, so its debug info names the source
		// by the absolute path of the checkout it was built in. Reading
		// the source back only works there; TestSourceMapping covers
		// that wiring with a binary built by the test itself.
		if _, err := os.Stat(code.File); err == nil {
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
		}
		return
	}
	t.Fatal("main.sumInts not found")
}

// TestLoadTinyGo checks the TinyGo path: names from the name section and
// source positions from DWARF rather than from a Go pclntab.
func TestLoadTinyGo(t *testing.T) {
	file, err := Load(filepath.Join("..", "..", "testdata", "tinygo", "example.wasm"))
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
		// The body is a loop adding into a total, then the return: the
		// two statements of the function that generate code.
		lines := map[int]bool{}
		for _, in := range code.Insts {
			if in.Line != 0 {
				lines[in.Line] = true
			}
		}
		if !lines[7] || !lines[9] {
			t.Errorf("instructions map to lines %v, want 7 and 9 among them", lines)
		}
		// The fixture is checked in, so its debug info names the source
		// by the absolute path of the checkout it was built in. Reading
		// the source back only works there; TestSourceMapping covers
		// that wiring with a binary built by the test itself.
		if _, err := os.Stat(code.File); err == nil {
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
		}
		return
	}
	t.Fatal("main.sumInts not found")
}

// TestLoadComponent checks TinyGo's wasip2 output: a component, whose
// code lives in the core modules nested inside it.
func TestLoadComponent(t *testing.T) {
	file, err := Load(filepath.Join("..", "..", "testdata", "tinygo", "component.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	var sumInts disasm.Func
	for _, fn := range file.Funcs() {
		if fn.Name() == "main/main.sumInts" {
			sumInts = fn
		}
	}
	if sumInts == nil {
		t.Fatal("main/main.sumInts not found; names should be qualified by module")
	}
	// The nested module keeps its own DWARF, so source still maps.
	code, err := sumInts.Load(disasm.Options{Context: 1})
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
	if !lines[7] || !lines[9] {
		t.Errorf("instructions map to lines %v, want 7 and 9 among them", lines)
	}
}

// TestLoadEmptyComponent checks that a component with nothing in it is
// reported as such rather than as a version number.
func TestLoadEmptyComponent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.wasm")
	if err := os.WriteFile(path, []byte("\x00asm\x0d\x00\x01\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "core module") {
		t.Fatalf("err = %v, want it to report an empty component", err)
	}
}
