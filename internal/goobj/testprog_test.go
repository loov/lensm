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
	// ELF, PE and Mach-O.
	for _, target := range []struct{ os, arch string }{
		{"linux", "amd64"},
		{"linux", "arm64"},
		{"windows", "amd64"},
		{"darwin", "arm64"},
	} {
		arch := target.arch
		t.Run(target.os+"/"+arch, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "testprog_"+arch+".exe")
			cmd := exec.Command("go", "build", "-o", bin, "./testdata/testprog")
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "GOOS="+target.os, "GOARCH="+arch, "CGO_ENABLED=0")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("go build: %v\n%s", err, out)
			}

			file, err := Load(bin)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()

			checkRefs(t, file)

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
				} else if in := code.Insts[0]; in.NativeText == "" || in.Mnemonic == "" {
					t.Errorf("main.sumInts has no native syntax or mnemonic: %+v", in)
				} else if in.Line == 0 || !strings.HasSuffix(in.File, "main.go") {
					t.Errorf("main.sumInts has no source location: %+v", in)
				}
				return
			}
			t.Fatal("main.sumInts not found in binary")
		})
	}
}

// checkRefs checks that a tail call links to its target and that no
// address computation is drawn as a jump.
func checkRefs(t *testing.T, file *File) {
	t.Helper()
	tailCall := false
	for _, fn := range file.Funcs() {
		code, err := fn.Load(disasm.Options{})
		if err != nil {
			continue
		}
		for _, in := range code.Insts {
			if fn.Name() == "runtime.morestack_noctxt.abi0" && in.Call == "runtime.morestack.abi0" {
				tailCall = true
			}
			if in.RefPC != 0 && (in.Mnemonic == "ADRP" || in.Mnemonic == "ADR") {
				t.Fatalf("%s: %s is drawn as a jump", fn.Name(), in.Text)
			}
		}
	}
	if !tailCall {
		t.Error("runtime.morestack_noctxt.abi0 has no link to runtime.morestack.abi0")
	}
}
