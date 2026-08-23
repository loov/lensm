// Package wasmobj loads WebAssembly modules as disasm.Files. Function
// bodies are rendered as WAT through watgo; there is no source mapping.
package wasmobj

import (
	"debug/gosym"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/eliben/watgo"
	"github.com/eliben/watgo/wasmir"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/objfile"
	"loov.dev/lensm/internal/source"
)

var _ disasm.File = (*File)(nil)
var _ disasm.Func = (*Func)(nil)

// File is a loaded wasm module.
type File struct {
	// names maps the function index space (imports first, then defined
	// functions) to display names, for resolving call targets.
	names []string
	funcs []disasm.Func
	// data is the module file, which DWARF addresses index into.
	data []byte
	// pcln is the Go line table recovered from the module's data
	// segments, addressed by wasm PC; nil for non-Go modules.
	pcln *gosym.Table
	// lines is the DWARF line table, addressed by module offset; what
	// TinyGo and clang emit instead of a pclntab. nil when absent.
	lines *lineIndex
}

func (file *File) Funcs() []disasm.Func { return file.funcs }
func (file *File) Close() error         { return nil }

// Func is one module-defined function.
type Func struct {
	obj  *File
	name string
	fn   wasmir.Function
	// index is the function's position in the module's function index
	// space, the PC_F half of every PC inside it.
	index int
	// body is where this function's instructions start in the file and
	// how many bytes they span, for looking up DWARF rows.
	body codeRange
}

func (fn *Func) Name() string { return fn.name }

func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// A component — what TinyGo's wasip2 target and other component-model
	// tools emit — shares the magic of a core module but carries a
	// different version, and holds its core modules nested inside.
	if len(data) >= 8 && data[4] == 0x0d && data[5] == 0x00 && data[6] == 0x01 && data[7] == 0x00 {
		return nil, fmt.Errorf("%q: WebAssembly component, only core modules are supported", path)
	}
	module, err := watgo.DecodeWASM(data)
	if err != nil {
		return nil, fmt.Errorf("%q: %w", path, err)
	}

	file := &File{}
	for _, imp := range module.Imports {
		if imp.Kind == wasmir.ExternalKindFunction {
			file.names = append(file.names, imp.Module+"."+imp.Name)
		}
	}
	codeStart, bodies := codeSection(data)
	for i, fn := range module.Funcs {
		name := fn.Name
		if name == "" {
			name = fmt.Sprintf("func%d", len(file.names))
		}
		entry := codeRange{}
		if i < len(bodies) {
			entry = bodies[i]
		}
		file.funcs = append(file.funcs, &Func{obj: file, name: name, fn: module.Funcs[i], index: i, body: entry})
		file.names = append(file.names, name)
	}
	file.pcln = objfile.FindWasmLineTable(linearMemory(module))
	if file.pcln == nil {
		file.lines = newLineIndex(module, codeStart, bodies)
	}
	file.data = data
	sort.SliceStable(file.funcs, func(i, k int) bool {
		return strings.ToLower(file.funcs[i].Name()) < strings.ToLower(file.funcs[k].Name())
	})
	return file, nil
}

// wasmPCBase is the first PC_F value the Go linker assigns to a
// function (funcValueOffset in cmd/link/internal/wasm).
const wasmPCBase = 0x1000

// linearMemory reconstructs the module's initialized memory from its
// active data segments, where a Go module's pclntab lives. It gives up
// on a segment placed implausibly far past the data actually present:
// a module can name a huge offset with a few bytes of payload.
func linearMemory(module *wasmir.Module) []byte {
	const maxImage = 1 << 31
	var end, total int64
	for _, seg := range module.Data {
		off, ok := segmentOffset(seg)
		if !ok {
			continue
		}
		if e := off + int64(len(seg.Init)); e > end {
			end = e
		}
		total += int64(len(seg.Init))
	}
	if end <= 0 || end > maxImage || end > total+1<<20 {
		return nil
	}
	image := make([]byte, end)
	for _, seg := range module.Data {
		if off, ok := segmentOffset(seg); ok {
			copy(image[off:], seg.Init)
		}
	}
	return image
}

// segmentOffset returns the linear-memory offset of an active data
// segment with a constant offset; ok is false for passive segments and
// for offsets that are not a plain constant.
func segmentOffset(seg wasmir.DataSegment) (int64, bool) {
	if seg.Mode != wasmir.DataSegmentModeActive {
		return 0, false
	}
	if len(seg.OffsetExpr) > 0 {
		switch in := seg.OffsetExpr[0]; in.Kind {
		case wasmir.InstrI32Const:
			return int64(in.I32Const), in.I32Const >= 0
		case wasmir.InstrI64Const:
			return in.I64Const, in.I64Const >= 0
		default:
			return 0, false
		}
	}
	return seg.OffsetI64, seg.OffsetI64 >= 0
}

