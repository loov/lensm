package avrasm

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestExhaustiveObjdump decodes every 16-bit word (each followed by a
// zero word, the address of the 32-bit forms) and compares with GNU
// avr-objdump. Skipped unless AVRASM_FUZZ is set.
func TestExhaustiveObjdump(t *testing.T) {
	if os.Getenv("AVRASM_FUZZ") == "" {
		t.Skip("AVRASM_FUZZ not set")
	}
	objdump := os.Getenv("OBJDUMP")
	if objdump == "" {
		objdump = "gobjdump"
	}
	var text bytes.Buffer
	for w := 0; w < 0x10000; w++ {
		binary.Write(&text, binary.LittleEndian, uint16(w))
		binary.Write(&text, binary.LittleEndian, uint16(0))
	}
	path := filepath.Join(t.TempDir(), "fuzz.elf")
	if err := os.WriteFile(path, minimalAVRELF(text.Bytes()), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(objdump, "-d", path).Output()
	if err != nil {
		t.Skipf("objdump: %v", err)
	}
	ref := map[uint64]string{}
	rx := regexp.MustCompile(`^\s*([0-9a-f]+):\s+((?:[0-9a-f]{2} )+)\s*(\S+)\s*([^;]*)(?:;\s*(0x[0-9a-f]+))?`)
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		m := rx.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		addr, _ := strconv.ParseUint(m[1], 16, 64)
		txt := m[3] + " " + strings.TrimSpace(m[4])
		if strings.Contains(m[4], ".+") || strings.Contains(m[4], ".-") {
			txt = m[3] + " " + m[5]
		}
		ref[addr] = strings.Join(strings.Fields(strings.ToLower(txt)), " ")
	}
	data := text.Bytes()
	var mismatches, undecoded, unknownRef int
	for w := 0; w < 0x10000; w++ {
		pc := uint64(4 * w)
		inst, err := Decode(data[pc:], pc)
		want := ref[pc]
		if err != nil {
			if want != "" && !strings.HasPrefix(want, ".word") {
				undecoded++
				if undecoded < 30 {
					t.Errorf("%04x: undecoded, objdump %q", w, want)
				}
			}
			continue
		}
		if strings.HasPrefix(want, ".word") || want == "" {
			unknownRef++
			continue
		}
		got := strings.Join(strings.Fields(strings.ToLower(inst.Text)), " ")
		if got != want && !(strings.HasSuffix(got, " 0x0") && strings.HasSuffix(want, " 0")) {
			mismatches++
			if mismatches < 40 {
				t.Errorf("%04x: got %q, objdump %q", w, got, want)
			}
		}
	}
	t.Logf("%d mismatches, %d undecoded, %d objdump .word", mismatches, undecoded, unknownRef)
}

// minimalAVRELF wraps code in an ELF32 AVR executable with a .text section.
func minimalAVRELF(code []byte) []byte {
	var b bytes.Buffer
	le := binary.LittleEndian
	const ehsz = 52
	shstr := []byte("\x00.text\x00.shstrtab\x00")
	shstrOff := ehsz + len(code)
	shOff := (shstrOff + len(shstr) + 3) &^ 3
	b.Write([]byte{0x7f, 'E', 'L', 'F', 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	binary.Write(&b, le, uint16(2))  // ET_EXEC
	binary.Write(&b, le, uint16(83)) // EM_AVR
	binary.Write(&b, le, uint32(1))
	binary.Write(&b, le, uint32(0)) // entry
	binary.Write(&b, le, uint32(0)) // phoff
	binary.Write(&b, le, uint32(shOff))
	binary.Write(&b, le, uint32(0x85)) // e_flags: avr5
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
	shdr(1, 1, 6, 0, ehsz, uint32(len(code)), 0, 0, 2, 0)
	shdr(7, 3, 0, 0, uint32(shstrOff), uint32(len(shstr)), 0, 0, 1, 0)
	return b.Bytes()
}
