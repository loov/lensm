package goobj

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"loov.dev/lensm/internal/disasm"
)

func TestLoadCode_ConcurrentCallsShareCache(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a test binary")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	err := os.WriteFile(src, []byte(`package main

func main() { println(add(1, 2)) }

//go:noinline
func add(a, b int) int { return a + b }
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "example.exe")
	if out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	file, err := Load(bin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })

	funcs := file.Funcs()
	if len(funcs) > 32 {
		funcs = funcs[:32]
	}

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for _, fn := range funcs {
				_, _ = fn.Load(disasm.Options{Context: 1})
			}
		})
	}
	wg.Wait()
}
