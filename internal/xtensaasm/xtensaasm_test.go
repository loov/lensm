package xtensaasm

import (
	"bufio"
	"debug/elf"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestDecode(t *testing.T) {
	tests := []struct {
		hex    string // objdump's big-endian spelling of the instruction word
		pc     uint64
		want   string
		target uint64
	}{
		{"004136", 0, "entry a1, 32", 0},
		{"090c", 0, "movi.n a9, 0", 0},
		{"098d", 0, "mov.n a8, a9", 0},
		{"0aa926", 0x40080997, "beqi a9, 12, 0x400809a5", 0x400809a5},
		{"a29a", 0, "add.n a10, a2, a9", 0},
		{"0aa8", 0, "l32i.n a10, a10, 0", 0},
		{"994b", 0, "addi.n a9, a9, 4", 0},
		{"f4a966", 0x400809a2, "bnei a9, 12, 0x4008099a", 0x4008099a},
		{"f01d", 0, "retw.n", 0},
		{"f00d", 0, "ret.n", 0},
		{"000080", 0, "ret", 0},
		{"0020c0", 0, "memw", 0},
		{"002080", 0, "excw", 0},
		{"f4caa2", 0, "addi a10, a10, -12", 0},
		{"ee0b", 0, "addi.n a14, a14, -1", 0},
		{"85a730", 0, "extui a10, a3, 23, 9", 0},
		{"115880", 0, "slli a5, a8, 8", 0},
		{"213600", 0, "srai a3, a0, 6", 0},
		{"41f140", 0, "srli a15, a4, 1", 0},
		{"01a022", 0, "movi a2, 1", 0},
		{"060c", 0, "movi.n a6, 0", 0},
		{"012142", 0, "l32i a4, a1, 4", 0},
		{"145152", 0, "s16i a5, a1, 40", 0},
		{"0048b2", 0, "s8i a11, a8, 0", 0},
		{"097800", 0, "l32e a0, a8, -36", 0},
		{"fffc31", 0x4008000f, "l32r a3, 0x40080000", 0x40080000},
		{"03e620", 0, "rsr.ps a2", 0},
		{"13e620", 0, "wsr.ps a2", 0},
		{"034820", 0, "rsr.windowbase a2", 0},
		{"006340", 0, "rsil a4, 3", 0},
		{"007000", 0, "waiti 0", 0},
		{"004000", 0, "break 0, 0", 0},
		{"0008e0", 0, "callx8 a8", 0},
		{"000cc0", 0, "callx0 a12", 0},
		{"011dd5", 0x40080048, "call4 0x40081228", 0x40081228},
		{"faf045", 0x4008084c, "call0 0x4007b754", 0x4007b754},
		{"044b46", 0x4008004b, "j 0x4008117c", 0x4008117c},
		{"00f516", 0x40080225 - 0xf - 4, "beqz a5, 0x40080225", 0x40080225},
		{"f0bc", 0x400804b0, "beqz.n a0, 0x400804f3", 0x400804f3},
		{"fa18a7", 0x4008020d + 6 - 4, "beq a8, a10, 0x4008020d", 0x4008020d},
		{"f243f6", 0x40080b51 + 14 - 4, "bgeui a3, 4, 0x40080b51", 0x40080b51},
		{"020076", 0x40080bc3 - 2 - 4, "bf b0, 0x40080bc3", 0x40080bc3},
		{"008000", 0, "any4 b0, b0:b1:b2:b3", 0},
		{"0a8990", 0, "add.s f8, f9, f9", 0},
		{"cabc00", 0, "float.s f11, a12, 0", 0},
		{"9adc00", 0, "trunc.s a13, f12, 0", 0},
		{"030193", 0, "lsi f9, a1, 12", 0},
		{"0804e0", 0, "lsx f0, a4, a14", 0},
		{"faa840", 0, "rfr a10, f8", 0},
		{"fa9250", 0, "wfr f9, a2", 0},
		{"2b0890", 0, "oeq.s b0, f8, f9", 0},
		{"faf000", 0, "mov.s f15, f0", 0},
		{"d2a890", 0, "quos a10, a8, a9", 0},
		{"821600", 0, "mull a1, a6, a0", 0},
		{"40fc40", 0, "nsau a4, a12", 0},
		{"001c00", 0, "movsp a0, a12", 0},
		{"400900", 0, "ssr a9", 0},
		{"814430", 0, "src a4, a4, a3", 0},
		{"537570", 0, "max a7, a5, a7", 0},
		{"f00000", 0, "subx8 a0, a0, a0", 0},
		{"000000", 0, "ill", 0},
	}
	for _, test := range tests {
		code := make([]byte, len(test.hex)/2)
		for i := range code {
			v, _ := strconv.ParseUint(test.hex[len(test.hex)-2*i-2:len(test.hex)-2*i], 16, 8)
			code[i] = byte(v)
		}
		inst, err := Decode(code, test.pc)
		if err != nil {
			t.Errorf("%s: %v", test.hex, err)
			continue
		}
		if inst.Text != test.want {
			t.Errorf("%s: got %q, want %q", test.hex, inst.Text, test.want)
		}
		if inst.Len != len(code) {
			t.Errorf("%s: Len = %d, want %d", test.hex, inst.Len, len(code))
		}
		if test.target != 0 && (!inst.HasTarget || inst.Target != test.target) {
			t.Errorf("%s: Target = %#x, want %#x", test.hex, inst.Target, test.target)
		}
	}
}

// TestCompareObjdump compares with GNU xtensa objdump on the ELF in
// XTENSAASM_ELF (OBJDUMP, default gobjdump). Development check.
func TestCompareObjdump(t *testing.T) {
	path := os.Getenv("XTENSAASM_ELF")
	if path == "" {
		t.Skip("XTENSAASM_ELF not set")
	}
	objdump := os.Getenv("OBJDUMP")
	if objdump == "" {
		objdump = "gobjdump"
	}
	out, err := exec.Command(objdump, "-d", path).Output()
	if err != nil {
		t.Skipf("objdump: %v", err)
	}
	f, err := elf.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	text := f.Section(".text")
	data, _ := text.Data()

	rx := regexp.MustCompile(`^\s*([0-9a-f]+):\s+([0-9a-f]+)\s+(\S+)\s*(.*)$`)
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	var total, mismatches int
	for sc.Scan() {
		m := rx.FindStringSubmatch(sc.Text())
		// objdump spells undefined words (literal pools inside .text) as
		// "excw" and FLIX bundles as "{ ... }"; neither is code here.
		if m == nil || m[3] == ".byte" || m[3] == "{" || m[3] == "}" || m[3] == "excw" || strings.HasPrefix(m[3], "mul") && strings.Contains(m[3], ".") {
			continue // MAC16 is not implemented either
		}
		pc, _ := strconv.ParseUint(m[1], 16, 64)
		if pc < text.Addr || pc >= text.Addr+text.Size {
			continue
		}
		ops := m[4]
		if i := strings.Index(ops, " <"); i >= 0 {
			ops = ops[:i]
		}
		want := strings.TrimSpace(m[3] + " " + ops)
		// Targets print as bare hex; we write 0x.
		want = regexp.MustCompile(`\b([0-9a-f]{6,8})$`).ReplaceAllString(want, "0x$1")
		total++
		inst, err := Decode(data[pc-text.Addr:], pc)
		if err != nil {
			mismatches++
			if mismatches < 40 {
				t.Errorf("%#x: undecoded (%v), objdump %q (%s)", pc, err, want, m[2])
			}
			continue
		}
		if inst.Text != want || inst.Len != len(m[2])/2 {
			mismatches++
			if mismatches < 40 {
				t.Errorf("%#x: got %q (%d), objdump %q (%s)", pc, inst.Text, inst.Len, want, m[2])
			}
		}
	}
	t.Logf("%d instructions, %d mismatches", total, mismatches)
}
