package wasmobj

import (
	"debug/dwarf"

	"github.com/eliben/watgo/wasmir"

	"loov.dev/lensm/internal/objfile"
)

// dwarfLines reads the DWARF line table from the module's custom
// sections — what TinyGo and clang emit, where Go emits a pclntab.
// Its addresses are offsets within the code section, so they are shifted
// by that section's position to index the module like everything else
// here. nil when there is no DWARF, or when the addresses do not line up
// with the functions: wasm-opt and friends rewrite code without updating
// DWARF, and lines from a stale table would be confidently wrong.
func dwarfLines(module *wasmir.Module, codeStart uint64, bodies []codeRange) *objfile.Lines {
	section := map[string][]byte{}
	for _, custom := range module.CustomSections {
		section[custom.Name] = custom.Payload
	}
	if section[".debug_line"] == nil || section[".debug_info"] == nil {
		return nil
	}
	data, err := dwarf.New(section[".debug_abbrev"], section[".debug_aranges"], nil,
		section[".debug_info"], section[".debug_line"], nil,
		section[".debug_ranges"], section[".debug_str"])
	if err != nil {
		return nil
	}
	if !describesBodies(data, codeStart, bodies) {
		return nil
	}
	return objfile.LinesFromDWARF(data, int64(codeStart))
}

// describesBodies reports whether the DWARF still matches the module:
// every subprogram entry names the start of a real function body. One
// that does not means the code moved after the debug info was written.
func describesBodies(data *dwarf.Data, codeStart uint64, bodies []codeRange) bool {
	starts := make(map[uint64]bool, len(bodies))
	for _, body := range bodies {
		starts[body.start] = true
	}
	found := 0
	reader := data.Reader()
	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			break
		}
		if entry.Tag != dwarf.TagSubprogram {
			continue
		}
		low, ok := entry.Val(dwarf.AttrLowpc).(uint64)
		if !ok || low == 0 {
			continue
		}
		if !starts[low+codeStart] {
			return false
		}
		found++
	}
	return found > 0
}
