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
