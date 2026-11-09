package goobj

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eliben/watgo"

	"loov.dev/lensm/internal/disasm"
)

// TestWasmStatements checks the statements spelled for $f in small
// modules, leaving out the rows without one.
func TestWasmStatements(t *testing.T) {
	tests := []struct {
		name   string
		module string
		want   []string
	}{{
		name: "precedence",
		module: `(func $f (param i32 i32) (result i32)
			local.get 0 local.get 1 local.get 0 i32.sub i32.sub
			local.get 0 local.get 1 i32.add i32.const 2 i32.mul
			i32.mul)`,
		want: []string{"return (p0 - (p1 - p0)) * ((p0 + p1) * 2)"},
	}, {
		name: "and and shift inside arithmetic",
		module: `(func $f (param i32) (result i32)
			local.get 0 i32.const 3 i32.and i32.const 1 i32.sub
			local.get 0 i32.const 2 i32.shl
			i32.add)`,
		want: []string{"return (p0 & 3) - 1 + (p0 << 2)"},
	}, {
		name: "unsigned and negated comparisons",
		module: `(func $f (param i32 i32) (result i32)
			local.get 0 local.get 1 i32.lt_u i32.eqz)`,
		want: []string{"return p0 >=u p1"},
	}, {
		name: "a float comparison is not turned around",
		module: `(func $f (param f64) (result i32)
			local.get 0 local.get 0 f64.lt i32.eqz)`,
		want: []string{"return (p0 < p0) == 0"},
	}, {
		name: "widened comparison and wrapped extension",
		module: `(func $f (param i32) (result i32)
			local.get 0 i32.eqz i64.extend_i32_u i64.eqz
			local.get 0 i64.extend_i32_u i32.wrap_i64
			i32.add)`,
		want: []string{"return (p0 != 0) + p0"},
	}, {
		name: "select",
		module: `(func $f (param i32 i32) (result i32)
			local.get 0 local.get 1 local.get 0 i32.eqz select)`,
		want: []string{"return p0 == 0 ? p0 : p1"},
	}, {
		name: "a value read before its local changes",
		module: `(func $f (param i32 i32) (result i32)
			local.get 1 local.get 0 local.set 1 local.get 1 i32.add)`,
		want: []string{"t1 = p1", "p1 = p0", "return t1 + p1"},
	}, {
		name: "a load before a store",
		module: `(memory 1)
		(func $f (param i32) (result i32)
			local.get 0 i32.load offset=4
			local.get 0 i32.const 1 i32.store8
			local.get 0 i32.load8_u i32.add)`,
		want: []string{"t1 = i32[p0 + 4]", "i8[p0] = 1", "return t1 + u8[p0]"},
	}, {
		name: "a call before what reads after it",
		module: `(global (mut i32) (i32.const 0))
		(func $g (result i32) global.get 0 i32.const 1 i32.add global.set 0 i32.const 5)
		(func $f (result i32) call $g global.get 0 i32.add)`,
		want: []string{"t1 = g()", "return t1 + g0"},
	}, {
		name: "a dropped call shows, a dropped constant doesn't",
		module: `(func $g (result i32) i32.const 5)
		(func $f call $g drop i32.const 1 drop)`,
		want: []string{"g()"},
	}, {
		name: "a block's value",
		module: `(func $f (param i32) (result i32)
			(block (result i32) i32.const 1 local.get 0 br_if 0 drop i32.const 2))`,
		want: []string{"block L1 {", "if (p0) { L1 = 1; break L1 }", "L1 = 2; } return L1"},
	}, {
		name: "loop",
		module: `(func $f (param i32) (result i32) (local i32)
			(loop
				local.get 1 local.get 0 i32.add local.set 1
				local.get 0 i32.const 1 i32.sub local.tee 0
				br_if 0)
			local.get 1)`,
		want: []string{"loop L1 {", "l1 = l1 + p0", "p0 = p0 - 1", "if (p0) continue L1", "}", "return l1"},
	}, {
		name: "switch",
		module: `(func $f (param i32) (result i32)
			(block (block (block local.get 0 br_table 0 1 2)
				i32.const 10 return)
				i32.const 20 return)
			i32.const 30)`,
		want: []string{"switch (p0)", "case 0:", "return 10", "case 1:", "return 20", "default:", "return 30"},
	}, {
		name: "goto case",
		module: `(func $f (param i32) (result i32)
			(loop (block (block local.get 0 br_table 0 1 1)
				i32.const 1 local.set 0 br 1)
				i32.const 7 return)
			i32.const 0)`,
		// Outside Go the selector may be read again, so its write shows.
		want: []string{"loop L1 {", "switch (p0)", "case 0:", "p0 = 1", "goto case 1", "case 1, default:", "return 7", "}", "return 0"},
	}, {
		name: "goto a value past the table",
		module: `(func $f (param i32) (result i32)
			(loop (block (block local.get 0 br_table 0 1)
				i32.const 5 local.set 0 br 1)
				i32.const 7 return)
			i32.const 0)`,
		want: []string{"loop L1 {", "switch (p0)", "case 0:", "p0 = 5", "goto default", "default:", "return 7", "}", "return 0"},
	}, {
		name: "a trap in a block keeps the values before it",
		module: `(func $f (param i32 i32) (result i32)
			local.get 0 local.get 1 (if (then unreachable)) i32.const 1 i32.add)`,
		want: []string{"if (p1) {", "unreachable", "}", "return p0 + 1"},
	}, {
		name: "a call runs before the block after it",
		module: `(func $g (result i32) i32.const 5)
		(func $f (param i32) (result i32)
			call $g local.get 0 (if (then unreachable)))`,
		want: []string{"t1 = g()", "if (p0) {", "unreachable", "} return t1"},
	}, {
		name: "a branch out of the function returns its value",
		module: `(func $f (param i32) (result i32)
			local.get 0 local.get 0 br_if 0 drop local.get 0 br 0)`,
		want: []string{"if (p0) return p0", "return p0"},
	}, {
		name: "an if names its label",
		module: `(func $f (param i32) (result i32)
			(if (result i32) (local.get 0)
				(then i32.const 1 local.get 0 br_if 0 drop i32.const 2)
				(else i32.const 3)))`,
		want: []string{"if L1 (p0) {", "if (p0) { L1 = 1; break L1 }", "L1 = 2; } else {", "L1 = 3; } return L1"},
	}, {
		name: "a br_table carries its value",
		module: `(func $f (param i32) (result i32)
			(block (result i32) i32.const 4 local.get 0 br_table 0 0))`,
		want: []string{"block L1 {", "L1 = 4; switch (p0) L1 else L1", "} return L1"},
	}, {
		name:   "a dropped memory.grow shows",
		module: `(memory 1) (func $f i32.const 1 memory.grow drop)`,
		want:   []string{"memory_grow(1)"},
	}, {
		name: "an address's base binds as a sum",
		module: `(memory 1) (func $f (param i32 i32 i32) (result i32)
			local.get 0 local.get 1 local.get 2 select i32.load offset=8
			local.get 0 i32.const 3 i32.and i32.load offset=4
			i32.add)`,
		want: []string{"return i32[(p2 ? p0 : p1) + 8] + i32[(p0 & 3) + 4]"},
	}, {
		name: "bitwise operators in a comparison",
		module: `(func $f (param i32 i32) (result i32)
			local.get 0 local.get 1 i32.and i32.eqz
			local.get 0 local.get 1 i32.or i32.const 4 i32.lt_s
			i32.and)`,
		want: []string{"return ((p0 & p1) == 0) & ((p0 | p1) < 4)"},
	}, {
		name: "a long value gets a name",
		module: `(func $f (param i32) (result i32)
			local.get 0 ` + strings.Repeat("i32.const 1234567 i32.add ", 20) + `)`,
		want: []string{"t1 = p0" + strings.Repeat(" + 1234567", 20) + " return t1"},
	}, {
		name: "an if's parameters, for either branch",
		module: `(func $f (param i32 i32) (result i32)
			local.get 0 local.get 1
			(if (param i32) (result i32) (then i32.const 1 i32.add) (else i32.const 2 i32.mul)))`,
		want: []string{"if L1 (p1) {", "L1 = p0 + 1; } else {", "L1 = p0 * 2; } return L1"},
	}, {
		name: "several values from a block",
		module: `(func $f (result i32)
			(block (result i32 i32) i32.const 1 i32.const 2) i32.add)`,
		want: []string{"block L1 {", "L1.0, L1.1 = 1, 2; }", "return L1.0 + L1.1"},
	}, {
		name: "several values from a call",
		module: `(func $g (result i32 i32) i32.const 1 i32.const 2)
		(func $f (result i32) call $g i32.add)`,
		want: []string{"t1, t2 = g()", "return t1 + t2"},
	}, {
		name: "a br_table that isn't a switch",
		module: `(func $f (param i32)
			(block (block local.get 0 i32.const 1 i32.add local.set 0 local.get 0 br_table 0 1)))`,
		want: []string{"block L1 {", "block L2 {", "p0 = p0 + 1", "switch (p0) L2 else L1", "}", "}"},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := wasmStatementsOf(t, "(module "+test.module+")")
			if !slices.Equal(got, test.want) {
				t.Errorf("got:\n\t%s\nwant:\n\t%s", strings.Join(got, "\n\t"), strings.Join(test.want, "\n\t"))
			}
		})
	}
}

// wasmStatementsOf compiles a module and spells $f in it.
func wasmStatementsOf(t *testing.T, module string) []string {
	t.Helper()
	bin, err := watgo.CompileWATToWASM([]byte(module))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "m.wasm")
	if err := os.WriteFile(path, bin, 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	fn := disasm.FindFunc(file, "f")
	if fn == nil {
		t.Fatal("no $f")
	}
	code, err := fn.Load(disasm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, in := range code.Insts {
		if in.NativeText != "" {
			got = append(got, in.NativeText)
		}
	}
	return got
}
