package wasmobj

import (
	"cmp"
	"debug/dwarf"
	"slices"

	"github.com/eliben/watgo/wasmir"
)

// lineIndex is the DWARF line table of a module — what TinyGo and clang
// emit, where Go emits a pclntab — as rows sorted by module offset.
type lineIndex struct{ rows []lineRow }

type lineRow struct {
	addr uint64
	file string
	line int
}

// newLineIndex reads the DWARF line table from the module's custom
// sections — what TinyGo and clang emit, where Go emits a pclntab.
// Its addresses are offsets within the code section, so they are shifted
// by that section's position to index the file like everything else here.
// nil when there is no DWARF, or when the addresses do not line up with
// the functions: wasm-opt and friends rewrite code without updating
// DWARF, and lines from a stale table would be confidently wrong.
func newLineIndex(module *wasmir.Module, codeStart uint64, bodies []codeRange) *lineIndex {
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

	index := &lineIndex{}
	reader := data.Reader()
	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			break
		}
		if entry.Tag != dwarf.TagCompileUnit {
			continue
		}
		lines, err := data.LineReader(entry)
		if err != nil || lines == nil {
			continue
		}
		var row dwarf.LineEntry
		for lines.Next(&row) == nil {
			if row.EndSequence || row.File == nil {
				continue
			}
			index.rows = append(index.rows, lineRow{
				addr: row.Address + codeStart,
				file: row.File.Name,
				line: row.Line,
			})
		}
		reader.SkipChildren()
	}
	if len(index.rows) == 0 {
		return nil
	}
	slices.SortFunc(index.rows, func(a, b lineRow) int {
		if a.addr != b.addr {
			return cmp.Compare(a.addr, b.addr)
		}
		return cmp.Compare(a.line, b.line)
	})
	return index
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

// at returns the source position covering addr: the last row at or
// before it, within limit. Rows mark where a statement begins, so an
// instruction inherits the position of the statement it belongs to.
func (index *lineIndex) at(addr, limit uint64) (string, int) {
	i, found := slices.BinarySearchFunc(index.rows, addr, func(row lineRow, addr uint64) int {
		return cmp.Compare(row.addr, addr)
	})
	if !found {
		i--
	}
	if i < 0 || index.rows[i].addr < limit {
		return "", 0
	}
	return index.rows[i].file, index.rows[i].line
}
