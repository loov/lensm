package goobj

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/eliben/watgo"
	"github.com/eliben/watgo/wasmir"

	"loov.dev/lensm/internal/disasm"
)

// wasmModule decodes the whole module once, for the types of calls and
// blocks and the names of locals and globals, which the listing of a
// single function doesn't carry. It is nil for a component or a module
// watgo can't decode, and the statements are then left out.
func (file *File) wasmModule() *wasmir.Module {
	if file.wasmTried {
		return file.wasm
	}
	file.wasmTried = true
	data, err := os.ReadFile(file.path)
	// A core module is version 1; a component has another version.
	if err != nil || len(data) < 8 || string(data[:8]) != "\x00asm\x01\x00\x00\x00" {
		return nil
	}
	file.wasm, _ = watgo.DecodeWASM(data)
	return file.wasm
}

// expr is an expression on the wasm value stack and how tightly it
// binds, as in Go: 5 for * / % << >> &, 4 for + - | ^, 3 for
// comparisons, 1 for the conditional and 9 for anything that needs no
// parentheses.
type expr struct {
	text string
	prec int
	// not is the comparison's negation, for eqz to use.
	not string
	// shift marks a shift, which Go binds tighter than + and C looser,
	// so it gets parentheses inside a sum either way.
	shift bool
	// row is the instruction that completed it, and reads what its
	// value depends on, so a statement that changes that first can
	// give the value a name on its own row.
	row   int
	reads reads
}

// reads is what an expression's value depends on.
type reads struct {
	locals, globals []uint32
	memory, call    bool
}

func (r *reads) add(o reads) {
	r.locals = append(r.locals, o.locals...)
	r.globals = append(r.globals, o.globals...)
	r.memory = r.memory || o.memory
	r.call = r.call || o.call
}

