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

// TestLoadCPP checks a C++ binary: mangled symbols come back as the
// names they had in the source, and the source mapping works.
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
		if strings.HasPrefix(fn.Name(), "sum_ints(") {
			sumInts = fn
		}
	}
	if sumInts == nil {
		names := make([]string, 0, len(file.Funcs()))
		for _, fn := range file.Funcs() {
			names = append(names, fn.Name())
		}
		t.Fatalf("no demangled sum_ints among %v", names)
	}
	// The signature is what keeps overloads apart, so it has to survive.
	if !strings.Contains(sumInts.Name(), "vector<long") {
		t.Errorf("Name = %q, want the parameter types", sumInts.Name())
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

const rustSumInts = `#[inline(never)]
pub fn sum_ints(xs: &[i64]) -> i64 {
    let mut total = 0;
    for x in xs {
        total += x;
    }
    total
}

fn main() {
    let xs = vec![1i64, 2, 3];
    println!("{}", sum_ints(&xs));
}
`

// TestLoadRust checks a Rust binary: symbols come back demangled, and
// the function's file is the one it was written in rather than whatever
// got inlined into its first instruction.
func TestLoadRust(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a Rust program")
	}
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Skipf("no rustc: %v", err)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "prog.rs")
	if err := os.WriteFile(source, []byte(rustSumInts), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "prog")
	cmd := exec.Command("rustc", "-g", "-O", "-o", binary, source)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("rustc: %v\n%s", err, out)
	}

	file, err := Load(binary)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var sumInts disasm.Func
	for _, fn := range file.Funcs() {
		if fn.Name() == "prog::sum_ints" {
			sumInts = fn
		}
	}
	if sumInts == nil {
		t.Fatal("prog::sum_ints not found; Rust symbols should demangle")
	}
	code, err := sumInts.Load(disasm.Options{Context: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(code.File, "prog.rs") {
		t.Errorf("File = %q, want the Rust source", code.File)
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
}

// TestLoadRISCV32 loads a TinyGo ESP32-C3 build: a 32-bit RISC-V ELF with
// DWARF but no pclntab, decoded by the riscv64 decoder.
func TestLoadRISCV32(t *testing.T) {
	file, err := Load(filepath.Join("..", "..", "testdata", "tinygo", "esp32c3.elf"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var sumInts disasm.Func
	for _, fn := range file.Funcs() {
		if fn.Name() == "main.sumInts" {
			sumInts = fn
		}
	}
	if sumInts == nil {
		t.Fatal("main.sumInts not found")
	}
	code, err := sumInts.Load(disasm.Options{Context: 1})
	if err != nil {
		t.Fatal(err)
	}
	if code.Arch != "riscv32" {
		t.Errorf("Arch = %q, want riscv32", code.Arch)
	}
	if !strings.HasSuffix(code.File, "main.go") {
		t.Errorf("File = %q, want main.go", code.File)
	}
	var lines, decoded int
	for _, in := range code.Insts {
		if in.Line != 0 {
			lines++
		}
		if in.Text != "" && !strings.HasPrefix(in.Text, "BYTE") {
			decoded++
		}
	}
	if lines == 0 {
		t.Error("no instruction carries a source position")
	}
	if decoded == 0 {
		t.Error("no instruction decoded")
	}
}

// TestLoadThumb loads a TinyGo Raspberry Pi Pico build: Thumb code in a
// 32-bit ARM ELF, with literal pools marked by $d mapping symbols and
// function symbols carrying the Thumb bit.
func TestLoadThumb(t *testing.T) {
	file, err := Load(filepath.Join("..", "..", "testdata", "tinygo", "pico.elf"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	funcs := map[string]disasm.Func{}
	for _, fn := range file.Funcs() {
		funcs[fn.Name()] = fn
	}
	sumInts := funcs["main.sumInts"]
	if sumInts == nil {
		t.Fatal("main.sumInts not found")
	}
	code, err := sumInts.Load(disasm.Options{Context: 1})
	if err != nil {
		t.Fatal(err)
	}
	if code.Arch != "arm" {
		t.Errorf("Arch = %q, want arm", code.Arch)
	}
	if !strings.HasSuffix(code.File, "main.go") {
		t.Errorf("File = %q, want main.go", code.File)
	}
	var texts []string
	var jumps int
	for _, in := range code.Insts {
		if in.Text == "" {
			continue
		}
		texts = append(texts, in.Text)
		if in.RefPC != 0 {
			jumps++
		}
	}
	// The loop: a compare, a conditional branch forward, the body and a
	// branch back; the function symbol's Thumb bit must not shift the
	// code by a byte.
	want := []string{"movs r2, #0", "mov r1, r2", "cmp r2, #0xc"}
	for i, w := range want {
		if i >= len(texts) || texts[i] != w {
			t.Errorf("instruction %d = %q, want %q", i, texts[i], w)
		}
	}
	if jumps != 2 {
		t.Errorf("%d branches with targets, want 2; instructions: %q", jumps, texts)
	}

	// The scheduler wrapper calls machine functions and ends with a
	// literal pool: calls must resolve to names and the pool must show
	// as data words, not garbage instructions.
	wrapper := funcs["runtime.run$1$gowrapper"]
	if wrapper == nil {
		t.Fatal("runtime.run$1$gowrapper not found")
	}
	code, err = wrapper.Load(disasm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var calls, words int
	for _, in := range code.Insts {
		if in.Call != "" {
			calls++
		}
		if strings.HasPrefix(in.Text, ".word ") {
			words++
		}
	}
	if calls == 0 {
		t.Error("no bl resolved to a callee name")
	}
	if words == 0 {
		t.Error("literal pool not rendered as .word data")
	}
}

// TestLoadAVR loads a TinyGo Arduino Uno build: an 8-bit AVR ELF with
// DWARF, where branch targets are resolved from word-addressed fields.
func TestLoadAVR(t *testing.T) {
	file, err := Load(filepath.Join("..", "..", "testdata", "tinygo", "arduino.elf"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var sumInts disasm.Func
	for _, fn := range file.Funcs() {
		if fn.Name() == "main.sumInts" {
			sumInts = fn
		}
	}
	if sumInts == nil {
		t.Fatal("main.sumInts not found")
	}
	code, err := sumInts.Load(disasm.Options{Context: 1})
	if err != nil {
		t.Fatal(err)
	}
	if code.Arch != "avr" {
		t.Errorf("Arch = %q, want avr", code.Arch)
	}
	if !strings.HasSuffix(code.File, "main.go") {
		t.Errorf("File = %q, want main.go", code.File)
	}
	var lines, branches int
	for _, in := range code.Insts {
		if in.Line != 0 {
			lines++
		}
		if in.RefPC != 0 {
			branches++
		}
		if strings.HasPrefix(in.Text, "BYTE") {
			t.Errorf("undecoded instruction at %#x", in.PC)
		}
	}
	if len(code.Insts) == 0 || !strings.HasPrefix(code.Insts[0].Text, "push r12") {
		t.Errorf("unexpected start: %+v", code.Insts[:min(3, len(code.Insts))])
	}
	if lines == 0 {
		t.Error("no instruction carries a source position")
	}
	if branches == 0 {
		t.Error("no branch target resolved")
	}
}
