package pprof

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/goobj"
)

// TestLoadPlacesSamples profiles a program that spins in one function
// and checks that the samples land in its code in the binary.
func TestLoadPlacesSamples(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a program")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	err := os.WriteFile(src, []byte(`package main

import (
	"os"
	"runtime/pprof"
	"time"
)

var sink int

//go:noinline
func spin(until time.Time) {
	for time.Now().Before(until) {
		for i := range 100000 {
			sink += i * i
		}
	}
}

func main() {
	f, _ := os.Create(os.Args[1])
	pprof.StartCPUProfile(f)
	spin(time.Now().Add(300 * time.Millisecond))
	pprof.StopCPUProfile()
	f.Close()
}
`), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "spin.exe")
	if out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	prof := filepath.Join(dir, "cpu.pprof")
	if out, err := exec.Command(bin, prof).CombinedOutput(); err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}

	file, err := goobj.Load(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	lookup := func(name string) (uint64, uint64, bool) {
		fn := disasm.FindFunc(file, name)
		if fn == nil {
			return 0, 0, false
		}
		addr, size := fn.(disasm.Ranged).Range()
		return addr, size, true
	}
	p, err := Load(prof, lookup)
	if err != nil {
		t.Fatal(err)
	}
	addr, size, _ := lookup("main.spin")
	var in, all int64
	for pc, v := range p.Flat {
		all += v
		if addr <= pc && pc < addr+size {
			in += v
		}
	}
	if all == 0 || in*2 < all {
		t.Errorf("%d of %d %s in main.spin [%#x, %#x)", in, all, p.Type, addr, addr+size)
	}
	// With the shift right, every sample is at the start of an
	// instruction.
	code, err := disasm.FindFunc(file, "main.spin").Load(disasm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	starts := map[uint64]bool{}
	for _, ix := range code.Insts {
		starts[ix.PC] = true
	}
	for pc := range p.Flat {
		if addr <= pc && pc < addr+size && !starts[pc] {
			t.Errorf("sample at %#x is inside an instruction", pc)
		}
	}
}