// wasmStatements spells the instructions of the function at index idx
// in the module, the imports counted, as statements: each instruction
// that completes one gets its text, such as "l2 = l2 + i32[p0 + l1]",
// and the instructions feeding it get none. insts are the function's
// rows, in the order of its body.
func wasmStatements(m *wasmir.Module, idx int, insts []disasm.Inst) {
	funcs := wasmFuncTypes(m)
	imported := len(funcs) - len(m.Funcs)
	if idx < imported || idx >= len(funcs) {
		return
	}
	fn := &m.Funcs[idx-imported]
	body := fn.Body
	if len(body) > 0 && body[len(body)-1].Kind == wasmir.InstrEnd {
		body = body[:len(body)-1]
	}
	if len(body) != len(insts) {
		return
	}
	ftype := typeOf(m, fn.TypeIdx)
	params := len(ftype.Params)
	name := func(i uint32) string {
		if int(i) < params {
			if int(i) < len(fn.ParamNames) && fn.ParamNames[i] != "" {
				return strings.TrimPrefix(fn.ParamNames[i], "$")
			}
			return fmt.Sprint("p", i)
		}
		if k := int(i) - params; k < len(fn.LocalNames) && fn.LocalNames[k] != "" {
			return strings.TrimPrefix(fn.LocalNames[k], "$")
		}
		return fmt.Sprint("l", i)
	}
	globals := wasmGlobalNames(m)
	if isGoWasm(m) {
		for i, name := range goWasmGlobals {
			if i < len(globals) && globals[i] == "" {
				globals[i] = name
			}
		}
	}
	global := func(i uint32) string {
		if int(i) < len(globals) && globals[i] != "" {
			return globals[i]
		}
		return fmt.Sprint("g", i)
	}

	// Name the blocks that branches go to, in order.
	type block struct {
		label  string
		loop   bool
		height int // the stack's height when the block started
		result int // the values the block leaves
	}
	targeted := wasmTargets(body)
	labels := 0

	var stack []expr
	current := 0
	// popped gathers what the instruction's operands read, and own what
	// the instruction itself does, for the value it pushes.
	var popped, own reads
	pop := func() expr {
		if len(stack) == 0 {
			return expr{text: "?", prec: 9}
		}
		e := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		popped.add(e.reads)
		return e
	}
	temps := 0
	// settle names the values waiting on the stack that what is about
	// to happen would change, on the rows that computed them, so the
	// statements keep their order.
	settle := func(changes func(r reads) bool) {
		for k := range stack {
			e := &stack[k]
			if !changes(e.reads) || e.row < 0 {
				continue
			}
			temps++
			t := fmt.Sprint("t", temps)
			assign := t + " = " + e.text
			if insts[e.row].NativeText != "" {
				assign = insts[e.row].NativeText + "; " + assign
			}
			insts[e.row].NativeText = assign
			*e = expr{text: t, prec: 9, row: -1}
		}
	}
	readsLocal := func(n uint32) func(reads) bool {
		return func(r reads) bool { return slices.Contains(r.locals, n) }
	}
	readsGlobal := func(n uint32) func(reads) bool {
		return func(r reads) bool { return r.call || slices.Contains(r.globals, n) }
	}
	readsMemory := func(r reads) bool { return r.memory || r.call }
	readsAny := func(r reads) bool { return r.memory || r.call || len(r.globals) > 0 }
	popN := func(n int) []string {
		args := make([]string, n)
		for i := n - 1; i >= 0; i-- {
			args[i] = pop().text
		}
		return args
	}
	pushExpr := func(e expr) {
		e.row, e.reads = current, popped
		e.reads.add(own)
		stack = append(stack, e)
	}
	push := func(text string, prec int) { pushExpr(expr{text: text, prec: prec}) }
	// wrap puts e in parentheses when it binds looser than prec.
	wrap := func(e expr, prec int) string {
		if e.prec < prec || e.shift && (prec == 4 || prec == 5) {
			return "(" + e.text + ")"
		}
		return e.text
	}
	frames := []block{{label: "", result: len(ftype.Results)}}
	// leftover spells the values a block leaves as an assignment to its
	// label, so they show before the block's end.
	leftover := func(b *block) string {
		if len(stack) <= b.height || b.result == 0 {
			stack = stack[:min(len(stack), b.height)]
			return ""
		}
		v := pop().text
		stack = stack[:min(len(stack), b.height)]
		return b.label + " = " + v + "; "
	}

	for i := range body {
		in := &body[i]
		current, popped, own = i, reads{}, reads{}
		op, arg, _ := strings.Cut(insts[i].Text, " ")
		typ, short, _ := strings.Cut(op, ".")
		text := ""
		switch {
		case op == "block" || op == "loop" || op == "if" || op == "try_table":
			b := block{loop: op == "loop", result: blockResults(m, in)}
			if targeted[i] || b.result > 0 {
				labels++
				b.label = fmt.Sprint("L", labels)
			}
			cond := ""
			if op == "if" {
				cond = pop().text
			}
			b.height = len(stack)
			frames = append(frames, b)
			switch op {
			case "if":
				text = "if (" + cond + ") {"
			case "try_table":
				text = "try " + b.label + " {"
			default:
				text = strings.TrimSpace(op+" "+b.label) + " {"
			}
		case op == "else":
			b := &frames[len(frames)-1]
			text = leftover(b) + "} else {"
		case op == "end":
			if len(frames) == 1 {
				break
			}
			b := frames[len(frames)-1]
			text = leftover(&b) + "}"
			frames = frames[:len(frames)-1]
			if b.result > 0 {
				push(b.label, 9)
			}
		case op == "br" || op == "br_if" || op == "br_table":
			cond := ""
			if op != "br" {
				cond = pop().text
			}
			target := func(depth uint32) string {
				if int(depth) >= len(frames) {
					return "?"
				}
				b := frames[len(frames)-1-int(depth)]
				switch {
				case int(depth) == len(frames)-1:
					return "return"
				case b.loop:
					return "continue " + b.label
				}
				return "break " + b.label
			}
			// A branch to a block that gives a value carries it, the top
			// of the stack, and br_if leaves it there when not taken.
			carry := func(depth uint32) string {
				if int(depth) >= len(frames)-1 || len(stack) == 0 {
					return ""
				}
				b := frames[len(frames)-1-int(depth)]
				if b.loop || b.result == 0 {
					return ""
				}
				return b.label + " = " + stack[len(stack)-1].text + "; "
			}
			switch op {
			case "br":
				text = carry(in.BranchDepth) + target(in.BranchDepth)
			case "br_if":
				if c := carry(in.BranchDepth); c != "" {
					text = "if (" + cond + ") { " + c + target(in.BranchDepth) + " }"
				} else {
					text = "if (" + cond + ") " + target(in.BranchDepth)
				}
			default:
				var cases []string
				for _, d := range in.BranchTable {
					cases = append(cases, strings.TrimPrefix(strings.TrimPrefix(target(d), "break "), "continue "))
				}
				def := strings.TrimPrefix(strings.TrimPrefix(target(in.BranchDefault), "break "), "continue ")
				text = "switch (" + cond + ") " + strings.Join(cases, ", ") + " else " + def
			}
			if op != "br_if" {
				// What follows is unreachable until the block ends.
				stack = stack[:0:0]
			}
		case op == "return":
			text = strings.TrimSpace("return " + strings.Join(popN(len(ftype.Results)), ", "))
		case op == "unreachable":
			text = "unreachable"
			stack = stack[:0:0]
		case op == "nop":
		case op == "drop":
			text = pop().text
		case op == "select":
			c, b, a := pop(), pop(), pop()
			push(wrap(c, 3)+" ? "+wrap(a, 2)+" : "+wrap(b, 2), 1)
		case op == "local.get":
			own.locals = []uint32{in.LocalIndex}
			push(name(in.LocalIndex), 9)
		case op == "local.set":
			v := pop()
			settle(readsLocal(in.LocalIndex))
			text = name(in.LocalIndex) + " = " + v.text
		case op == "local.tee":
			v := pop()
			settle(readsLocal(in.LocalIndex))
			text = name(in.LocalIndex) + " = " + v.text
			popped, own = reads{}, reads{locals: []uint32{in.LocalIndex}}
			push(name(in.LocalIndex), 9)
		case op == "global.get":
			own.globals = []uint32{in.GlobalIndex}
			push(global(in.GlobalIndex), 9)
		case op == "global.set":
			v := pop()
			settle(readsGlobal(in.GlobalIndex))
			text = global(in.GlobalIndex) + " = " + v.text
		case op == "call" || op == "call_indirect" || op == "return_call":
			var t wasmir.TypeDef
			callee := strings.TrimPrefix(arg, "$")
			if op == "call_indirect" {
				t = typeOf(m, in.CallTypeIndex)
				callee = "table[" + pop().text + "]"
			} else if int(in.FuncIndex) < len(funcs) {
				t = funcs[in.FuncIndex]
			}
			call := callee + "(" + strings.Join(popN(len(t.Params)), ", ") + ")"
			settle(readsAny)
			own.call = true
			switch {
			case op == "return_call":
				text = "return " + call
			case len(t.Results) == 1:
				push(call, 9)
			default:
				text = call
				for range t.Results {
					push("?", 9)
				}
			}
		case short == "const":
			push(arg, 9)
		case strings.HasPrefix(short, "load"):
			own.memory = true
			push(memType(typ, short)+"["+address(pop(), in.MemoryOffset)+"]", 9)
		case strings.HasPrefix(short, "store"):
			v := pop()
			a := address(pop(), in.MemoryOffset)
			settle(readsMemory)
			text = memType(typ, short) + "[" + a + "] = " + v.text
		case op == "memory.size":
			own.memory = true
			push("memory_size()", 9)
		case op == "memory.grow":
			n := pop()
			settle(readsMemory)
			own.memory = true
			push("memory_grow("+n.text+")", 9)
		case op == "memory.copy" || op == "memory.fill":
			args := popN(3)
			settle(readsMemory)
			text = strings.ReplaceAll(op, ".", "_") + "(" + strings.Join(args, ", ") + ")"
		case binaryOps[short] != (binaryOp{}):
			o := binaryOps[short]
			b, a := pop(), pop()
			if o.fn {
				push(short+"("+a.text+", "+b.text+")", 9)
				break
			}
			e := expr{text: wrap(a, o.prec) + " " + o.text + " " + wrap(b, o.prec+1), prec: o.prec, shift: strings.HasPrefix(short, "sh")}
			// A float comparison is false either way for NaN, so only an
			// integer one turns around.
			if not, ok := negations[o.text]; ok && typ[0] == 'i' {
				e.not = wrap(a, o.prec) + " " + not + " " + wrap(b, o.prec+1)
			}
			pushExpr(e)
		case short == "eqz":
			a := pop()
			if a.not != "" {
				pushExpr(expr{text: a.not, prec: 3, not: a.text})
				break
			}
			pushExpr(expr{text: wrap(a, 4) + " == 0", prec: 3, not: wrap(a, 4) + " != 0"})
		case short == "neg":
			push("-"+wrap(pop(), 9), 9)
		case isUnary(short):
			push(convert(typ, short)+"("+pop().text+")", 9)
		default:
			// An instruction this doesn't know the operands of: show it
			// and start over with an unknown stack.
			text = insts[i].Text
			stack = stack[:0:0]
		}
		insts[i].NativeText = text
	}
	// The values left at the end are the function's results.
	if last := len(insts) - 1; last >= 0 && insts[last].NativeText == "" && len(ftype.Results) > 0 && len(stack) > 0 {
		insts[last].NativeText = "return " + strings.Join(popN(min(len(ftype.Results), len(stack))), ", ")
	}
}

