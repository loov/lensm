package xtensaasm

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestFuzzObjdump compares random 24-bit words (and every 16-bit narrow
// word) with GNU xtensa objdump, each padded to 4 bytes with a trailing
// zero byte so the two walks stay aligned. Skipped unless XTENSAASM_FUZZ
// is set.
func TestFuzzObjdump(t *testing.T) {
	if os.Getenv("XTENSAASM_FUZZ") == "" {
		t.Skip("XTENSAASM_FUZZ not set")
	}
	objdump := os.Getenv("OBJDUMP")
	if objdump == "" {
		objdump = "gobjdump"
	}
	rng := rand.New(rand.NewPCG(3, 4))
	var text bytes.Buffer
	var samples []uint32
	for w := 0; w < 0x10000; w++ {
		if op0 := w & 0xf; op0 >= 8 && op0 <= 13 {
			samples = append(samples, uint32(w))
		}
	}
	for range 200000 {
		w := rng.Uint32() & 0xffffff
		if op0 := w & 0xf; op0 < 8 {
			samples = append(samples, w)
		}
	}
	for _, w := range samples {
		binary.Write(&text, binary.LittleEndian, w) // little-endian: op0 first, 4th byte zero
	}
	path := filepath.Join(t.TempDir(), "fuzz.elf")
	if err := os.WriteFile(path, minimalXtensaELF(text.Bytes()), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(objdump, "-d", path).Output()
	if err != nil {
		t.Skipf("objdump: %v", err)
	}
	ref := map[uint64]string{}
	rx := regexp.MustCompile(`^\s*([0-9a-f]+):\s+([0-9a-f]+)\s+(\S+)\s*(.*)$`)
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		m := rx.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		addr, _ := strconv.ParseUint(m[1], 16, 64)
		ops := m[4]
		if i := strings.Index(ops, " <"); i >= 0 {
			ops = ops[:i]
		}
		if i := strings.Index(ops, " ("); i >= 0 {
			ops = ops[:i] // l32r's literal value
		}
		want := strings.TrimSpace(m[3] + " " + ops)
		want = regexp.MustCompile(`\b([0-9a-f]{1,8})$`).ReplaceAllStringFunc(want, func(h string) string {
			if strings.HasPrefix(want, "j ") || strings.HasPrefix(want, "call") || strings.HasPrefix(want, "b") || strings.HasPrefix(want, "loop") || strings.HasPrefix(want, "l32r") {
				return "0x" + h
			}
			return h
		})
		ref[addr] = want + "|" + strconv.Itoa(len(m[2])/2)
	}
	data := text.Bytes()
	var mismatches, undecoded, skipped int
	seen := map[string]int{}
	extra := map[string]int{} // decoded here, undefined for objdump
	for i, w := range samples {
		pc := uint64(4 * i)
		want := ref[pc]
		inst, err := Decode(data[pc:], pc)
		mn, _, _ := strings.Cut(want, " ")
		mn, _, _ = strings.Cut(mn, "|")
		if want == "" || mn == ".byte" || mn == "excw" || mn == "{" || strings.HasPrefix(mn, "mul") && strings.Contains(mn, ".") || strings.HasPrefix(mn, "umul") || strings.HasPrefix(mn, "ldinc") || strings.HasPrefix(mn, "lddec") {
			skipped++
			if err == nil && (mn == "excw" || mn == ".byte") && !strings.HasPrefix(inst.Text, "excw") {
				extra[strings.Fields(inst.Text)[0]]++
			}
			continue
		}
		got := ""
		if err == nil {
			got = inst.Text + "|" + strconv.Itoa(inst.Len)
		}
		if got != want {
			key := mn
			if err != nil {
				undecoded++
			} else {
				mismatches++
			}
			seen[key]++
			if seen[key] <= 2 {
				t.Errorf("%06x: got %q (%v), objdump %q", w, got, err, want)
			}
		}
	}
	t.Logf("%d samples, %d mismatches, %d undecoded, %d skipped; by mnemonic: %v", len(samples), mismatches, undecoded, skipped, seen)
	t.Logf("decoded here but undefined for objdump: %v", extra)
}

func minimalXtensaELF(code []byte) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	const ehsz = 52
	shstr := []byte("\x00.text\x00.shstrtab\x00")
	shstrOff := ehsz + len(code)
	shOff := (shstrOff + len(shstr) + 3) &^ 3
	b.Write([]byte{0x7f, 'E', 'L', 'F', 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	binary.Write(&b, le, uint16(2))
	binary.Write(&b, le, uint16(94)) // EM_XTENSA
	binary.Write(&b, le, uint32(1))
	binary.Write(&b, le, uint32(0))
	binary.Write(&b, le, uint32(0))
	binary.Write(&b, le, uint32(shOff))
	binary.Write(&b, le, uint32(0x300)) // e_flags: XT_INSN|XT_LIT as TinyGo sets
	binary.Write(&b, le, uint16(ehsz))
	binary.Write(&b, le, uint16(32))
	binary.Write(&b, le, uint16(0))
	binary.Write(&b, le, uint16(40))
	binary.Write(&b, le, uint16(3))
	binary.Write(&b, le, uint16(2))
	b.Write(code)
	b.Write(shstr)
	for b.Len() < shOff {
		b.WriteByte(0)
	}
	shdr := func(v ...uint32) {
		for _, x := range v {
			binary.Write(&b, le, x)
		}
	}
	shdr(0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	shdr(1, 1, 6, 0, ehsz, uint32(len(code)), 0, 0, 4, 0)
	shdr(7, 3, 0, 0, uint32(shstrOff), uint32(len(shstr)), 0, 0, 1, 0)
	return b.Bytes()
}
