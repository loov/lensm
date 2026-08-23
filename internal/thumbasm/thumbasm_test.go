package thumbasm

import (
	"encoding/binary"
	"testing"
)

// TestDecode checks hand-assembled instructions across the encoding
// space: Thumb-1, Thumb-2 immediates and shifts, loads/stores, branches
// with PC-relative targets, and M-profile system registers. The
// reference spellings are llvm-objdump's.
func TestDecode(t *testing.T) {
	tests := []struct {
		words  []uint16
		pc     uint64
		want   string
		target uint64
	}{
		{[]uint16{0x2200}, 0, "movs r2, #0", 0},
		{[]uint16{0x4611}, 0, "mov r1, r2", 0},
		{[]uint16{0x2a0c}, 0, "cmp r2, #0xc", 0},
		{[]uint16{0xd003}, 0x10001b62, "beq 0x10001b6c", 0x10001b6c},
		{[]uint16{0x5883}, 0, "ldr r3, [r0, r2]", 0},
		{[]uint16{0x1859}, 0, "adds r1, r3, r1", 0},
		{[]uint16{0x1d12}, 0, "adds r2, r2, #4", 0},
		{[]uint16{0xe7f9}, 0x10001b6a, "b 0x10001b60", 0x10001b60},
		{[]uint16{0x4770}, 0, "bx lr", 0},
		{[]uint16{0xb5f0}, 0, "push {r4, r5, r6, r7, lr}", 0},
		{[]uint16{0xbd10}, 0, "pop {r4, pc}", 0},
		{[]uint16{0xb095}, 0, "sub sp, #0x54", 0},
		{[]uint16{0xa812}, 0, "add r0, sp, #0x48", 0},
		{[]uint16{0x9104}, 0, "str r1, [sp, #0x10]", 0},
		{[]uint16{0x4276}, 0, "rsbs r6, r6, #0", 0},
		{[]uint16{0x0052}, 0, "lsls r2, r2, #1", 0},
		{[]uint16{0x0840}, 0, "lsrs r0, r0, #1", 0},
		{[]uint16{0x4348}, 0, "muls r0, r1, r0", 0},
		{[]uint16{0xb2c0}, 0, "uxtb r0, r0", 0},
		{[]uint16{0x4a03}, 0x10de, "ldr r2, 0x10ec", 0x10ec},
		{[]uint16{0xb100}, 0x1000, "cbz r0, 0x1004", 0x1004},
		{[]uint16{0xbf00}, 0, "nop", 0},
		{[]uint16{0xdf00}, 0, "svc #0", 0},
		// Thumb-2.
		{[]uint16{0xf000, 0xf865}, 0x10001a8e, "bl 0x10001b5c", 0x10001b5c},
		{[]uint16{0xf7ff, 0xfb23}, 0x10001a9a, "bl 0x100010e4", 0x100010e4},
		{[]uint16{0xf04f, 0x0001}, 0, "mov.w r0, #1", 0},
		{[]uint16{0xf44f, 0x7080}, 0, "mov.w r0, #0x100", 0},
		{[]uint16{0xf240, 0x2039}, 0, "movw r0, #0x239", 0},
		{[]uint16{0xf2c0, 0x0001}, 0, "movt r0, #1", 0},
		{[]uint16{0xf101, 0x0004}, 0, "add.w r0, r1, #4", 0},
		{[]uint16{0xf201, 0x4015}, 0, "addw r0, r1, #0x415", 0},
		{[]uint16{0xea40, 0x2206}, 0, "orr.w r2, r0, r6, lsl #8", 0},
		{[]uint16{0xea5f, 0x0030}, 0, "rrxs r0, r0", 0},
		{[]uint16{0xebad, 0x0d03}, 0, "sub.w sp, sp, r3", 0},
		{[]uint16{0xf850, 0x3b04}, 0, "ldr r3, [r0], #4", 0},
		{[]uint16{0xf85d, 0x0d04}, 0, "ldr r0, [sp, #-4]!", 0},
		{[]uint16{0xf8d0, 0x1048}, 0, "ldr.w r1, [r0, #0x48]", 0},
		{[]uint16{0xe9d0, 0x2300}, 0, "ldrd r2, r3, [r0]", 0},
		{[]uint16{0xe8bd, 0x8ff0}, 0, "pop.w {r4, r5, r6, r7, r8, r9, r10, r11, pc}", 0},
		{[]uint16{0xe92d, 0x4ff0}, 0, "push.w {r4, r5, r6, r7, r8, r9, r10, r11, lr}", 0},
		{[]uint16{0xfb90, 0xf0f1}, 0, "sdiv r0, r0, r1", 0},
		{[]uint16{0xfba0, 0x2301}, 0, "umull r2, r3, r0, r1", 0},
		{[]uint16{0xf3c0, 0x0207}, 0, "ubfx r2, r0, #0, #8", 0},
		{[]uint16{0xf36f, 0x0007}, 0, "bfc r0, #0, #8", 0},
		{[]uint16{0xfa90, 0xf0a0}, 0, "rbit r0, r0", 0},
		{[]uint16{0xf3bf, 0x8f5f}, 0, "dmb sy", 0},
		{[]uint16{0xf3bf, 0x8f6f}, 0, "isb sy", 0},
		{[]uint16{0xf3ef, 0x8010}, 0, "mrs r0, primask", 0},
		{[]uint16{0xf380, 0x8810}, 0, "msr primask, r0", 0},
		{[]uint16{0xf380, 0x8800}, 0, "msr apsr_nzcvq, r0", 0},
		{[]uint16{0xb672}, 0, "cpsid i", 0},
		{[]uint16{0xe8d0, 0xf001}, 0, "tbb [r0, r1]", 0},
		{[]uint16{0xe8d0, 0xf011}, 0, "tbh [r0, r1, lsl #1]", 0},
		{[]uint16{0xe850, 0x1f00}, 0, "ldrex r1, [r0]", 0},
		{[]uint16{0xe840, 0x1200}, 0, "strex r2, r1, [r0]", 0},
		{[]uint16{0xf000, 0x8003}, 0x1000, "beq.w 0x100a", 0x100a},
		// VFP (Cortex-M4F/M7/M33 floating point).
		{[]uint16{0xecb0, 0x0a02}, 0, "vldmia r0!, {s0-s1}", 0},
		{[]uint16{0xec90, 0x0b04}, 0, "vldmia r0, {d0-d1}", 0},
		{[]uint16{0xed2d, 0x8b04}, 0, "vpush {d8-d9}", 0},
		{[]uint16{0xecbd, 0x8b04}, 0, "vpop {d8-d9}", 0},
		{[]uint16{0xed90, 0x0a01}, 0, "vldr s0, [r0, #4]", 0},
		{[]uint16{0xed80, 0x1b02}, 0, "vstr d1, [r0, #8]", 0},
		{[]uint16{0xeeb0, 0x0a40}, 0, "vmov.f32 s0, s0", 0},
		{[]uint16{0xeeb7, 0x0ac0}, 0, "vcvt.f64.f32 d0, s0", 0},
		{[]uint16{0xeeb8, 0x0a40}, 0, "vcvt.f32.u32 s0, s0", 0},
		{[]uint16{0xeebd, 0x0ac0}, 0, "vcvt.s32.f32 s0, s0", 0},
		{[]uint16{0xeebc, 0x0bc0}, 0, "vcvt.u32.f64 s0, d0", 0},
		{[]uint16{0xeeb0, 0x0a00}, 0, "vmov.f32 s0, #2.0", 0},
		{[]uint16{0xeeb7, 0x0b00}, 0, "vmov.f64 d0, #1.0", 0},
		{[]uint16{0xeef1, 0xfa10}, 0, "vmrs apsr_nzcv, fpscr", 0},
		{[]uint16{0xeee1, 0x0a10}, 0, "vmsr fpscr, r0", 0},
		{[]uint16{0xeeb4, 0x0a40}, 0, "vcmp.f32 s0, s0", 0},
		{[]uint16{0xeeb5, 0x0a40}, 0, "vcmp.f32 s0, #0.0", 0},
		{[]uint16{0xfe80, 0x0a00}, 0, "vmaxnm.f32 s0, s0, s0", 0},
		{[]uint16{0xeea0, 0x0a00}, 0, "vfma.f32 s0, s0, s0", 0},
		{[]uint16{0xee00, 0x0a10}, 0, "vmov s0, r0", 0},
		{[]uint16{0xee10, 0x0a10}, 0, "vmov r0, s0", 0},
		{[]uint16{0xec41, 0x0b10}, 0, "vmov d0, r0, r1", 0},
		{[]uint16{0xec51, 0x0b10}, 0, "vmov r0, r1, d0", 0},
		{[]uint16{0xee30, 0x0b40}, 0, "vsub.f64 d0, d0, d0", 0},
		{[]uint16{0xeeb1, 0x0bc0}, 0, "vsqrt.f64 d0, d0", 0},
		{[]uint16{0xee20, 0x0a00}, 0, "vmul.f32 s0, s0, s0", 0},
		{[]uint16{0xee80, 0x0a00}, 0, "vdiv.f32 s0, s0, s0", 0},
		{[]uint16{0xfe00, 0x0a00}, 0, "vseleq.f32 s0, s0, s0", 0},
	}
	for _, test := range tests {
		code := make([]byte, 0, 4)
		for _, w := range test.words {
			code = binary.LittleEndian.AppendUint16(code, w)
		}
		var d Decoder
		inst, err := d.Decode(code, test.pc)
		if err != nil {
			t.Errorf("%x: %v", test.words, err)
			continue
		}
		if inst.Text != test.want {
			t.Errorf("%x: got %q, want %q", test.words, inst.Text, test.want)
		}
		if inst.Len != 2*len(test.words) {
			t.Errorf("%x: Len = %d, want %d", test.words, inst.Len, 2*len(test.words))
		}
		if test.target != 0 && (!inst.HasTarget || inst.Target != test.target) {
			t.Errorf("%x: Target = %#x (%v), want %#x", test.words, inst.Target, inst.HasTarget, test.target)
		}
	}
}

