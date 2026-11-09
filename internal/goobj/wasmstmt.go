package goobj

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/eliben/watgo/wasmir"

	"loov.dev/lensm/internal/disasm"
)

// expr is an expression on the wasm value stack and how tightly it
// binds, as in Go: 5 for * / % << >> &, 4 for + - | ^, 3 for
// comparisons, 1 for the conditional and 9 for anything that needs no
// parentheses.
type expr struct {
	text string
	prec int
	// not is the comparison's negation, for eqz to use.
	not string
	// bits marks a shift or a bitwise operator, which Go and C bind
	// differently against arithmetic and comparisons, so it gets
	// parentheses inside those either way.
	bits bool
	// narrow is the 32-bit value an extension widened, which wrapping
	// gives back.
	narrow string
	// konst is set for a constant, val.
	konst bool
	val   int64
	// sp marks an address on Go's stack, spOff bytes from where SP was
	// when the function started; spGlobal marks the SP global itself.
	sp       bool
	spOff    int64
	spGlobal bool
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

// wasmModule reads the module once, on first use. It is nil for a
// component or a module that can't be read, and the statements are
// then left out.
func (file *File) wasmModule() *wasmInfo {
	if !file.wasmTried {
		file.wasmTried = true
		if data, err := os.ReadFile(file.path); err == nil {
			file.wasm = loadWasmInfo(data)
		}
	}
	return file.wasm
}

// wasmStatements spells the instructions of the function at index idx
// in the module, the imports counted, as statements: each instruction
// that completes one gets its text, such as "l2 = l2 + i32[p0 + l1]",
// and the instructions feeding it get none. insts are the function's
// rows, in the order of its body.
//
// goArgs names a Go function's arguments and results by their offset
// from the first, for the loads and stores that reach them.
func wasmStatements(info *wasmInfo, idx int, insts []disasm.Inst, goArgs map[int64]string) {
	m, funcs, imported := info.m, info.funcTypes, info.imported
	if idx < imported || idx >= len(funcs) {
		return
	}
	defer func() {
		// A body it can't follow keeps its listing, without statements.
		if recover() != nil {
			for i := range insts {
				insts[i].NativeText, insts[i].Stack = "", nil
			}
		}
	}()
	fn := &m.Funcs[idx-imported]
	body := info.body(idx - imported)
	if len(body) > 0 && body[len(body)-1].Kind == wasmir.InstrEnd {
		body = body[:len(body)-1]
	}
	if len(body) != len(insts) {
		return
	}
	locals := info.locals[idx-imported]
	ftype := typeOf(m, fn.TypeIdx)
	params := len(ftype.Params)
	goWasm := info.goWasm
	name := func(i uint32) string {
		if int(i) < params {
			if int(i) < len(fn.ParamNames) && fn.ParamNames[i] != "" {
				return strings.TrimPrefix(fn.ParamNames[i], "$")
			}
			if n := locals[i]; n != "" {
				return n
			}
			if i == 0 && goWasm {
				// The resume point the function continues from.
				return "PC_B"
			}
			return fmt.Sprint("p", i)
		}
		if k := int(i) - params; k < len(fn.LocalNames) && fn.LocalNames[k] != "" {
			return strings.TrimPrefix(fn.LocalNames[k], "$")
		}
		if n := locals[i]; n != "" {
			return n
		}
		return fmt.Sprint("l", i)
	}
	globals := info.globals
	global := func(i uint32) string {
		if int(i) < len(globals) && globals[i] != "" {
			return globals[i]
		}
		return fmt.Sprint("g", i)
	}

	// block is an open block, loop or if.
	type block struct {
		label string
		// cases is set for a block that a switch's case goes to: its end
		// starts that case.
		cases string
		// selector is the switch right inside a loop, nil when there is
		// none.
		selector *wasmSelector
		loop     bool
		// cond marks an if, and adj is where l1 pointed when it started.
		cond     bool
		adj      int64
		adjKnown bool
		height   int // the stack's height when the block started
		result   int // the values the block leaves
	}
	// The blocks that branches go to get labels, in order.
	targeted := wasmTargets(body)
	cases, switches, selectors := wasmSwitches(m, body, insts)
	labels := 0

	// Go's wasm port keeps SP in local 1. adj is where it points, from
	// SP at the function's start, while adjKnown; frameAdj is where it
	// points once the function has made its frame.
	var adj, frameAdj int64
	adjKnown, frameKnown := false, false
	var frames []block
	inIf := func() bool {
		return slices.ContainsFunc(frames[1:], func(b block) bool { return b.cond })
	}
	// moveSP follows a store to local 1.
	moveSP := func(local uint32, v expr) {
		switch {
		case goArgs == nil || local != 1:
		case v.sp:
			adj, adjKnown = v.spOff, true
		case v.spGlobal && frameKnown:
			// After a call SP is back at the frame.
			adj, adjKnown = frameAdj, true
		case v.spGlobal:
			// The function's start, before it makes its frame.
			adj, adjKnown = 0, true
		default:
			adjKnown = false
		}
	}
	// argName is the argument or result a Go stack address reaches.
	argName := func(base expr, offset uint64) (string, bool) {
		if !base.sp {
			return "", false
		}
		// The arguments start past the return address.
		n, ok := goArgs[base.spOff+int64(offset)-8]
		return n, ok
	}

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
	readsCall := func(r reads) bool { return r.call }
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
		if e.prec < prec || e.bits && prec >= 3 && prec <= 5 {
			return "(" + e.text + ")"
		}
		return e.text
	}
	frames = []block{{label: "", result: len(ftype.Results)}}
	// cut drops the values of the current block: the code after a
	// branch or trap doesn't run until the block ends.
	cut := func() {
		stack = stack[:min(len(stack), frames[len(frames)-1].height)]
	}
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
		for k := len(stack) - 1; k >= max(len(stack)-maxStack, 0); k-- {
			t := stack[k].text
			if len(t) > maxExpr {
				t = t[:maxExpr] + "…"
			}
			insts[i].Stack = append(insts[i].Stack, t)
		}
		op, arg, _ := strings.Cut(insts[i].Text, " ")
		typ, short, _ := strings.Cut(op, ".")
		text := ""
		switch {
		case op == "block" || op == "loop" || op == "if" || op == "try_table":
			b := block{loop: op == "loop", result: blockResults(m, in), cases: cases[i], selector: selectors[i], cond: op == "if", adj: adj, adjKnown: adjKnown}
			if b.cases == "" && (targeted[i] || b.result > 0) {
				labels++
				b.label = fmt.Sprint("L", labels)
			}
			cond := ""
			if op == "if" {
				cond = pop().text
			}
			// A call waiting on the stack ran before the block.
			settle(readsCall)
			b.height = len(stack)
			frames = append(frames, b)
			switch {
			case b.cases != "":
				// The switch spells it.
			case op == "if":
				text = strings.TrimSpace("if "+b.label) + " (" + cond + ") {"
			case op == "try_table":
				text = strings.TrimSpace("try "+b.label) + " {"
			default:
				text = strings.TrimSpace(op+" "+b.label) + " {"
			}
		case op == "else":
			b := &frames[len(frames)-1]
			text = leftover(b) + "} else {"
			adj, adjKnown = b.adj, b.adjKnown
		case op == "end":
			if len(frames) == 1 {
				break
			}
			b := frames[len(frames)-1]
			text = leftover(&b) + "}"
			if b.cases != "" {
				text = b.cases + ":"
				// Go enters case 0 at the function's start and any other
				// to resume, with its frame made.
				if b.cases == "case 0" || strings.HasPrefix(b.cases, "case 0,") {
					adj, adjKnown = 0, true
				} else {
					adj, adjKnown = frameAdj, frameKnown
				}
			}
			if b.cond {
				// Go only moves SP inside an if on its way out.
				adj, adjKnown = b.adj, b.adjKnown
			}
			frames = frames[:len(frames)-1]
			if b.result > 0 {
				push(b.label, 9)
			}
		case op == "br" || op == "br_if" || op == "br_table":
			cond := ""
			if op != "br" {
				cond = pop().text
			}
			settle(readsCall)
			target := func(depth uint32) string {
				if int(depth) >= len(frames) {
					return "?"
				}
				b := frames[len(frames)-1-int(depth)]
				switch {
				case int(depth) == len(frames)-1:
					if len(ftype.Results) > 0 && len(stack) > 0 {
						return "return " + stack[len(stack)-1].text
					}
					return "return"
				case b.cases != "":
					return "goto " + b.cases
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
			// Setting a switch's local to a constant and going back to its
			// loop goes to a case. Go jumps that way inside a function and
			// never reads PC_B after, so there the write goes unsaid.
			if op == "br" && i >= 2 && int(in.BranchDepth) < len(frames) {
				b := frames[len(frames)-1-int(in.BranchDepth)]
				set := &body[i-1]
				value, err := strconv.Atoi(strings.TrimPrefix(insts[i-2].Text, "i32.const "))
				if b.loop && b.selector != nil && set.Kind == wasmir.InstrLocalSet && set.LocalIndex == b.selector.local && err == nil && strings.HasPrefix(insts[i-2].Text, "i32.const ") {
					if goWasm && !strings.Contains(insts[i-1].NativeText, ";") {
						insts[i-1].NativeText = ""
					}
					text = "goto case " + strconv.Itoa(value)
					if value < 0 || value >= b.selector.cases {
						text = "goto default"
					}
					cut()
					break
				}
			}
			switch {
			case op == "br":
				text = carry(in.BranchDepth) + target(in.BranchDepth)
			case op == "br_if":
				if c := carry(in.BranchDepth); c != "" {
					text = "if (" + cond + ") { " + c + target(in.BranchDepth) + " }"
				} else {
					text = "if (" + cond + ") " + target(in.BranchDepth)
				}
			case switches[i]:
				text = "switch (" + cond + ")"
			default:
				var carried []string
				for _, d := range append(slices.Clone(in.BranchTable), in.BranchDefault) {
					if c := carry(d); c != "" && !slices.Contains(carried, c) {
						carried = append(carried, c)
					}
				}
				var cases []string
				for _, d := range in.BranchTable {
					cases = append(cases, strings.TrimPrefix(strings.TrimPrefix(target(d), "break "), "continue "))
				}
				def := strings.TrimPrefix(strings.TrimPrefix(target(in.BranchDefault), "break "), "continue ")
				text = strings.Join(carried, "") + "switch (" + cond + ") " + strings.Join(cases, ", ") + " else " + def
			}
			if op != "br_if" {
				// What follows is unreachable until the block ends.
				cut()
			}
		case op == "return":
			text = strings.TrimSpace("return " + strings.Join(popN(len(ftype.Results)), ", "))
		case op == "unreachable":
			text = "unreachable"
			cut()
		case op == "nop":
		case op == "drop":
			// Only a call or memory.grow does something worth showing.
			if v := pop(); v.reads.call && v.row >= 0 {
				text = v.text
			}
		case op == "select":
			c, b, a := pop(), pop(), pop()
			push(wrap(c, 3)+" ? "+wrap(a, 2)+" : "+wrap(b, 2), 1)
		case op == "local.get":
			own.locals = []uint32{in.LocalIndex}
			pushExpr(expr{text: name(in.LocalIndex), prec: 9, sp: goArgs != nil && in.LocalIndex == 1 && adjKnown, spOff: adj})
		case op == "local.set":
			v := pop()
			settle(readsLocal(in.LocalIndex))
			moveSP(in.LocalIndex, v)
			text = name(in.LocalIndex) + " = " + v.text
		case op == "local.tee":
			v := pop()
			settle(readsLocal(in.LocalIndex))
			moveSP(in.LocalIndex, v)
			text = name(in.LocalIndex) + " = " + v.text
			popped, own = reads{}, reads{locals: []uint32{in.LocalIndex}}
			pushExpr(expr{text: name(in.LocalIndex), prec: 9, sp: v.sp, spOff: v.spOff, spGlobal: v.spGlobal})
		case op == "global.get":
			settle(readsCall)
			own.globals = []uint32{in.GlobalIndex}
			pushExpr(expr{text: global(in.GlobalIndex), prec: 9, spGlobal: goArgs != nil && in.GlobalIndex == 0})
		case op == "global.set":
			v := pop()
			if goArgs != nil && in.GlobalIndex == 0 && v.sp && !frameKnown && !inIf() {
				// The function's first SP outside the stack check makes
				// its frame.
				frameAdj, frameKnown = v.spOff, true
			}
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
			v, err := strconv.ParseInt(arg, 0, 64)
			pushExpr(expr{text: arg, prec: 9, konst: err == nil, val: v})
		case strings.HasPrefix(short, "load"):
			settle(readsCall)
			own.memory = true
			base := pop()
			if n, ok := argName(base, in.MemoryOffset); ok {
				push(n, 9)
				break
			}
			push(memType(typ, short)+"["+address(base, in.MemoryOffset)+"]", 9)
		case strings.HasPrefix(short, "store"):
			v := pop()
			base := pop()
			a := memType(typ, short) + "[" + address(base, in.MemoryOffset) + "]"
			if n, ok := argName(base, in.MemoryOffset); ok {
				a = n
			}
			settle(readsMemory)
			text = a + " = " + v.text
		case op == "memory.size":
			settle(readsCall)
			own.memory = true
			push("memory_size()", 9)
		case op == "memory.grow":
			n := pop()
			settle(readsMemory)
			// It changes memory as a call might, and drop shows it.
			own.memory, own.call = true, true
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
			e := expr{text: wrap(a, o.prec) + " " + o.text + " " + wrap(b, o.prec+1), prec: o.prec, bits: strings.HasPrefix(short, "sh") || short == "and" || short == "or" || short == "xor"}
			// An address on Go's stack stays one past an offset.
			switch {
			case short == "add" && a.sp && b.konst:
				e.sp, e.spOff = true, a.spOff+b.val
			case short == "add" && b.sp && a.konst:
				e.sp, e.spOff = true, b.spOff+a.val
			case short == "sub" && a.sp && b.konst:
				e.sp, e.spOff = true, a.spOff-b.val
			}
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
			pushExpr(expr{text: wrap(a, 9) + " == 0", prec: 3, not: wrap(a, 9) + " != 0"})
		case short == "neg":
			push("-"+wrap(pop(), 9), 9)
		case isUnary(short):
			a := pop()
			if a.not != "" && strings.HasPrefix(short, "extend_i32") {
				// A comparison is 0 or 1 at any width.
				pushExpr(expr{text: a.text, prec: a.prec, not: a.not})
				break
			}
			if short == "wrap_i64" && a.narrow != "" {
				pushExpr(expr{text: a.narrow, prec: 9, sp: a.sp, spOff: a.spOff})
				break
			}
			e := expr{text: convert(typ, short) + "(" + a.text + ")", prec: 9}
			if short == "wrap_i64" || strings.HasPrefix(short, "extend_i32") {
				e.sp, e.spOff = a.sp, a.spOff
			}
			if strings.HasPrefix(short, "extend_i32") {
				e.narrow = wrap(a, 9)
			}
			pushExpr(e)
		default:
			// An instruction this doesn't know the operands of: show it
			// and start the block over with an unknown stack.
			text = insts[i].Text
			cut()
		}
		// A value grown too long to read gets a name on its own row.
		if n := len(stack); n > 0 && stack[n-1].row == i && len(stack[n-1].text) > maxExpr {
			temps++
			t := fmt.Sprint("t", temps)
			text = strings.TrimSpace(text + " " + t + " = " + stack[n-1].text)
			stack[n-1] = expr{text: t, prec: 9, row: -1}
		}
		insts[i].NativeText = text
	}
	// The values left at the end are the function's results.
	if last := len(insts) - 1; last >= 0 && len(ftype.Results) > 0 && len(stack) > 0 {
		ret := "return " + strings.Join(popN(min(len(ftype.Results), len(stack))), ", ")
		insts[last].NativeText = strings.TrimSpace(insts[last].NativeText + " " + ret)
	}
}

const (
	// maxStack is the most values a row keeps of the stack before it.
	maxStack = 16
	// maxExpr is the longest a value gets before it is named.
	maxExpr = 200
)

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
	text := base.text
	if base.prec < 4 || base.bits {
		text = "(" + text + ")"
	}
	return text + " + " + strconv.FormatUint(offset, 10)
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

// wasmSelector is a switch on a local: the value of local picks one
// of its cases, or the default from cases on.
type wasmSelector struct {
	local uint32
	cases int
}

// wasmSwitches finds the br_tables that pick a case: blocks opened one
// right after another, then the value to switch on, then a br_table
// whose every target is one of those blocks. Each such block's end
// starts a case, which cases spells by its row, as in "case 2, 3",
// and switches marks the br_tables.
//
// When the switch is on a local, right inside a loop, selectors holds
// that local plus one by the loop's row.
func wasmSwitches(m *wasmir.Module, body []wasmir.Instruction, insts []disasm.Inst) (cases map[int]string, switches map[int]bool, selectors map[int]*wasmSelector) {
	cases, switches, selectors = map[int]string{}, map[int]bool{}, map[int]*wasmSelector{}
	for j, in := range body {
		if in.Kind != wasmir.InstrBrTable {
			continue
		}
		// Between the blocks and the br_table only the value is
		// computed, with nothing a statement would show.
		k := j - 1
		for k >= 0 && !isOpener(body[k].Kind) && pure(insts[k].Text) {
			k--
		}
		if k < 0 || !isOpener(body[k].Kind) {
			continue
		}
		// run holds the blocks opened one after another, innermost first.
		var run []int
		for ; k >= 0 && isOpener(body[k].Kind); k-- {
			run = append(run, k)
		}
		values := map[uint32][]string{}
		deepest := in.BranchDefault
		for v, d := range in.BranchTable {
			values[d] = append(values[d], strconv.Itoa(v))
			deepest = max(deepest, d)
		}
		ok := int(deepest) < len(run)
		for d := uint32(0); ok && d <= deepest; d++ {
			b := &body[run[d]]
			ok = b.Kind == wasmir.InstrBlock && blockResults(m, b) == 0 &&
				(len(values[d]) > 0 || d == in.BranchDefault)
		}
		if !ok {
			continue
		}
		switches[j] = true
		if int(deepest)+1 < len(run) && body[run[deepest+1]].Kind == wasmir.InstrLoop && j-1 == run[0]+1 && body[j-1].Kind == wasmir.InstrLocalGet {
			selectors[run[deepest+1]] = &wasmSelector{local: body[j-1].LocalIndex, cases: len(in.BranchTable)}
		}
		for d := uint32(0); d <= deepest; d++ {
			text := "default"
			if len(values[d]) > 0 {
				text = "case " + strings.Join(values[d], ", ")
				if d == in.BranchDefault {
					text += ", default"
				}
			}
			cases[run[d]] = text
		}
	}
	return cases, switches, selectors
}

func isOpener(k wasmir.InstrKind) bool {
	switch k {
	case wasmir.InstrBlock, wasmir.InstrLoop, wasmir.InstrIf, wasmir.InstrTryTable:
		return true
	}
	return false
}

// pure reports whether an instruction only computes a value from
// others, so it shows no statement of its own.
func pure(text string) bool {
	op, _, _ := strings.Cut(text, " ")
	typ, short, _ := strings.Cut(op, ".")
	switch typ {
	case "local", "global":
		return short == "get"
	case "i32", "i64", "f32", "f64":
		return !strings.HasPrefix(short, "store") && !strings.HasPrefix(short, "atomic")
	}
	return false
}