// negations are the comparisons' opposites.
var negations = map[string]string{
	"==": "!=", "!=": "==",
	"<": ">=", ">=": "<", ">": "<=", "<=": ">",
	"<u": ">=u", ">=u": "<u", ">u": "<=u", "<=u": ">u",
}

type binaryOp struct {
	text string
	prec int
	fn   bool // spelled as a call: min(a, b)
}

// binaryOps are the operators of the instructions taking two values,
// by the name after the type. Unsigned operators end in u.
var binaryOps = map[string]binaryOp{
	"add": {"+", 4, false}, "sub": {"-", 4, false}, "mul": {"*", 5, false},
	"div": {"/", 5, false}, "div_s": {"/", 5, false}, "div_u": {"/u", 5, false},
	"rem_s": {"%", 5, false}, "rem_u": {"%u", 5, false},
	"and": {"&", 5, false}, "or": {"|", 4, false}, "xor": {"^", 4, false},
	"shl": {"<<", 5, false}, "shr_s": {">>", 5, false}, "shr_u": {">>u", 5, false},
	"rotl": {"", 9, true}, "rotr": {"", 9, true},
	"eq": {"==", 3, false}, "ne": {"!=", 3, false},
	"lt": {"<", 3, false}, "lt_s": {"<", 3, false}, "lt_u": {"<u", 3, false},
	"gt": {">", 3, false}, "gt_s": {">", 3, false}, "gt_u": {">u", 3, false},
	"le": {"<=", 3, false}, "le_s": {"<=", 3, false}, "le_u": {"<=u", 3, false},
	"ge": {">=", 3, false}, "ge_s": {">=", 3, false}, "ge_u": {">=u", 3, false},
	"min": {"", 9, true}, "max": {"", 9, true}, "copysign": {"", 9, true},
}

