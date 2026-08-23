package objfile

import (
	"cmp"
	"debug/dwarf"
	"os"
	"slices"
	"strings"
)

// Lines maps addresses to source positions, as DWARF records them for
// code the Go compiler didn't produce. Rows sit at the start of a
// statement and hold until the next one, so a lookup takes the last row
// at or before the address.
type Lines struct{ rows []lineRow }

type lineRow struct {
	addr uint64
	file string
	line int
}

// LinesFromDWARF reads the line tables of every compilation unit. shift
// is added to each address, for formats whose DWARF addresses are
// relative to something other than the addresses used elsewhere — wasm
// counts from the start of the code section. nil when there are no rows.
func LinesFromDWARF(data *dwarf.Data, shift int64) *Lines {
	lines := &Lines{}
	reader := data.Reader()
	for {
		entry, err := reader.Next()
		if err != nil || entry == nil {
			break
		}
		if entry.Tag != dwarf.TagCompileUnit {
			continue
		}
		unit, err := data.LineReader(entry)
		if err != nil || unit == nil {
			continue
		}
		compDir, _ := entry.Val(dwarf.AttrCompDir).(string)
		fixed := map[string]string{}
		var row dwarf.LineEntry
		for unit.Next(&row) == nil {
			out := lineRow{addr: uint64(int64(row.Address) + shift)}
			// An end-sequence row marks where the code of a sequence
			// stops. Keeping it with no position is what stops the last
			// statement of one function from covering whatever follows.
			if !row.EndSequence && row.File != nil {
				name, ok := fixed[row.File.Name]
				if !ok {
					name = unjoinCompDir(compDir, row.File.Name)
					fixed[row.File.Name] = name
				}
				out.file, out.line = name, row.Line
			}
			lines.rows = append(lines.rows, out)
		}
		reader.SkipChildren()
	}
	if len(lines.rows) == 0 {
		return nil
	}
	slices.SortFunc(lines.rows, func(a, b lineRow) int {
		return cmp.Or(cmp.Compare(a.addr, b.addr), cmp.Compare(a.line, b.line))
	})
	return lines
}

// unjoinCompDir undoes debug/dwarf joining an absolute file name onto
// the compilation directory. Its pathJoin documents that the name must
// be relative, but clang records absolute names, and DWARF says the
// directory is then ignored — so "/build/dir" + "/src/prog.c" arrives as
// "/build/dir/src/prog.c". The join is only undone when the path it
// produced is missing and the plain one is there, so a project that
// really does have that layout keeps working.
func unjoinCompDir(compDir, name string) string {
	if compDir == "" || !strings.HasPrefix(name, compDir+"/") {
		return name
	}
	if _, err := os.Stat(name); err == nil {
		return name
	}
	absolute := name[len(compDir):]
	if _, err := os.Stat(absolute); err != nil {
		return name
	}
	return absolute
}

// At returns the source position covering addr; zero values when no row
// covers it.
func (lines *Lines) At(addr uint64) (string, int) {
	if lines == nil {
		return "", 0
	}
	i, found := slices.BinarySearchFunc(lines.rows, addr, func(row lineRow, addr uint64) int {
		return cmp.Compare(row.addr, addr)
	})
	if !found {
		i--
	}
	if i < 0 {
		return "", 0
	}
	return lines.rows[i].file, lines.rows[i].line
}