// resumeBlocks returns, for each instruction of body, the index of the
// resume point it belongs to — the PC_B half of its PC. The compiler
// wraps a function's body in one block per resume point and dispatches
// on PC_B through a br_table, so after that dispatch each block that
// ends moves execution into the next resume point.
func resumeBlocks(body []wasmir.Instruction) []int {
	blocks := make([]int, len(body))
	depth, block := 0, 0
	// dispatched turns on at the end that closes the br_table's block;
	// resumeDepth is then the depth of the innermost resume block.
	dispatched, sawTable, resumeDepth := false, false, 0
	for i, in := range body {
		if in.Kind == wasmir.InstrEnd {
			depth--
			switch {
			case !dispatched && sawTable:
				dispatched, resumeDepth = true, depth
			case dispatched && depth == resumeDepth-1:
				block++
				resumeDepth--
			}
		}
		blocks[i] = block
		switch in.Kind {
		case wasmir.InstrBlock, wasmir.InstrLoop, wasmir.InstrIf:
			depth++
		case wasmir.InstrBrTable:
			sawTable = true
		}
	}
	return blocks
}

// pcToLine maps the resume point block of this function to a source
// position; zero values when the module carries no Go line table.
func (fn *Func) pcToLine(block int) (string, int) {
	if fn.obj.pcln == nil {
		return "", 0
	}
	file, line, _ := fn.obj.pcln.PCToLine(uint64(wasmPCBase+fn.index)<<16 | uint64(block))
	if line < 0 {
		return "", 0
	}
	return file, line
}

// instructionOffsets returns the file offset of every instruction in the
// body, for looking up DWARF rows. Lengths come from re-encoding each
// instruction, and the total is checked against the bytes the function
// actually occupies: a module whose encoding differs from watgo's — a
// non-canonical integer, an instruction it round-trips differently —
// would otherwise shift every later offset silently. nil when the module
// carries no DWARF, the body is unreadable, or the check fails.
func (fn *Func) instructionOffsets() []uint64 {
	if fn.obj.lines == nil || fn.body.size == 0 {
		return nil
	}
	body := fn.obj.data[fn.body.start : fn.body.start+fn.body.size]
	locals := localsSize(body)
	if locals < 0 {
		return nil
	}

	// Encoding a body of just "end" gives the fixed cost of the wrapper
	// module, so each instruction's length is what it adds to that.
	empty, ok := encodedSize(fn.fn.Locals, []wasmir.Instruction{{Kind: wasmir.InstrEnd}})
	if !ok {
		return nil
	}
	offsets := make([]uint64, len(fn.fn.Body))
	next := fn.body.start + uint64(locals)
	for i, instruction := range fn.fn.Body {
		size, ok := encodedSize(fn.fn.Locals, []wasmir.Instruction{instruction, {Kind: wasmir.InstrEnd}})
		if !ok {
			return nil
		}
		offsets[i] = next
		next += uint64(size - empty)
	}
	if next != fn.body.start+fn.body.size {
		return nil
	}
	return offsets
}

// encodedSize is the size of a module holding one function with this
// body; only differences between calls are meaningful.
func encodedSize(locals []wasmir.ValueType, body []wasmir.Instruction) (int, bool) {
	encoded, err := watgo.EncodeWASM(&wasmir.Module{
		Types: []wasmir.TypeDef{{Kind: wasmir.TypeDefKindFunc}},
		Funcs: []wasmir.Function{{Locals: locals, Body: body}},
	})
	if err != nil {
		return 0, false
	}
	return len(encoded), true
}

func (fn *Func) Load(opts disasm.Options) (*disasm.Code, error) {
	// watgo prints whole modules only, so the body goes into a synthetic
	// single-function module under a void signature; the real signature
	// is not needed to render the instructions.
	wat, err := watgo.PrintWAT(&wasmir.Module{
		Types: []wasmir.TypeDef{{Kind: wasmir.TypeDefKindFunc}},
		Funcs: []wasmir.Function{{Locals: fn.fn.Locals, Body: fn.fn.Body}},
	})
	if err != nil {
		return nil, err
	}

	blocks := resumeBlocks(fn.fn.Body)
	offsets := fn.instructionOffsets()
	position := func(i int) (string, int) {
		if offsets != nil {
			return fn.obj.lines.at(offsets[i], fn.body.start)
		}
		return fn.pcToLine(blocks[i])
	}

	code := &disasm.Code{Name: fn.name, Arch: "wasm"}
	refs := source.Refs{}
	// The body prints one instruction per line between "(func" and its
	// closing ")", in Body order; the final InstrEnd becomes the ")".
	// Block nesting is kept as one space per level: Go's function
	// prologue opens one block per resume point, so two would run wide.
	inBody := false
	for line := range strings.Lines(string(wat)) {
		text := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(text, "(func"):
			inBody = true
			continue
		case !inBody || text == "" || text == ")":
			continue
		}
		i := len(code.Insts)
		if i >= len(fn.fn.Body) {
			break
		}
		if in := fn.fn.Body[i]; in.Kind == wasmir.InstrCall {
			if idx := int(in.FuncIndex); idx < len(fn.obj.names) {
				text = "call " + fn.obj.names[idx]
			}
		}
		level := (len(line) - len(strings.TrimLeft(line, " ")) - 4) / 2
		inst := disasm.Inst{PC: uint64(i), Text: strings.Repeat(" ", max(level, 0)) + text}
		inst.File, inst.Line = position(i)
		if code.File == "" {
			// The function's own file: the prologue can precede the first
			// line-table row, so take it from the first instruction that
			// has one rather than from the entry.
			code.File = inst.File
		}
		if strings.HasPrefix(text, "call ") {
			inst.Call = strings.TrimPrefix(text, "call ")
		}
		refs.Add(inst.File, inst.Line, i)
		code.Insts = append(code.Insts, inst)
	}
	code.Source = source.Load(refs, code.File, opts.Context)
	return code, nil
}
