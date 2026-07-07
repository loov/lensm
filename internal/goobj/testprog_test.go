package goobj

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"loov.dev/lensm/internal/disasm"
)

// TestLoadCrossCompiledTestprog cross-compiles testdata/testprog for each
// supported architecture and checks the result disassembles.
func TestLoadCrossCompiledTestprog(t *testing.T) {
	if testing.Short() {
		t.Skip("builds test binaries")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "testprog_"+arch+".exe")
			cmd := exec.Command("go", "build", "-o", bin, "./testdata/testprog")
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH="+arch, "CGO_ENABLED=0")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go build: %v\n%s", err, out)
			}

			file, err := Load(bin)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()

			for _, fn := range file.Funcs() {
				if !strings.Contains(fn.Name(), "main.sumInts") {
					continue
				}
				code, err := fn.Load(disasm.Options{Context: 1})
				if err != nil {
					t.Fatal(err)
				}
				if code.Arch != arch {
					t.Errorf("Arch = %q, want %q", code.Arch, arch)
				}
				if len(code.Insts) == 0 {
					t.Error("main.sumInts disassembled to 0 instructions")
				} else if code.Insts[0].NativeText == "" {
					t.Error("main.sumInts has no native syntax")
				}
				return
			}
			t.Fatal("main.sumInts not found in binary")
		})
	}
}
