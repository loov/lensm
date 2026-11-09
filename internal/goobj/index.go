package goobj

import (
	"runtime/debug"
	"slices"

	"github.com/loov/disasm/analysis"

	"loov.dev/lensm/internal/disasm"
)

var _ disasm.Indexer = (*File)(nil)

// Index disassembles every function for its calls and costs. It holds
// the lock one function at a time, so the code view can load in
// between.
func (file *File) Index() *disasm.Index {
	index := &disasm.Index{
		Callers: map[string][]string{},
		Stats:   make([]disasm.FuncStats, len(file.funcs)),
	}
	for i, f := range file.funcs {
		fn := f.(*Func)
		calls, stats, ok := file.scan(fn)
		if !ok {
			// The binary changed under the mapping.
			return nil
		}
		index.Stats[i] = stats
		for _, callee := range calls {
			index.Callers[callee] = append(index.Callers[callee], fn.Name())
		}
	}
	for callee, callers := range index.Callers {
		slices.Sort(callers)
		index.Callers[callee] = slices.Compact(callers)
	}
	return index
}

// scan decodes fn for the functions it calls and its stats. It
// reports false when reading the binary faults, see File.disassemble.
func (file *File) scan(fn *Func) (calls []string, stats disasm.FuncStats, ok bool) {
	file.mu.Lock()
	defer file.mu.Unlock()
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	stats.Size = fn.fn.Size
	decoded, err := file.bin.Disassemble(fn.fn)
	if err != nil {
		return nil, stats, true
	}
	var stack []analysis.StackAccess
	if file.bin.Arch != "wasm" {
		stack = analysis.StackAccesses(file.bin.Arch, decoded)
	}
	for i, in := range decoded {
		var call string
		if stack == nil {
			call = wasmCall(in)
		} else {
			call = callName(file.bin, fn, in)
			marks := instMarks(stack[i], in, call)
			for k, m := range disasm.Marks {
				if marks&m != 0 {
					stats.Marks[k]++
				}
			}
		}
		if in.Call && call != "" && call != fn.Name() {
			calls = append(calls, call)
		}
	}
	return calls, stats, true
}
