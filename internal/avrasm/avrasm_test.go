package avrasm

import (
	"bufio"
	"debug/elf"
	"encoding/binary"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	tests := []struct {
		words  []uint16
		pc     uint64
		want   string
		target uint64
	}{
		{[]uint16{0x0000}, 0, "nop", 0},
		{[]uint16{0x011b}, 0, "movw r2, r22", 0},
		{[]uint16{0xe081}, 0, "ldi r24, 0x01", 0},
		{[]uint16{0x2d9f}, 0, "mov r25, r15", 0},
		{[]uint16{0x2399}, 0, "and r25, r25", 0},
		{[]uint16{0xf40a}, 0xe22, "brpl 0xe26", 0xe26},
		{[]uint16{0xf451}, 0xe2a, "brne 0xe40", 0xe40},
		{[]uint16{0x940e, 0x0658}, 0xe2e, "call 0xcb0", 0xcb0},
		{[]uint16{0x940c, 0x0658}, 0, "jmp 0xcb0", 0xcb0},
		{[]uint16{0x1982}, 0, "sub r24, r2", 0},
		{[]uint16{0x0993}, 0, "sbc r25, r3", 0},
		{[]uint16{0x9649}, 0, "adiw r24, 0x19", 0},
		{[]uint16{0x879c}, 0, "std Y+12, r25", 0},
		{[]uint16{0x8398}, 0, "st Y, r25", 0},
		{[]uint16{0x8188}, 0, "ld r24, Y", 0},
		{[]uint16{0x8189}, 0, "ldd r24, Y+1", 0},
		{[]uint16{0x8180}, 0, "ld r24, Z", 0},
		{[]uint16{0x9181}, 0, "ld r24, Z+", 0},
		{[]uint16{0x918d}, 0, "ld r24, X+", 0},
		{[]uint16{0x92cf}, 0, "push r12", 0},
		{[]uint16{0x91cf}, 0, "pop r28", 0},
		{[]uint16{0x9180, 0x0100}, 0, "lds r24, 0x0100", 0},
		{[]uint16{0x9380, 0x0100}, 0, "sts 0x0100, r24", 0},
		{[]uint16{0xb78f}, 0, "in r24, 0x3f", 0},
		{[]uint16{0xbf8f}, 0, "out 0x3f, r24", 0},
		{[]uint16{0x9a9f}, 0, "sbi 0x13, 7", 0},
		{[]uint16{0xfd87}, 0, "sbrc r24, 7", 0},
		{[]uint16{0x9508}, 0, "ret", 0},
		{[]uint16{0x9518}, 0, "reti", 0},
		{[]uint16{0x94f8}, 0, "cli", 0},
		{[]uint16{0x9478}, 0, "sei", 0},
		{[]uint16{0x9588}, 0, "sleep", 0},
		{[]uint16{0x95a8}, 0, "wdr", 0},
		{[]uint16{0x95c8}, 0, "lpm", 0},
		{[]uint16{0x9185}, 0, "lpm r24, Z+", 0},
		{[]uint16{0xcfff}, 0x100, "rjmp 0x100", 0x100},
		{[]uint16{0xc001}, 0x100, "rjmp 0x104", 0x104},
		{[]uint16{0xd002}, 0x100, "rcall 0x106", 0x106},
		{[]uint16{0x9f9c}, 0, "mul r25, r28", 0},
		{[]uint16{0x9586}, 0, "lsr r24", 0},
		{[]uint16{0x9587}, 0, "ror r24", 0},
		{[]uint16{0x9582}, 0, "swap r24", 0},
		{[]uint16{0x9580}, 0, "com r24", 0},
		{[]uint16{0x3083}, 0, "cpi r24, 0x03", 0},
		{[]uint16{0x0510}, 0, "cpc r17, r0", 0},
		{[]uint16{0x9409}, 0, "ijmp", 0},
		{[]uint16{0x9509}, 0, "icall", 0},
		{[]uint16{0x0298}, 0, "muls r25, r24", 0},
		{[]uint16{0x9701}, 0, "sbiw r24, 0x01", 0},
	}
	for _, test := range tests {
		var code []byte
		for _, w := range test.words {
			code = binary.LittleEndian.AppendUint16(code, w)
		}
		inst, err := Decode(code, test.pc)
		if err != nil {
			t.Errorf("%x: %v", test.words, err)
			continue
		}
		if inst.Text != test.want {
			t.Errorf("%x: got %q, want %q", test.words, inst.Text, test.want)
		}
		if test.target != 0 && (!inst.HasTarget || inst.Target != test.target) {
			t.Errorf("%x: Target = %#x, want %#x", test.words, inst.Target, test.target)
		}
	}
}

// TestCompareObjdump decodes the .text of the ELF in AVRASM_ELF and
// compares with GNU avr-objdump (OBJDUMP, default gobjdump on PATH, as
// Homebrew's binutils installs it). A development check; skipped unless
// AVRASM_ELF is set.
func TestCompareObjdump(t *testing.T) {
	path := os.Getenv("AVRASM_ELF")
	if path == "" {
		t.Skip("AVRASM_ELF not set")
	}
	objdump := os.Getenv("OBJDUMP")
	if objdump == "" {
		objdump = "gobjdump"
	}
	out, err := exec.Command(objdump, "-d", path).Output()
	if err != nil {
		t.Skipf("objdump: %v", err)
	}
	ref := map[uint64]string{}
	rx := regexp.MustCompile(`^\s*([0-9a-f]+):\s+((?:[0-9a-f]{2} )+)\s*(\S+)\s*([^;]*)(?:;\s*(0x[0-9a-f]+))?`)
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		m := rx.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		addr, _ := strconv.ParseUint(m[1], 16, 64)
		text := m[3] + " " + strings.TrimSpace(m[4])
		// Relative branches print ".+20 ; 0xe40": take the target.
		if strings.Contains(m[4], ".+") || strings.Contains(m[4], ".-") {
			text = m[3] + " " + m[5]
		}
		ref[addr] = strings.Join(strings.Fields(strings.ToLower(text)), " ")
	}

	f, err := elf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	text := f.Section(".text")
	data, _ := text.Data()
	var total, mismatches int
	for pc := text.Addr; pc < text.Addr+text.Size; {
		inst, err := Decode(data[pc-text.Addr:], pc)
		want, ok := ref[pc]
		if err != nil {
			if ok && !strings.HasPrefix(want, ".word") {
				mismatches++
				if mismatches < 40 {
					t.Errorf("%#x: undecoded, objdump %q", pc, want)
				}
			}
			pc += 2
			continue
		}
		total++
		got := strings.Join(strings.Fields(strings.ToLower(inst.Text)), " ")
		if ok && got != want {
			mismatches++
			if mismatches < 40 {
				t.Errorf("%#x: got %q, objdump %q", pc, got, want)
			}
		}
		pc += uint64(inst.Len)
	}
	t.Logf("%d instructions, %d mismatches", total, mismatches)
}
