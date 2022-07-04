package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gioui.org/widget"
	"golang.org/x/exp/slices"

	"loov.dev/lensm/internal/objfile"
)

type Options struct {
	Exe        string
	Filter     *regexp.Regexp
	Context    int
	MaxSymbols int
}

type Output struct {
	Matches []Match
	More    bool
}

type Match struct {
	Name string
	File string

	Code         []Instruction
	CodeMaxStack int

	Source []Source

	// UI
	Select widget.Clickable
}

type Instruction struct {
	PC   uint64
	Text string
	File string
	Line int

	RefPC     uint64
	RefOffset int
	RefStack  int
}

type Source struct {
	File   string
	Blocks []SourceBlock
}

type SourceBlock struct {
	Range
	Lines   []string
	Related [][]Range // for each line, a range index in Match.Code
}

var rxRefAbs = regexp.MustCompile(`\s0x[0-9a-fA-F]+$`)
var rxRefRel = regexp.MustCompile(`\s\-?[0-9]+\(PC\)$`)

func Parse(opts Options) (*Output, error) {
	f, err := objfile.Open(opts.Exe)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dis, err := f.Disasm()
	if err != nil {
		return nil, err
	}

	out := &Output{}

	for _, sym := range dis.Syms() {
		symStart := sym.Addr
		symEnd := sym.Addr + uint64(sym.Size)
		relocs := sym.Relocs
		if sym.Code != 'T' && sym.Code != 't' ||
			symStart < dis.TextStart() ||
			opts.Filter != nil && !opts.Filter.MatchString(sym.Name) {
			continue
		}
		if len(out.Matches) == opts.MaxSymbols {
			out.More = true
			break
		}

		neededLines := make(map[string]*LineSet)

		file, _, _ := dis.PCLN().PCToLine(sym.Addr)
		needRefPCs := map[uint64]struct{}{}

		sym := Match{
			Name: sym.Name,
			File: file,
		}
		initialIx := []Instruction{}
		dis.Decode(symStart, symEnd, relocs, false, func(pc, size uint64, file string, line int, text string) {
			// TODO: find a better way to calculate the jump target
			var refPC uint64
			if match := rxRefAbs.FindString(text); match != "" {
				if target, err := strconv.ParseInt(match[3:], 16, 64); err == nil {
					refPC = uint64(target)
				}
			} else if match := rxRefRel.FindString(text); match != "" {
				// TODO: this calculation seems incorrect
				if target, err := strconv.ParseInt(match[1:len(match)-4], 10, 64); err == nil {
					refPC = uint64(int64(pc) + target*4)
				} else {
					panic(err)
				}
			}

			if refPC != 0 {
				needRefPCs[refPC] = struct{}{}
			}
			initialIx = append(initialIx, Instruction{
				PC:    pc,
				Text:  text,
				File:  file,
				Line:  line,
				RefPC: refPC,
			})

			if file != "" {
				lineset, ok := neededLines[file]
				if !ok {
					lineset = &LineSet{}
					neededLines[file] = lineset
				}
				lineset.Add(line)
			}
		})

		pcToIndex := map[uint64]int{}
		for _, ix := range initialIx {
			if _, ok := needRefPCs[ix.PC]; ok {
				// add empty line
				sym.Code = append(sym.Code, Instruction{})
			}
			pcToIndex[ix.PC] = len(sym.Code)
			sym.Code = append(sym.Code, ix)
		}

		type jumpInterval struct {
			index    int
			ix       *Instruction
			min, max uint64
		}

		jumps := []jumpInterval{}
		for i := range sym.Code {
			ix := &sym.Code[i]
			if ix.RefPC != 0 {
				target, ok := pcToIndex[ix.RefPC]
				if !ok {
					continue
				}
				ix.RefOffset = target - i

				if ix.PC <= ix.RefPC {
					jumps = append(jumps, jumpInterval{
						index: i,
						ix:    ix,
						min:   ix.PC,
						max:   ix.RefPC,
					})
				} else {
					jumps = append(jumps, jumpInterval{
						index: i,
						ix:    ix,
						min:   ix.RefPC,
						max:   ix.PC,
					})
				}
			}
		}

		sort.Slice(jumps, func(i, k int) bool {
			if jumps[i].min == jumps[k].min {
				return jumps[i].max > jumps[k].max
			}
			return jumps[i].min < jumps[k].min
		})

		stackLayers := []uint64{}
		insertToStack := func(ix *Instruction, max uint64) {
			found := false
			for k, pc := range stackLayers {
				if pc == 0 {
					stackLayers[k] = max
					ix.RefStack = k
					found = true
					break
				}
			}
			if !found {
				sym.CodeMaxStack = len(stackLayers)
				ix.RefStack = len(stackLayers)
				stackLayers = append(stackLayers, max)
			}
		}

		for _, jump := range jumps {
			for i, pc := range stackLayers {
				if pc <= jump.min {
					stackLayers[i] = 0
				}
			}
			insertToStack(jump.ix, jump.max)
		}
		for i := range sym.Code {
			ix := &sym.Code[i]
			ix.RefStack = sym.CodeMaxStack - ix.RefStack + 1
		}
		sym.CodeMaxStack++

		// remove trailing interrupts from funcs
		for len(sym.Code) > 0 && strings.HasPrefix(sym.Code[len(sym.Code)-1].Text, "INT ") {
			sym.Code = sym.Code[:len(sym.Code)-1]
		}

		// load sources
		sym.Source = LoadSources(neededLines, sym.File, opts.Context)

		// create a mapping from source code to disassembly
		type fileLine struct {
			file string
			line int
		}

		lineRefs := map[fileLine]*LineSet{}
		for i, ix := range sym.Code {
			k := fileLine{file: ix.File, line: ix.Line}
			n, ok := lineRefs[k]
			if !ok {
				n = &LineSet{}
				lineRefs[k] = n
			}
			n.Add(i)
		}
		for i := range sym.Source {
			src := &sym.Source[i]
			for k := range src.Blocks {
				block := &src.Blocks[k]
				block.Related = make([][]Range, len(block.Lines))
				for line := block.From; line <= block.To; line++ { // todo check: line <= block.To
					if refs, ok := lineRefs[fileLine{file: src.File, line: line}]; ok {
						block.Related[line-block.From] = refs.RangesZero()
					}
				}
			}
		}

		out.Matches = append(out.Matches, sym)
	}

	return out, nil
}

