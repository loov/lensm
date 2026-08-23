package goobj

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"loov.dev/lensm/internal/disasm"
)

// buildC compiles src with the platform C compiler and returns the
// binary's path, skipping the test when there is no usable compiler.
func buildC(t *testing.T, name, src string) string {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	if _, err := exec.LookPath(cc); err != nil {
		t.Skipf("no C compiler: %v", err)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, name)
	if err := os.WriteFile(source, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "prog")
	// -O1 so the loop is real code rather than a pile of spills, -g for
	// the line table this test is about.
	cmd := exec.Command(cc, "-g", "-O1", "-o", binary, source)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("%s: %v\n%s", cc, err, out)
	}
	return binary
}

const cSumInts = `#include <stdio.h>

__attribute__((noinline))
long sum_ints(const long *xs, int n) {
	long total = 0;
	for (int i = 0; i < n; i++) {
		total += xs[i];
	}
	return total;
}

int main(void) {
	long xs[] = {1, 2, 3};
	printf("%ld\n", sum_ints(xs, 3));
	return 0;
}
`

// TestLoadC checks a binary from the platform C compiler: functions come
// from the symbol table and source from DWARF, which on macOS lives in
// the dSYM bundle beside the binary rather than inside it.
func TestLoadC(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a C program")
	}
	file, err := Load(buildC(t, "prog.c", cSumInts))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var sumInts disasm.Func
	for _, fn := range file.Funcs() {
		if fn.Name() == "sum_ints" {
			sumInts = fn
		}
	}
	if sumInts == nil {
		t.Fatal("sum_ints not found")
	}
	code, err := sumInts.Load(disasm.Options{Context: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(code.Insts) == 0 {
		t.Fatal("sum_ints disassembled to 0 instructions")
	}
	if !strings.HasSuffix(code.File, "prog.c") {
		t.Errorf("File = %q, want the C source", code.File)
	}
	lines := map[int]bool{}
	for _, in := range code.Insts {
		if in.Line != 0 {
			lines[in.Line] = true
		}
	}
	// The loop body and the return are distinct statements, so a working
	// line table maps the body to more than one of them.
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
}

const cppSumInts = `#include <cstdio>
#include <vector>

__attribute__((noinline))
long sum_ints(const std::vector<long> &xs) {
	long total = 0;
	for (long x : xs) {
		total += x;
	}
	return total;
}

int main() {
	std::vector<long> xs{1, 2, 3};
	printf("%ld\n", sum_ints(xs));
	return 0;
}
`

// TestLoadCPP checks a C++ binary. Names come from the symbol table, so
// they are the mangled ones; the source mapping is what has to work.
func TestLoadCPP(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a C++ program")
	}
	binary := buildCPP(t, cppSumInts)
	file, err := Load(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var sumInts disasm.Func
	for _, fn := range file.Funcs() {
		if strings.Contains(fn.Name(), "sum_ints") {
			sumInts = fn
		}
	}
	if sumInts == nil {
		t.Fatal("no symbol mentioning sum_ints")
	}
	code, err := sumInts.Load(disasm.Options{Context: 1})
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, in := range code.Insts {
		if in.Line != 0 {
			lines++
		}
	}
	if lines == 0 {
		t.Error("no instruction carries a source position")
	}
	if len(code.Source) == 0 {
		t.Error("no source loaded")
	}
}

func buildCPP(t *testing.T, src string) string {
	t.Helper()
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if _, err := exec.LookPath(cxx); err != nil {
		t.Skipf("no C++ compiler: %v", err)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "prog.cpp")
	if err := os.WriteFile(source, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "prog")
	if out, err := exec.Command(cxx, "-g", "-O1", "-o", binary, source).CombinedOutput(); err != nil {
		t.Skipf("%s: %v\n%s", cxx, err, out)
	}
	return binary
}
