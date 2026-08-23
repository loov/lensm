// Package objfile loads executables into a format-independent
// representation: the architecture, the functions with their machine
// code, a symbol lookup, and the Go pclntab for PC to line mapping.
//
// It is a trimmed port of github.com/loov/ixdiff/internal/objfile and
// replaces vendored cmd/internal packages: only stdlib debug/* is used.
package objfile

import (
	"bytes"
	"cmp"
	"debug/dwarf"
	"debug/elf"
	"debug/gosym"
	"debug/macho"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Binary is a loaded executable.
type Binary struct {
	Arch string // GOARCH name, e.g. "amd64"
	// Funcs are the functions inside the text section, sorted by address.
	Funcs []*Func

	text     []byte
	textAddr uint64
	// byteOrder of instruction words; only ppc64 has a big-endian variant.
	byteOrder binary.ByteOrder
	// syms are all symbols sorted by address, used to resolve addresses to names.
	syms []sym
	pcln *gosym.Table
	// lines is the DWARF line table, used when there is no pclntab:
	// binaries from clang, gcc and anything else that isn't Go.
	lines *Lines
}

// Func is a single function inside a binary.
type Func struct {
	Name string
	Addr uint64
	Size uint64

	bin *Binary
}

// Code returns the machine code of the function, or nil when it lies
// outside the text section.
func (f *Func) Code() []byte {
	return sectionSlice(f.bin.text, f.Addr-f.bin.textAddr, f.Size)
}

// PCToLine maps a pc to its source location using the Go pclntab;
// zero values when unknown.
func (b *Binary) PCToLine(pc uint64) (file string, line int) {
	if b.pcln == nil {
		return b.lines.At(pc)
	}
	file, line, _ = b.pcln.PCToLine(pc)
	return file, line
}

// Lookup resolves addr to the name and base of the symbol containing it,
// matching the contract of the x/arch GoSyntax symname functions.
func (b *Binary) Lookup(addr uint64) (name string, base uint64) {
	// i is the first symbol at or after addr; unless addr hits a symbol
	// start exactly, the containing one is the symbol before it.
	i, found := slices.BinarySearchFunc(b.syms, addr, func(s sym, a uint64) int {
		return cmp.Compare(s.addr, a)
	})
	if !found {
		i--
	}
	if i >= 0 {
		if s := b.syms[i]; addr < s.addr+s.size {
			return s.name, s.addr
		}
	}
	return "", 0
}

type sym struct {
	name string
	addr uint64
	size uint64 // zero when the format does not record sizes (Mach-O, PE)
}

// Open reads and parses the binary at path, detecting ELF, Mach-O and PE
// from the magic bytes.
func Open(path string) (*Binary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 4 {
		return nil, fmt.Errorf("%q: too short to be a binary", path)
	}
	r := bytes.NewReader(data)
	var bin *Binary
	switch magic := string(data[:4]); {
	case magic == elf.ELFMAG:
		bin, err = openELF(r, data)
	case magic == "\xcf\xfa\xed\xfe" || magic == "\xfe\xed\xfa\xcf":
		bin, err = openMachO(r, data)
	case magic[0] == 'M' && magic[1] == 'Z':
		bin, err = openPE(r, data)
	default:
		err = fmt.Errorf("unsupported binary format")
	}
	if err != nil {
		return nil, fmt.Errorf("%q: %w", path, err)
	}
	if bin.pcln == nil && bin.lines == nil {
		bin.loadCompanionDWARF(path)
	}
	bin.finish()
	return bin, nil
}

// loadCompanionDWARF reads the line table from a dSYM bundle beside the
// binary. On macOS the linker leaves debug info in the object files and
// dsymutil collects it there, so an executable built with -g carries no
// DWARF of its own.
func (b *Binary) loadCompanionDWARF(path string) {
	dsym := path + ".dSYM/Contents/Resources/DWARF/" + filepath.Base(path)
	file, err := macho.Open(dsym)
	if err != nil {
		return
	}
	defer file.Close()
	b.loadDWARF(file.DWARF)
}

// sectionSlice returns data[off:off+size], or nil when the range is
// invalid; overflow-safe for corrupt headers near 2^64.
func sectionSlice(data []byte, off, size uint64) []byte {
	if off > uint64(len(data)) || size > uint64(len(data))-off {
		return nil
	}
	return data[off : off+size]
}

// sectionMarkers are linker boundary symbols; they are not functions and
// would shadow the first real symbol in Lookup.
var sectionMarkers = map[string]bool{
	"runtime.text": true, "text": true, "_text": true,
	"runtime.etext": true, "etext": true, "_etext": true,
}

func (b *Binary) addSym(name string, addr, size uint64) {
	if sectionMarkers[name] || addr == 0 {
		return
	}
	b.syms = append(b.syms, sym{name: name, addr: addr, size: size})
}

// loadPclntab parses a Go runtime pclntab; it gives exact function
// ranges even for stripped binaries. Best-effort: on failure the
// symbol-table functions remain.
func (b *Binary) loadPclntab(pclntab []byte) {
	b.pcln = LineTable(pclntab, b.textAddr)
}

// LineTable parses a Go pclntab whose text starts at textAddr; nil when
// the data isn't a table this Go version understands.
func LineTable(pclntab []byte, textAddr uint64) *gosym.Table {
	if len(pclntab) == 0 {
		return nil
	}
	tab, err := gosym.NewTable(nil, gosym.NewLineTable(pclntab, textAddr))
	if err != nil {
		return nil
	}
	return tab
}

// FindLineTable scans data — a memory image or data section — for an
// embedded pclntab and parses it. nil when there is none.
func FindLineTable(data []byte, textAddr uint64) *gosym.Table {
	return LineTable(findPclntab(data), textAddr)
}

// FindWasmLineTable scans a reconstructed linear-memory image for a Go
// pclntab and returns a table addressed by wasm PCs: function index plus
// funcValueOffset, shifted left 16, with the resume-point block in the
// low bits. That is the PC the compiler's line deltas are relative to,
// but the table stores function entries unshifted, so gosym would place
// every block after the first inside the following function. Scaling the
// stored entries — and only those — puts them back in PC space, leaving
// the deltas to count blocks. nil when there is no usable table.
func FindWasmLineTable(image []byte) *gosym.Table {
	tab := findPclntab(image)
	if tab == nil {
		return nil
	}
	tab = bytes.Clone(tab)
	if !scaleWasmEntries(tab) {
		return nil
	}
	return LineTable(tab, 0)
}

// scaleWasmEntries shifts every function entry in a pclntab left by 16,
// in the function table and in each _func. It reports whether the layout
// was understood and every entry fitted.
func scaleWasmEntries(tab []byte) bool {
	if len(tab) < 8 {
		return false
	}
	ptrSize := int(tab[7])
	if ptrSize != 4 && ptrSize != 8 {
		return false
	}
	// Header: magic, pad, quantum, ptrSize, then ptr-sized nfunc, nfiles,
	// textStart and the offsets of the name, cu, file, pc and func tables.
	field := func(i int) (uint64, bool) {
		off := 8 + i*ptrSize
		if off+ptrSize > len(tab) {
			return 0, false
		}
		if ptrSize == 8 {
			return binary.LittleEndian.Uint64(tab[off:]), true
		}
		return uint64(binary.LittleEndian.Uint32(tab[off:])), true
	}
	nfunc, ok1 := field(0)
	funcTab, ok2 := field(7)
	if !ok1 || !ok2 || nfunc > uint64(len(tab)) {
		return false
	}
	// The function table is nfunc (entryOff, funcOff) uint32 pairs plus a
	// final entryOff marking the end of the text.
	shift := func(off uint64) bool {
		if off+4 > uint64(len(tab)) {
			return false
		}
		v := binary.LittleEndian.Uint32(tab[off:])
		if v >= 1<<16 { // already in PC space, or too many functions
			return false
		}
		binary.LittleEndian.PutUint32(tab[off:], v<<16)
		return true
	}
	for i := uint64(0); i <= nfunc; i++ {
		if !shift(funcTab + i*8) {
			return false
		}
		if i == nfunc {
			break
		}
		funcOff := uint64(binary.LittleEndian.Uint32(tab[funcTab+i*8+4:]))
		// _func starts with its own entryOff.
		if !shift(funcTab + funcOff) {
			return false
		}
	}
	return true
}

// finish sorts symbols, infers missing sizes as the distance to the next
// symbol, and collects the functions: pclntab entries first (exact sizes,
// present even when stripped), then any remaining text symbols.
func (b *Binary) finish() {
	if b.pcln != nil {
		for _, fn := range b.pcln.Funcs {
			b.addSym(fn.Name, fn.Entry, fn.End-fn.Entry)
		}
	}
	// Sort sized symbols last at equal addresses, so Lookup's "last
	// symbol at or before addr" prefers the one with a real extent.
	slices.SortStableFunc(b.syms, func(x, y sym) int {
		return cmp.Or(cmp.Compare(x.addr, y.addr), cmp.Compare(x.size, y.size))
	})
	textEnd := b.textAddr + uint64(len(b.text))
	for i := range b.syms {
		s := &b.syms[i]
		if s.size != 0 {
			continue
		}
		for _, next := range b.syms[i+1:] {
			if next.addr != s.addr {
				s.size = next.addr - s.addr
				break
			}
		}
		if s.size == 0 && s.addr < textEnd {
			s.size = textEnd - s.addr
		}
	}

	seen := map[string]bool{}
	add := func(name string, addr, size uint64) {
		if addr < b.textAddr || addr >= textEnd || seen[name] {
			return
		}
		seen[name] = true
		b.Funcs = append(b.Funcs, &Func{Name: name, Addr: addr, Size: size, bin: b})
	}
	if b.pcln != nil {
		for _, fn := range b.pcln.Funcs {
			add(fn.Name, fn.Entry, fn.End-fn.Entry)
		}
	}
	for _, s := range b.syms {
		add(s.name, s.addr, s.size)
	}
	slices.SortFunc(b.Funcs, func(x, y *Func) int { return cmp.Compare(x.Addr, y.Addr) })
}

// loadDWARF reads the line table a non-Go compiler left in the binary.
// Best-effort: most binaries carry none, and a Go binary has its pclntab
// instead.
func (b *Binary) loadDWARF(open func() (*dwarf.Data, error)) {
	if b.lines != nil {
		return
	}
	data, err := open()
	if err != nil {
		return
	}
	b.lines = LinesFromDWARF(data, 0)
}

// pclntabMagics are the little-endian header magics of pclntab versions.
var pclntabMagics = [][]byte{
	{0xf1, 0xff, 0xff, 0xff, 0x00, 0x00}, // Go 1.20+
	{0xf0, 0xff, 0xff, 0xff, 0x00, 0x00}, // Go 1.18–1.19
	{0xfa, 0xff, 0xff, 0xff, 0x00, 0x00}, // Go 1.16–1.17
}

// findPclntab locates a pclntab inside data by its header: a version
// magic followed by a plausible pc quantum and pointer size. It is the
// fallback for binaries without a dedicated section: PE always, and ELF
// when the system linker merged it into another data section.
func findPclntab(data []byte) []byte {
	for _, magic := range pclntabMagics {
		for off := 0; ; off += len(magic) {
			i := bytes.Index(data[off:], magic)
			if i < 0 {
				break
			}
			off += i
			if off+8 <= len(data) {
				quantum, ptrsize := data[off+6], data[off+7]
				if (quantum == 1 || quantum == 4) && (ptrsize == 4 || ptrsize == 8) {
					return data[off:]
				}
			}
		}
	}
	return nil
}

func openELF(r *bytes.Reader, data []byte) (*Binary, error) {
	ef, err := elf.NewFile(r)
	if err != nil {
		return nil, err
	}
	bin := &Binary{byteOrder: binary.LittleEndian}
	switch ef.Machine {
	case elf.EM_X86_64:
		bin.Arch = "amd64"
	case elf.EM_AARCH64:
		bin.Arch = "arm64"
	case elf.EM_386:
		bin.Arch = "386"
	case elf.EM_ARM:
		bin.Arch = "arm"
	case elf.EM_S390:
		bin.Arch = "s390x"
	case elf.EM_PPC64:
		bin.Arch = "ppc64le"
		if ef.ByteOrder == binary.BigEndian {
			bin.Arch, bin.byteOrder = "ppc64", binary.BigEndian
		}
	case elf.EM_RISCV:
		bin.Arch = "riscv64"
	case elf.EM_LOONGARCH:
		bin.Arch = "loong64"
	default:
		return nil, fmt.Errorf("unsupported ELF machine %v", ef.Machine)
	}
	if ef.Class != elf.ELFCLASS64 && bin.Arch != "386" && bin.Arch != "arm" {
		return nil, fmt.Errorf("unsupported 32-bit ELF for %s", bin.Arch)
	}

	text := ef.Section(".text")
	if text == nil {
		return nil, fmt.Errorf("no .text section")
	}
	bin.text = sectionSlice(data, text.Offset, text.FileSize)
	if bin.text == nil || text.Flags&elf.SHF_COMPRESSED != 0 {
		return nil, fmt.Errorf("unreadable .text section")
	}
	bin.textAddr = text.Addr

	syms, err := ef.Symbols()
	if err != nil && err != elf.ErrNoSymbols {
		return nil, fmt.Errorf("reading symbols: %w", err)
	}
	for _, s := range syms {
		switch elf.ST_TYPE(s.Info) {
		case elf.STT_FUNC, elf.STT_OBJECT:
			bin.addSym(s.Name, s.Value, s.Size)
		}
	}

	bin.loadDWARF(ef.DWARF)
	if sec := ef.Section(".gopclntab"); sec != nil {
		if tab, err := sec.Data(); err == nil {
			bin.loadPclntab(tab)
		}
	} else {
		for _, sec := range ef.Sections {
			if sec.Type == elf.SHT_PROGBITS && sec.Flags&elf.SHF_ALLOC != 0 && sec.Flags&elf.SHF_EXECINSTR == 0 {
				if d, err := sec.Data(); err == nil && findPclntab(d) != nil {
					bin.loadPclntab(findPclntab(d))
					break
				}
			}
		}
	}
	return bin, nil
}

func openMachO(r *bytes.Reader, data []byte) (*Binary, error) {
	mf, err := macho.NewFile(r)
	if err != nil {
		return nil, err
	}
	bin := &Binary{byteOrder: binary.LittleEndian}
	switch mf.Cpu {
	case macho.CpuAmd64:
		bin.Arch = "amd64"
	case macho.CpuArm64:
		bin.Arch = "arm64"
	default:
		return nil, fmt.Errorf("unsupported Mach-O cpu %v", mf.Cpu)
	}

	text := mf.Section("__text")
	if text == nil {
		return nil, fmt.Errorf("no __text section")
	}
	bin.text = sectionSlice(data, uint64(text.Offset), text.Size)
	if bin.text == nil {
		return nil, fmt.Errorf("unreadable __text section")
	}
	bin.textAddr = text.Addr

	if mf.Symtab != nil {
		for _, s := range mf.Symtab.Syms {
			// 0xe0 masks the N_STAB debugging bits; such entries
			// describe source info, not symbols.
			if s.Type&0xe0 != 0 {
				continue
			}
			bin.addSym(strings.TrimPrefix(s.Name, "_"), s.Value, 0)
		}
	}
	if sec := mf.Section("__gopclntab"); sec != nil {
		if tab, err := sec.Data(); err == nil {
			bin.loadPclntab(tab)
		}
	}
	bin.loadDWARF(mf.DWARF)
	return bin, nil
}

func openPE(r *bytes.Reader, data []byte) (*Binary, error) {
	pf, err := pe.NewFile(r)
	if err != nil {
		return nil, err
	}
	bin := &Binary{byteOrder: binary.LittleEndian}
	switch pf.Machine {
	case pe.IMAGE_FILE_MACHINE_AMD64:
		bin.Arch = "amd64"
	case pe.IMAGE_FILE_MACHINE_ARM64:
		bin.Arch = "arm64"
	case pe.IMAGE_FILE_MACHINE_I386:
		bin.Arch = "386"
	default:
		return nil, fmt.Errorf("unsupported PE machine %#x", pf.Machine)
	}

	var imageBase uint64
	switch hdr := pf.OptionalHeader.(type) {
	case *pe.OptionalHeader64:
		imageBase = hdr.ImageBase
	case *pe.OptionalHeader32:
		imageBase = uint64(hdr.ImageBase)
	default:
		return nil, fmt.Errorf("missing PE optional header")
	}

	text := pf.Section(".text")
	if text == nil {
		return nil, fmt.Errorf("no .text section")
	}
	// The on-disk section can be padded past its virtual size.
	bin.text = sectionSlice(data, uint64(text.Offset), min(uint64(text.Size), uint64(text.VirtualSize)))
	if bin.text == nil {
		return nil, fmt.Errorf("unreadable .text section")
	}
	bin.textAddr = imageBase + uint64(text.VirtualAddress)

	// COFF symbol values are offsets within their 1-based section.
	for _, s := range pf.Symbols {
		if s.SectionNumber <= 0 || int(s.SectionNumber) > len(pf.Sections) {
			continue
		}
		sec := pf.Sections[s.SectionNumber-1]
		bin.addSym(s.Name, imageBase+uint64(sec.VirtualAddress)+uint64(s.Value), 0)
	}

	bin.loadDWARF(pf.DWARF)
	// PE has no pclntab section; scan the data sections for its header.
	for _, name := range []string{".rdata", ".data"} {
		if sec := pf.Section(name); sec != nil {
			if d, err := sec.Data(); err == nil && findPclntab(d) != nil {
				bin.loadPclntab(findPclntab(d))
				break
			}
		}
	}
	return bin, nil
}