// isUnary reports whether the instruction takes one value and gives
// one: arithmetic such as clz and sqrt, and the conversions.
func isUnary(short string) bool {
	switch short {
	case "clz", "ctz", "popcnt", "abs", "sqrt", "ceil", "floor", "trunc", "nearest",
		"extend8_s", "extend16_s", "extend32_s", "demote_f64", "promote_f32":
		return true
	}
	for _, prefix := range []string{"wrap_", "extend_", "trunc_", "convert_", "reinterpret_"} {
		if strings.HasPrefix(short, prefix) {
			return true
		}
	}
	return false
}

// convert spells a one value instruction as a function: a conversion as
// the type it gives, u for unsigned, and anything else by its name.
func convert(typ, short string) string {
	switch {
	case strings.HasPrefix(short, "reinterpret_"), strings.HasPrefix(short, "extend") && !strings.HasPrefix(short, "extend_"):
		return short
	case strings.HasPrefix(short, "wrap_"), strings.HasPrefix(short, "extend_"),
		strings.HasPrefix(short, "trunc_"), strings.HasPrefix(short, "convert_"),
		short == "demote_f64", short == "promote_f32":
		if strings.HasSuffix(short, "_u") {
			if typ[0] == 'i' {
				return "u" + typ[1:]
			}
			return typ + "u"
		}
		return typ
	}
	return short
}