// TestITBlock checks that the IT instruction makes the following
// instructions conditional and drops the S suffix of 16-bit forms.
func TestITBlock(t *testing.T) {
	words := []uint16{
		0xbf1c, // itt ne
		0x1840, // addne r0, r0, r1
		0x4608, // movne r0, r1
		0x1840, // adds r0, r0, r1 (block ended)
		0xbfac, // ite ge
		0x1840, // addge
		0x1840, // addlt
	}
	want := []string{"itt ne", "addne r0, r0, r1", "movne r0, r1", "adds r0, r0, r1", "ite ge", "addge r0, r0, r1", "addlt r0, r0, r1"}
	var d Decoder
	for i, w := range words {
		inst, err := d.Decode(binary.LittleEndian.AppendUint16(nil, w), uint64(2*i))
		if err != nil {
			t.Fatalf("%#x: %v", w, err)
		}
		if inst.Text != want[i] {
			t.Errorf("%#x: got %q, want %q", w, inst.Text, want[i])
		}
	}
}

func TestUndecodable(t *testing.T) {
	var d Decoder
	if _, err := d.Decode([]byte{0xff}, 0); err == nil {
		t.Error("1-byte input decoded")
	}
	if _, err := d.Decode([]byte{0x00, 0xf0}, 0); err == nil {
		t.Error("truncated 32-bit instruction decoded")
	}
	// 0xde00 is the permanently undefined space (UDF); 0xf7f0 0xa000 is
	// the UDF.W encoding. Both should still decode as UDF.
	inst, err := d.Decode([]byte{0x00, 0xde}, 0)
	if err != nil || inst.Text != "udf #0" {
		t.Errorf("udf: %q, %v", inst.Text, err)
	}
}

func TestT32ExpandImm(t *testing.T) {
	tests := map[uint32]uint32{
		0x000: 0, 0x0ab: 0xab, 0x1ab: 0x00ab00ab, 0x2ab: 0xab00ab00, 0x3ab: 0xabababab,
		0x400: 0x80000000, 0x4ff: 0x7f800000, 0x7ff: 0x01fe0000, 0xf80: 0x100,
	}
	for in, want := range tests {
		if got, _ := t32ExpandImmC(in, false); got != want {
			t.Errorf("T32ExpandImm(%#x) = %#x, want %#x", in, got, want)
		}
	}
}