func LoadSources(needed map[string]*LineSet, symbolFile string, context int) []Source {
	sources := []Source{}
	for file, set := range needed {
		data, err := os.ReadFile(file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to load source from %q: %v\n", file, err)
			continue
		}
		lines := strings.Split(string(data), "\n")
		source := Source{
			File: file,
		}
		for _, r := range set.Ranges(context) {
			to := r.To - 1
			if to > len(lines) {
				to = len(lines)
			}
			lineBlock := lines[r.From-1 : to]
			for i, v := range lineBlock {
				lineBlock[i] = strings.Replace(v, "\t", "    ", -1)
			}

			source.Blocks = append(source.Blocks, SourceBlock{
				Range: r,
				Lines: lineBlock,
			})
		}
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].File == symbolFile {
			return true
		}
		if sources[j].File == symbolFile {
			return false
		}
		return sources[i].File < sources[j].File
	})
	return sources
}

type LineSet struct {
	Needed []int
}

func (rs *LineSet) Add(line int) {
	if len(rs.Needed) == 0 {
		rs.Needed = append(rs.Needed, line)
		return
	}
	at := sort.SearchInts(rs.Needed, line)
	if at >= len(rs.Needed) {
		rs.Needed = append(rs.Needed, line)
	} else if rs.Needed[at] != line {
		rs.Needed = slices.Insert(rs.Needed, at, line)
	}
}

func (rs *LineSet) Ranges(context int) []Range {
	if len(rs.Needed) == 0 {
		return nil
	}

	all := []Range{}

	current := Range{From: rs.Needed[0] - context, To: rs.Needed[0] + context + 1}
	if current.From < 1 {
		current.From = 1
	}
	for _, line := range rs.Needed {
		if line-context <= current.To {
			current.To = line + context + 1
		} else {
			all = append(all, current)
			current = Range{From: line - context, To: line + context + 1}
		}
	}
	all = append(all, current)

	return all
}
func (rs *LineSet) RangesZero() []Range {
	if len(rs.Needed) == 0 {
		return nil
	}

	all := []Range{}

	current := Range{From: rs.Needed[0], To: rs.Needed[0] + 1}
	for _, line := range rs.Needed {
		if line <= current.To {
			current.To = line + 1
		} else {
			all = append(all, current)
			current = Range{From: line, To: line + 1}
		}
	}
	all = append(all, current)

	return all
}

type Range struct{ From, To int }