// memType is the type of memory a load or store reads or writes:
// i32[a] for i32.load, u8[a] for i32.load8_u, i16[a] for i64.store16.
func memType(typ, short string) string {
	bits := strings.TrimLeft(short, "loadstore")
	if bits == "" {
		return typ
	}
	size, sign, _ := strings.Cut(bits, "_")
	if sign == "u" {
		return "u" + size
	}
	return "i" + size
}

// address spells a memory address, base plus the instruction's offset.
func address(base expr, offset uint64) string {
	switch {
	case offset == 0:
		return base.text
	case base.text == "0":
		return strconv.FormatUint(offset, 10)
	}
	return base.text + " + " + strconv.FormatUint(offset, 10)
}

// wasmFuncTypes is the type of each function in the module's index
// space, the imported ones first.
func wasmFuncTypes(m *wasmir.Module) []wasmir.TypeDef {
	var types []wasmir.TypeDef
	for _, imp := range m.Imports {
		if imp.Kind == wasmir.ExternalKindFunction {
			types = append(types, typeOf(m, imp.TypeIdx))
		}
	}
	for _, f := range m.Funcs {
		types = append(types, typeOf(m, f.TypeIdx))
	}
	return types
}

// wasmGlobalNames is the name of each global in the module's index
// space, empty for one without.
func wasmGlobalNames(m *wasmir.Module) []string {
	var names []string
	for _, imp := range m.Imports {
		if imp.Kind == wasmir.ExternalKindGlobal {
			names = append(names, imp.Name)
		}
	}
	for _, g := range m.Globals {
		names = append(names, strings.TrimPrefix(g.Name, "$"))
	}
	return names
}

func typeOf(m *wasmir.Module, i uint32) wasmir.TypeDef {
	if int(i) < len(m.Types) {
		return m.Types[i]
	}
	return wasmir.TypeDef{}
}

// blockResults is how many values a block leaves.
func blockResults(m *wasmir.Module, in *wasmir.Instruction) int {
	switch {
	case in.BlockTypeUsesIndex:
		return len(typeOf(m, in.BlockTypeIndex).Results)
	case in.BlockType != nil:
		return 1
	}
	return 0
}

// wasmTargets marks the blocks that a branch goes to, by their row.
func wasmTargets(body []wasmir.Instruction) map[int]bool {
	targeted := map[int]bool{}
	var open []int
	mark := func(depth uint32) {
		if int(depth) < len(open) {
			targeted[open[len(open)-1-int(depth)]] = true
		}
	}
	for i, in := range body {
		switch in.Kind {
		case wasmir.InstrBlock, wasmir.InstrLoop, wasmir.InstrIf, wasmir.InstrTryTable:
			open = append(open, i)
		case wasmir.InstrEnd:
			if len(open) > 0 {
				open = open[:len(open)-1]
			}
		case wasmir.InstrBr, wasmir.InstrBrIf:
			mark(in.BranchDepth)
		case wasmir.InstrBrTable:
			for _, d := range in.BranchTable {
				mark(d)
			}
			mark(in.BranchDefault)
		}
	}
	return targeted
}

// goWasmGlobals are the globals the Go linker gives every wasm module,
// in order, without naming them in the module.
var goWasmGlobals = []string{"SP", "CTXT", "g", "RET0", "RET1", "RET2", "RET3", "PAUSE"}

// isGoWasm reports whether the Go toolchain built the module, which
// marks it with its build ID.
func isGoWasm(m *wasmir.Module) bool {
	for _, c := range m.CustomSections {
		if c.Name == "go:buildid" {
			return true
		}
	}
	return false
}
