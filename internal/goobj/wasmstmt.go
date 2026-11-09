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

// How tightly an expression binds, as in Go.
const (
	precCond = 1 // c ? a : b
	precCmp  = 3 // == != < <= > >=
	precAdd  = 4 // + - | ^
	precMul  = 5 // * / % << >> &
	precAtom = 9 // anything that needs no parentheses
)

// expr is an expression on the wasm value stack.
type expr struct {
	text string
	prec int
	// not is the comparison's negation, for eqz to use.
	not string
	// bitwise marks a shift or a bitwise operator, which Go and C bind
	// differently against arithmetic and comparisons, so it gets
	// parentheses inside those either way.
	bitwise bool
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
	// row is the instruction that completed it, -1 for a temporary,
	// and reads what its value depends on, so a statement that changes
	// that first can give the value a name on its own row.
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

// What a statement about to run changes, for settle.
func readsLocal(n uint32) func(reads) bool {
	return func(r reads) bool { return slices.Contains(r.locals, n) }
}

func readsGlobal(n uint32) func(reads) bool {
	return func(r reads) bool { return r.call || slices.Contains(r.globals, n) }
}

func readsCall(r reads) bool   { return r.call }
func readsMemory(r reads) bool { return r.memory || r.call }
func readsAny(r reads) bool    { return r.memory || r.call || len(r.globals) > 0 }

// wrap puts e in parentheses when it binds looser than prec.
func wrap(e expr, prec int) string {
	if e.prec < prec || e.bitwise && prec >= precCmp && prec <= precMul {
		return "(" + e.text + ")"
	}
	return e.text
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
// rows, in the order of its body, and targeted marks the blocks that
// branches go to, by their row.
//
// goArgs names a Go function's arguments and results by their offset
// from the first, for the loads and stores that reach them.
func wasmStatements(info *wasmInfo, idx int, insts []disasm.Inst, targeted map[int]bool, goArgs map[int64]string) {
	k := idx - info.imported
	if k < 0 || k >= len(info.m.Funcs) {
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
	body := info.body(k)
	if len(body) > 0 && body[len(body)-1].Kind == wasmir.InstrEnd {
		body = body[:len(body)-1]
	}
	if len(body) != len(insts) {
		return
	}
	fn := &info.m.Funcs[k]
	w := &stmtWriter{
		info: info, fn: fn, ftype: typeOf(info.m, fn.TypeIdx),
		body: body, insts: insts, locals: info.locals[k],
		targeted: targeted,
		sp:       goSP{args: goArgs},
	}
	w.cases, w.switches, w.selectors = wasmSwitches(info.m, body, insts)
	w.frames = []wasmBlock{{results: len(w.ftype.Results)}}
	for i := range body {
		w.row, w.operandReads, w.selfReads = i, reads{}, reads{}
		w.keepStack()
		text := w.step(i)
		// A value grown too long to read gets a name on its own row.
		if n := len(w.stack); n > 0 && w.stack[n-1].row == i && len(w.stack[n-1].text) > maxExpr {
			t := w.temp()
			text = strings.TrimSpace(text + " " + t + " = " + w.stack[n-1].text)
			w.stack[n-1] = expr{text: t, prec: precAtom, row: -1}
		}
		insts[i].NativeText = text
	}
	// The values left at the end are the function's results.
	if last := len(insts) - 1; last >= 0 && len(w.ftype.Results) > 0 && len(w.stack) > 0 {
		ret := "return " + strings.Join(w.popN(min(len(w.ftype.Results), len(w.stack))), ", ")
		insts[last].NativeText = strings.TrimSpace(insts[last].NativeText + " " + ret)
	}
}

// stmtWriter follows a function's value stack and blocks, spelling
// each instruction that completes a statement.
type stmtWriter struct {
	info   *wasmInfo
	fn     *wasmir.Function
	ftype  wasmir.TypeDef
	body   []wasmir.Instruction
	insts  []disasm.Inst
	locals map[uint32]string // names from DWARF

	targeted  map[int]bool
	cases     map[int]string
	switches  map[int]bool
	selectors map[int]*wasmSelector

	stack  []expr
	frames []wasmBlock
	sp     goSP

	// row is the instruction being spelled. operandReads gathers what
	// its operands read and selfReads what it reads itself, which
	// pushExpr gives the value it pushes.
	row                     int
	operandReads, selfReads reads

	temps, labels int
}

// wasmBlock is an open block, loop or if; the function body is the
// outermost.
type wasmBlock struct {
	label string
	// cases is set for a block that a switch's case goes to: its end
	// starts that case.
	cases string
	// selector is the switch right inside a loop, nil when there is
	// none.
	selector *wasmSelector
	loop     bool
	// cond marks an if, and sp is where Go's SP pointed when it started.
	cond bool
	sp   goSPState
	// height is the stack's height under the block's values, and
	// results how many it leaves. params are the values an if takes,
	// which its else branch starts from again.
	height, results int
	params          []expr
}

// keepStack records the top of the stack before the row runs, for the
// view to show.
func (w *stmtWriter) keepStack() {
	for k := len(w.stack) - 1; k >= max(len(w.stack)-maxStack, 0); k-- {
		t := w.stack[k].text
		if len(t) > maxExpr {
			t = t[:maxExpr] + "…"
		}
		w.insts[w.row].Stack = append(w.insts[w.row].Stack, t)
	}
}

func (w *stmtWriter) localName(i uint32) string {
	params := uint32(len(w.ftype.Params))
	if i < params {
		if int(i) < len(w.fn.ParamNames) && w.fn.ParamNames[i] != "" {
			return strings.TrimPrefix(w.fn.ParamNames[i], "$")
		}
		if n := w.locals[i]; n != "" {
			return n
		}
		if i == 0 && w.info.goWasm {
			// The resume point the function continues from.
			return "PC_B"
		}
		return fmt.Sprint("p", i)
	}
	if k := int(i - params); k < len(w.fn.LocalNames) && w.fn.LocalNames[k] != "" {
		return strings.TrimPrefix(w.fn.LocalNames[k], "$")
	}
	if n := w.locals[i]; n != "" {
		return n
	}
	return fmt.Sprint("l", i)
}

func (w *stmtWriter) globalName(i uint32) string {
	if int(i) < len(w.info.globals) && w.info.globals[i] != "" {
		return w.info.globals[i]
	}
	return fmt.Sprint("g", i)
}

func (w *stmtWriter) pop() expr {
	if len(w.stack) == 0 {
		return expr{text: "?", prec: precAtom}
	}
	e := w.stack[len(w.stack)-1]
	w.stack = w.stack[:len(w.stack)-1]
	w.operandReads.add(e.reads)
	return e
}

// popN pops n values, in the order they were pushed.
func (w *stmtWriter) popN(n int) []string {
	args := make([]string, n)
	for i := n - 1; i >= 0; i-- {
		args[i] = w.pop().text
	}
	return args
}

// pushExpr pushes the row's value, which reads what the row's
// operands and the row itself read.
func (w *stmtWriter) pushExpr(e expr) {
	e.row, e.reads = w.row, w.operandReads
	e.reads.add(w.selfReads)
	w.stack = append(w.stack, e)
}

func (w *stmtWriter) push(text string, prec int) { w.pushExpr(expr{text: text, prec: prec}) }

func (w *stmtWriter) temp() string {
	w.temps++
	return fmt.Sprint("t", w.temps)
}

// settle names the values waiting on the stack that what is about to
// happen would change, on the rows that computed them, so the
// statements keep their order.
func (w *stmtWriter) settle(changes func(r reads) bool) {
	for k := range w.stack {
		e := &w.stack[k]
		if !changes(e.reads) || e.row < 0 {
			continue
		}
		t := w.temp()
		assign := t + " = " + e.text
		if w.insts[e.row].NativeText != "" {
			assign = w.insts[e.row].NativeText + "; " + assign
		}
		w.insts[e.row].NativeText = assign
		*e = expr{text: t, prec: precAtom, row: -1}
	}
}

func (w *stmtWriter) top() *wasmBlock { return &w.frames[len(w.frames)-1] }

// cut drops the values of the current block: the code after a branch
// or trap doesn't run until the block ends.
func (w *stmtWriter) cut() {
	w.stack = w.stack[:min(len(w.stack), w.top().height)]
}

// resultNames are the names a block's values get after it ends: its
// label, or with several, the label and an index.
func resultNames(label string, n int) []string {
	if n == 1 {
		return []string{label}
	}
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("%s.%d", label, i)
	}
	return names
}

// leftover spells the values a block leaves as an assignment to its
// label, so they show before the block's end.
func (w *stmtWriter) leftover(b *wasmBlock) string {
	defer func() { w.stack = w.stack[:min(len(w.stack), b.height)] }()
	if len(w.stack) < b.height+b.results || b.results == 0 {
		return ""
	}
	values := w.popN(b.results)
	return strings.Join(resultNames(b.label, b.results), ", ") + " = " + strings.Join(values, ", ") + "; "
}

func (w *stmtWriter) inIf() bool {
	return slices.ContainsFunc(w.frames[1:], func(b wasmBlock) bool { return b.cond })
}

// target spells where a branch depth blocks out goes.
func (w *stmtWriter) target(depth uint32) string {
	if int(depth) >= len(w.frames) {
		return "?"
	}
	b := w.frames[len(w.frames)-1-int(depth)]
	switch {
	case int(depth) == len(w.frames)-1:
		if n := min(len(w.ftype.Results), len(w.stack)); n > 0 {
			return "return " + strings.Join(w.peek(n), ", ")
		}
		return "return"
	case b.cases != "":
		return "goto " + b.cases
	case b.loop:
		return "continue " + b.label
	}
	return "break " + b.label
}

// peek spells the top n values, in the order they were pushed.
func (w *stmtWriter) peek(n int) []string {
	var texts []string
	for _, e := range w.stack[len(w.stack)-n:] {
		texts = append(texts, e.text)
	}
	return texts
}

// carry spells the values a branch to a block that gives them takes:
// the top of the stack, which br_if leaves there when not taken.
func (w *stmtWriter) carry(depth uint32) string {
	if int(depth) >= len(w.frames)-1 {
		return ""
	}
	b := w.frames[len(w.frames)-1-int(depth)]
	if b.loop || b.results == 0 || len(w.stack) < b.results {
		return ""
	}
	return strings.Join(resultNames(b.label, b.results), ", ") + " = " + strings.Join(w.peek(b.results), ", ") + "; "
}

// step spells row i, empty when it completes no statement.
func (w *stmtWriter) step(i int) string {
	in := &w.body[i]
	op, arg, _ := strings.Cut(w.insts[i].Text, " ")
	typ, short, _ := strings.Cut(op, ".")
	switch {
	case isWasmOpener(op):
		return w.open(i, op, in)
	case op == "else":
		b := w.top()
		text := w.leftover(b) + "} else {"
		w.sp.state = b.sp
		// The else branch starts from the if's parameters again.
		w.stack = append(w.stack, b.params...)
		return text
	case op == "end":
		return w.end()
	case op == "br" || op == "br_if" || op == "br_table":
		return w.branch(i, op, in)
	case op == "return":
		return strings.TrimSpace("return " + strings.Join(w.popN(len(w.ftype.Results)), ", "))
	case op == "unreachable":
		w.cut()
		return "unreachable"
	case op == "nop":
		return ""
	case op == "drop":
		// Only a call or memory.grow does something worth showing.
		if v := w.pop(); v.reads.call && v.row >= 0 {
			return v.text
		}
		return ""
	case op == "select":
		c, b, a := w.pop(), w.pop(), w.pop()
		w.push(wrap(c, precCmp)+" ? "+wrap(a, precCond+1)+" : "+wrap(b, precCond+1), precCond)
		return ""
	case op == "local.get":
		w.selfReads.locals = []uint32{in.LocalIndex}
		sp, off := w.sp.read(in.LocalIndex)
		w.pushExpr(expr{text: w.localName(in.LocalIndex), prec: precAtom, sp: sp, spOff: off})
		return ""
	case op == "local.set" || op == "local.tee":
		v := w.pop()
		w.settle(readsLocal(in.LocalIndex))
		w.sp.set(in.LocalIndex, v)
		text := w.localName(in.LocalIndex) + " = " + v.text
		if op == "local.tee" {
			w.operandReads, w.selfReads = reads{}, reads{locals: []uint32{in.LocalIndex}}
			w.pushExpr(expr{text: w.localName(in.LocalIndex), prec: precAtom, sp: v.sp, spOff: v.spOff, spGlobal: v.spGlobal})
		}
		return text
	case op == "global.get":
		w.settle(readsCall)
		w.selfReads.globals = []uint32{in.GlobalIndex}
		w.pushExpr(expr{text: w.globalName(in.GlobalIndex), prec: precAtom, spGlobal: w.sp.isGlobal(in.GlobalIndex)})
		return ""
	case op == "global.set":
		v := w.pop()
		w.sp.setGlobal(in.GlobalIndex, v, w.inIf())
		w.settle(readsGlobal(in.GlobalIndex))
		return w.globalName(in.GlobalIndex) + " = " + v.text
	case op == "call" || op == "call_indirect" || op == "return_call":
		return w.call(op, arg, in)
	}
	return w.value(op, arg, typ, short, in)
}

// open starts a block, loop or if.
func (w *stmtWriter) open(i int, op string, in *wasmir.Instruction) string {
	params, results := blockArity(w.info.m, in)
	b := wasmBlock{
		loop: op == "loop", cond: op == "if", results: results,
		cases: w.cases[i], selector: w.selectors[i], sp: w.sp.state,
	}
	if b.cases == "" && (w.targeted[i] || b.results > 0) {
		w.labels++
		b.label = fmt.Sprint("L", w.labels)
	}
	cond := ""
	if op == "if" {
		cond = w.pop().text
	}
	// A call waiting on the stack ran before the block.
	w.settle(readsCall)
	// The block's parameters are its own values.
	b.height = max(len(w.stack)-params, 0)
	if b.cond {
		b.params = slices.Clone(w.stack[b.height:])
	}
	w.frames = append(w.frames, b)
	switch {
	case b.cases != "":
		// The switch spells it.
		return ""
	case op == "if":
		return strings.TrimSpace("if "+b.label) + " (" + cond + ") {"
	case op == "try_table":
		return strings.TrimSpace("try "+b.label) + " {"
	}
	return strings.TrimSpace(op+" "+b.label) + " {"
}

// end closes the innermost block.
func (w *stmtWriter) end() string {
	if len(w.frames) == 1 {
		return ""
	}
	b := *w.top()
	text := w.leftover(&b) + "}"
	if b.cases != "" {
		text = b.cases + ":"
		w.sp.caseStart(b.cases)
	}
	if b.cond {
		// Go only moves SP inside an if on its way out.
		w.sp.state = b.sp
	}
	w.frames = w.frames[:len(w.frames)-1]
	if b.results > 0 {
		for _, n := range resultNames(b.label, b.results) {
			w.push(n, precAtom)
		}
	}
	return text
}

// branch spells br, br_if and br_table.
func (w *stmtWriter) branch(i int, op string, in *wasmir.Instruction) string {
	cond := ""
	if op != "br" {
		cond = w.pop().text
	}
	w.settle(readsCall)
	if op != "br_if" {
		// What follows is unreachable until the block ends.
		defer w.cut()
	}
	if text, ok := w.gotoCase(i, op, in); ok {
		return text
	}
	switch {
	case op == "br":
		return w.carry(in.BranchDepth) + w.target(in.BranchDepth)
	case op == "br_if":
		if c := w.carry(in.BranchDepth); c != "" {
			return "if (" + cond + ") { " + c + w.target(in.BranchDepth) + " }"
		}
		return "if (" + cond + ") " + w.target(in.BranchDepth)
	case w.switches[i]:
		return "switch (" + cond + ")"
	}
	var carried []string
	for _, d := range append(slices.Clone(in.BranchTable), in.BranchDefault) {
		if c := w.carry(d); c != "" && !slices.Contains(carried, c) {
			carried = append(carried, c)
		}
	}
	short := func(d uint32) string {
		return strings.TrimPrefix(strings.TrimPrefix(w.target(d), "break "), "continue ")
	}
	var cases []string
	for _, d := range in.BranchTable {
		cases = append(cases, short(d))
	}
	return strings.Join(carried, "") + "switch (" + cond + ") " + strings.Join(cases, ", ") + " else " + short(in.BranchDefault)
}

// gotoCase spells setting a switch's local to a constant and going back
// to its loop as going to a case. Go jumps that way inside a function
// and never reads PC_B after, so there the write goes unsaid.
func (w *stmtWriter) gotoCase(i int, op string, in *wasmir.Instruction) (string, bool) {
	if op != "br" || i < 2 || int(in.BranchDepth) >= len(w.frames) {
		return "", false
	}
	b := w.frames[len(w.frames)-1-int(in.BranchDepth)]
	set := &w.body[i-1]
	value, isConst := strings.CutPrefix(w.insts[i-2].Text, "i32.const ")
	n, err := strconv.Atoi(value)
	if !b.loop || b.selector == nil || set.Kind != wasmir.InstrLocalSet || set.LocalIndex != b.selector.local || !isConst || err != nil {
		return "", false
	}
	// Keep the write if a temporary was named on its row.
	if w.info.goWasm && !strings.Contains(w.insts[i-1].NativeText, ";") {
		w.insts[i-1].NativeText = ""
	}
	if n < 0 || n >= b.selector.cases {
		return "goto default", true
	}
	return "goto case " + strconv.Itoa(n), true
}

// call spells a call, which runs after the values it reads and before
// any that read what it might change.
func (w *stmtWriter) call(op, arg string, in *wasmir.Instruction) string {
	var t wasmir.TypeDef
	callee := strings.TrimPrefix(arg, "$")
	if op == "call_indirect" {
		t = typeOf(w.info.m, in.CallTypeIndex)
		callee = "table[" + w.pop().text + "]"
	} else if int(in.FuncIndex) < len(w.info.funcTypes) {
		t = w.info.funcTypes[in.FuncIndex]
	}
	call := callee + "(" + strings.Join(w.popN(len(t.Params)), ", ") + ")"
	w.settle(readsAny)
	w.selfReads.call = true
	switch {
	case op == "return_call":
		return "return " + call
	case len(t.Results) == 1:
		w.push(call, precAtom)
		return ""
	case len(t.Results) == 0:
		return call
	}
	// Several results get a temporary each.
	var names []string
	for range t.Results {
		names = append(names, w.temp())
	}
	for _, n := range names {
		w.stack = append(w.stack, expr{text: n, prec: precAtom, row: -1})
	}
	return strings.Join(names, ", ") + " = " + call
}

// value spells the instructions that compute a value or touch memory.
func (w *stmtWriter) value(op, arg, typ, short string, in *wasmir.Instruction) string {
	switch {
	case short == "const":
		v, err := strconv.ParseInt(arg, 0, 64)
		w.pushExpr(expr{text: arg, prec: precAtom, konst: err == nil, val: v})
	case strings.HasPrefix(short, "load"):
		w.settle(readsCall)
		w.selfReads.memory = true
		base := w.pop()
		if n, ok := w.sp.argName(base, in.MemoryOffset); ok {
			w.push(n, precAtom)
			break
		}
		w.push(memType(typ, short)+"["+address(base, in.MemoryOffset)+"]", precAtom)
	case strings.HasPrefix(short, "store"):
		v := w.pop()
		base := w.pop()
		a := memType(typ, short) + "[" + address(base, in.MemoryOffset) + "]"
		if n, ok := w.sp.argName(base, in.MemoryOffset); ok {
			a = n
		}
		w.settle(readsMemory)
		return a + " = " + v.text
	case op == "memory.size":
		w.settle(readsCall)
		w.selfReads.memory = true
		w.push("memory_size()", precAtom)
	case op == "memory.grow":
		n := w.pop()
		w.settle(readsMemory)
		// It changes memory as a call might, and drop shows it.
		w.selfReads.memory, w.selfReads.call = true, true
		w.push("memory_grow("+n.text+")", precAtom)
	case op == "memory.copy" || op == "memory.fill":
		args := w.popN(3)
		w.settle(readsMemory)
		return strings.ReplaceAll(op, ".", "_") + "(" + strings.Join(args, ", ") + ")"
	case binaryOps[short] != (binaryOp{}):
		w.binary(typ, short)
	case short == "eqz":
		a := w.pop()
		if a.not != "" {
			w.pushExpr(expr{text: a.not, prec: precCmp, not: a.text})
			break
		}
		w.pushExpr(expr{text: wrap(a, precAtom) + " == 0", prec: precCmp, not: wrap(a, precAtom) + " != 0"})
	case short == "neg":
		w.push("-"+wrap(w.pop(), precAtom), precAtom)
	case isUnary(short):
		w.unary(typ, short)
	default:
		// An instruction this doesn't know the operands of: show it
		// and start the block over with an unknown stack.
		w.cut()
		return w.insts[w.row].Text
	}
	return ""
}

func (w *stmtWriter) binary(typ, short string) {
	o := binaryOps[short]
	b, a := w.pop(), w.pop()
	if o.fn {
		w.push(short+"("+a.text+", "+b.text+")", precAtom)
		return
	}
	e := expr{
		text:    wrap(a, o.prec) + " " + o.text + " " + wrap(b, o.prec+1),
		prec:    o.prec,
		bitwise: strings.HasPrefix(short, "sh") || short == "and" || short == "or" || short == "xor",
	}
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
	w.pushExpr(e)
}

func (w *stmtWriter) unary(typ, short string) {
	a := w.pop()
	widen := strings.HasPrefix(short, "extend_i32")
	switch {
	case a.not != "" && widen:
		// A comparison is 0 or 1 at any width.
		w.pushExpr(expr{text: a.text, prec: a.prec, not: a.not})
		return
	case short == "wrap_i64" && a.narrow != "":
		w.pushExpr(expr{text: a.narrow, prec: precAtom, sp: a.sp, spOff: a.spOff})
		return
	}
	e := expr{text: convert(typ, short) + "(" + a.text + ")", prec: precAtom}
	if short == "wrap_i64" || widen {
		e.sp, e.spOff = a.sp, a.spOff
	}
	if widen {
		e.narrow = wrap(a, precAtom)
	}
	w.pushExpr(e)
}

// goSP follows Go's SP, which Go's wasm port keeps in local 1, to name
// the arguments and results a load or store reaches. args is nil
// outside Go.
type goSP struct {
	args  map[int64]string
	state goSPState
	// frame is where SP points once the function has made its frame.
	frame goSPState
}

// goSPState is where local 1 points, adj bytes from SP at the
// function's start, while known.
type goSPState struct {
	adj   int64
	known bool
}

// read is whether reading local gives a known address on Go's stack,
// and where.
func (s *goSP) read(local uint32) (bool, int64) {
	return s.args != nil && local == 1 && s.state.known, s.state.adj
}

func (s *goSP) isGlobal(global uint32) bool { return s.args != nil && global == 0 }

// set follows a store to local.
func (s *goSP) set(local uint32, v expr) {
	switch {
	case s.args == nil || local != 1:
	case v.sp:
		s.state = goSPState{v.spOff, true}
	case v.spGlobal && s.frame.known:
		// After a call SP is back at the frame.
		s.state = s.frame
	case v.spGlobal:
		// The function's start, before it makes its frame.
		s.state = goSPState{0, true}
	default:
		s.state.known = false
	}
}

// setGlobal follows a store to a global: the function's first SP
// outside the stack check makes its frame.
func (s *goSP) setGlobal(global uint32, v expr, inIf bool) {
	if s.isGlobal(global) && v.sp && !s.frame.known && !inIf {
		s.frame = goSPState{v.spOff, true}
	}
}

// caseStart follows the start of a case: Go enters case 0 at the
// function's start and any other to resume, with its frame made.
func (s *goSP) caseStart(cases string) {
	if cases == "case 0" || strings.HasPrefix(cases, "case 0,") {
		s.state = goSPState{0, true}
	} else {
		s.state = s.frame
	}
}

// argName is the argument or result a Go stack address reaches.
func (s *goSP) argName(base expr, offset uint64) (string, bool) {
	if !base.sp {
		return "", false
	}
	// The arguments start past the return address.
	n, ok := s.args[base.spOff+int64(offset)-8]
	return n, ok
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
	"add": {"+", precAdd, false}, "sub": {"-", precAdd, false}, "mul": {"*", precMul, false},
	"div": {"/", precMul, false}, "div_s": {"/", precMul, false}, "div_u": {"/u", precMul, false},
	"rem_s": {"%", precMul, false}, "rem_u": {"%u", precMul, false},
	"and": {"&", precMul, false}, "or": {"|", precAdd, false}, "xor": {"^", precAdd, false},
	"shl": {"<<", precMul, false}, "shr_s": {">>", precMul, false}, "shr_u": {">>u", precMul, false},
	"rotl": {"", precAtom, true}, "rotr": {"", precAtom, true},
	"eq": {"==", precCmp, false}, "ne": {"!=", precCmp, false},
	"lt": {"<", precCmp, false}, "lt_s": {"<", precCmp, false}, "lt_u": {"<u", precCmp, false},
	"gt": {">", precCmp, false}, "gt_s": {">", precCmp, false}, "gt_u": {">u", precCmp, false},
	"le": {"<=", precCmp, false}, "le_s": {"<=", precCmp, false}, "le_u": {"<=u", precCmp, false},
	"ge": {">=", precCmp, false}, "ge_s": {">=", precCmp, false}, "ge_u": {">=u", precCmp, false},
	"min": {"", precAtom, true}, "max": {"", precAtom, true}, "copysign": {"", precAtom, true},
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
	if base.prec < precAdd || base.bitwise {
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

// blockArity is how many values a block takes and leaves.
func blockArity(m *wasmir.Module, in *wasmir.Instruction) (params, results int) {
	switch {
	case in.BlockTypeUsesIndex:
		t := typeOf(m, in.BlockTypeIndex)
		return len(t.Params), len(t.Results)
	case in.BlockType != nil:
		return 0, 1
	}
	return 0, 0
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
// it by the loop's row.
func wasmSwitches(m *wasmir.Module, body []wasmir.Instruction, insts []disasm.Inst) (cases map[int]string, switches map[int]bool, selectors map[int]*wasmSelector) {
	cases, switches, selectors = map[int]string{}, map[int]bool{}, map[int]*wasmSelector{}
	for j, in := range body {
		if in.Kind != wasmir.InstrBrTable {
			continue
		}
		// Between the blocks and the br_table only the value is
		// computed, with nothing a statement would show.
		k := j - 1
		opener := func(k int) bool { return isWasmOpener(wasmOp(insts[k].Text)) }
		for k >= 0 && !opener(k) && pure(insts[k].Text) {
			k--
		}
		if k < 0 || !opener(k) {
			continue
		}
		// run holds the blocks opened one after another, innermost first.
		var run []int
		for ; k >= 0 && opener(k); k-- {
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
			params, results := blockArity(m, b)
			ok = b.Kind == wasmir.InstrBlock && params == 0 && results == 0 &&
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

// pure reports whether an instruction only computes a value from
// others, so it shows no statement of its own.
func pure(text string) bool {
	typ, short, _ := strings.Cut(wasmOp(text), ".")
	switch typ {
	case "local", "global":
		return short == "get"
	case "i32", "i64", "f32", "f64":
		return !strings.HasPrefix(short, "store") && !strings.HasPrefix(short, "atomic")
	}
	return false
}
