package thumbasm

import (
	"bufio"
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestCompareObjdump decodes every Thumb region of the ELF named by
// THUMBASM_ELF and compares against llvm-objdump (OBJDUMP, default
// /usr/bin/objdump, with OBJDUMP_TRIPLE selecting the target; set
// THUMBASM_STRICT_W to compare the .w qualifiers too). Skipped unless
// THUMBASM_ELF is set: it is the development check against a reference
// disassembler, e.g. on a TinyGo build for pico, pico2 or feather-m4.
func TestCompareObjdump(t *testing.T) {
	path := os.Getenv("THUMBASM_ELF")
	if path == "" {
		t.Skip("THUMBASM_ELF not set")
	}
	objdump := os.Getenv("OBJDUMP")
	if objdump == "" {
		objdump = "/usr/bin/objdump"
	}
	triple := os.Getenv("OBJDUMP_TRIPLE")
	if triple == "" {
		triple = "thumbv7em-none-eabihf"
	}
	out, err := exec.Command(objdump, "-d", "--triple="+triple, path).Output()
	if err != nil {
		t.Skipf("objdump: %v", err)
	}
	ref := parseObjdump(string(out))

	f, err := elf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	text := f.Section(".text")
	data, err := text.Data()
	if err != nil {
		t.Fatal(err)
	}
	syms, _ := f.Symbols()
	type mapping struct {
		addr uint64
		kind byte
	}
	var maps []mapping
	for _, s := range syms {
		if strings.HasPrefix(s.Name, "$") && s.Value >= text.Addr && s.Value < text.Addr+text.Size {
			maps = append(maps, mapping{s.Value, s.Name[1]})
		}
	}
	sort.Slice(maps, func(i, j int) bool { return maps[i].addr < maps[j].addr })

	var total, mismatches, undecoded int
	var dec Decoder
	for i, m := range maps {
		if m.kind != 't' {
			continue
		}
		end := text.Addr + text.Size
		if i+1 < len(maps) {
			end = maps[i+1].addr
		}
		dec = Decoder{}
		for pc := m.addr; pc < end; {
			code := data[pc-text.Addr : end-text.Addr]
			inst, err := dec.Decode(code, pc)
			want, ok := ref[pc]
			if err != nil {
				if ok && !strings.HasPrefix(want, "<unknown>") {
					undecoded++
					if undecoded <= 40 {
						t.Errorf("%#x: undecoded (%v), objdump: %s", pc, err, want)
					}
				}
				pc += 2
				continue
			}
			total++
			// PC-relative operands are spelled as absolute targets here
			// and as offsets by objdump; branches still check targets.
			if strings.Contains(want, "[pc") || strings.HasPrefix(want, "adr") {
				ok = false
			}
			if ok && normalize(inst.Text) != normalize(want) {
				mismatches++
				if mismatches <= 60 {
					t.Errorf("%#x: got %q, objdump %q", pc, inst.Text, want)
				}
			}
			pc += uint64(inst.Len)
		}
	}
	t.Logf("%d instructions, %d mismatches, %d undecoded", total, mismatches, undecoded)
}

var rxObjdumpLine = regexp.MustCompile(`^\s*([0-9a-f]+):\s+(?:[0-9a-f]{4} ?){1,2}\s+(.*)$`)

func parseObjdump(out string) map[uint64]string {
	ref := map[uint64]string{}
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		m := rxObjdumpLine.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		addr, _ := strconv.ParseUint(m[1], 16, 64)
		text := m[2]
		if i := strings.Index(text, "@"); i >= 0 {
			text = text[:i]
		}
		if i := strings.Index(text, "<"); i > 0 {
			text = text[:i]
		}
		ref[addr] = strings.TrimSpace(text)
	}
	return ref
}

var rxNum = regexp.MustCompile(`#?-?(0x[0-9a-f]+|\d+)`)
var rxFloat = regexp.MustCompile(`#-?\d+\.\d+(e[-+]\d+)?`)
var rxRange = regexp.MustCompile(`\{([sd])(\d+)-[sd](\d+)\}`)

// normalize makes llvm-objdump and thumbasm spellings comparable: hex vs
// decimal immediates, the optional .w width qualifier, and whitespace.
func normalize(s string) string {
	s = strings.ToLower(s)
	if os.Getenv("THUMBASM_STRICT_W") == "" {
		s = strings.ReplaceAll(s, ".w", "")
		s = strings.ReplaceAll(s, ".n", "")
	}
	s = strings.ReplaceAll(s, ", #-0]", "]") // [rn, #0] and [rn, #-0] are [rn]
	s = strings.ReplaceAll(s, ", #0]", "]")
	s = rxFloat.ReplaceAllStringFunc(s, func(m string) string {
		v, err := strconv.ParseFloat(m[1:], 64)
		if err != nil {
			return m
		}
		return "#" + strconv.FormatFloat(v, 'g', -1, 64)
	})
	s = rxRange.ReplaceAllStringFunc(s, func(m string) string {
		sub := rxRange.FindStringSubmatch(m)
		lo, _ := strconv.Atoi(sub[2])
		hi, _ := strconv.Atoi(sub[3])
		var names []string
		for i := lo; i <= hi; i++ {
			names = append(names, fmt.Sprintf("%s%d", sub[1], i))
		}
		return "{" + strings.Join(names, ", ") + "}"
	})
	s = rxNum.ReplaceAllStringFunc(s, func(m string) string {
		neg := strings.Contains(m, "-")
		m = strings.TrimLeft(m, "#-")
		v, err := strconv.ParseInt(m, 0, 64)
		if err != nil {
			return m
		}
		if neg {
			v = -v
		}
		return fmt.Sprint(v)
	})
	s = strings.ReplaceAll(s, ",", " ")
	return strings.Join(strings.Fields(s), " ")
}
