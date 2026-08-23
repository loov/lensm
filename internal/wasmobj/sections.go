package wasmobj

// The wasm binary format, only as far as this package needs it: DWARF
// addresses are byte offsets into the module, so mapping instructions to
// source needs each function body's position in the file, which the
// decoded IR does not carry.

// codeSectionID is the section holding function bodies.
const codeSectionID = 10

// codeRange is the position of one function body in the file: its bytes
// after the entry's size prefix, so it starts with the locals vector.
type codeRange struct{ start, size uint64 }

// codeSection is where a module keeps its function bodies: the file
// offset of the section payload, which DWARF code addresses are relative
// to, and the body of each entry in function order. A malformed module
// yields the entries read so far.
func codeSection(data []byte) (start uint64, bodies []codeRange) {
	c := &cursor{data: data, pos: 8} // past the magic and version
	for c.pos < len(data) && !c.fail {
		id := c.byte()
		size := c.uint()
		base := c.pos
		payload := c.bytes(size)
		if c.fail {
			return start, bodies
		}
		if id != codeSectionID {
			continue
		}
		start = uint64(base)
		sec := &cursor{data: payload}
		for range sec.uint() {
			n := sec.uint()
			bodyStart := base + sec.pos
			sec.bytes(n)
			if sec.fail {
				// Checked inside the loop: the declared entry count comes
				// from the file, and reads past the end are no-ops, so an
				// unchecked loop would spin on a huge count.
				return start, bodies
			}
			bodies = append(bodies, codeRange{uint64(bodyStart), n})
		}
	}
	return start, bodies
}

// localsSize returns the byte length of the locals vector a function body
// starts with, or -1 when it is malformed.
func localsSize(body []byte) int {
	c := &cursor{data: body}
	for range c.uint() {
		c.uint() // count
		c.byte() // value type
	}
	if c.fail {
		return -1
	}
	return c.pos
}

// cursor reads the LEB128-encoded structure of a wasm module. Reads past
// the end set fail and return zeros, so callers check fail once instead
// of handling an error per read.
type cursor struct {
	data []byte
	pos  int
	fail bool
}

func (c *cursor) byte() byte {
	if c.pos >= len(c.data) {
		c.fail = true
		return 0
	}
	b := c.data[c.pos]
	c.pos++
	return b
}

func (c *cursor) uint() uint64 {
	var v uint64
	for shift := 0; shift < 64; shift += 7 {
		b := c.byte()
		v |= uint64(b&0x7f) << shift
		if b&0x80 == 0 {
			return v
		}
	}
	c.fail = true
	return 0
}

func (c *cursor) bytes(n uint64) []byte {
	if n > uint64(len(c.data)-c.pos) {
		c.fail = true
		return nil
	}
	b := c.data[c.pos : c.pos+int(n)]
	c.pos += int(n)
	return b
}

// Component sections, of which only the two that can hold code matter
// here: a core module is embedded whole, and a nested component holds
// sections of its own.
const (
	componentCoreModuleID = 1
	componentNestedID     = 4
)

// isComponent reports whether data starts with a component header: the
// magic of a core module, with the component-model layer and version.
func isComponent(data []byte) bool {
	return len(data) >= 8 && string(data[:4]) == "\x00asm" &&
		data[4] == 0x0d && data[5] == 0x00 && data[6] == 0x01 && data[7] == 0x00
}

// coreModules returns every core module a component embeds, outermost
// first, descending into nested components. Modules are stored whole, so
// each one is returned as its own module binary.
func coreModules(data []byte) [][]byte {
	// A component nests components nests components; the limit is only
	// there so a malformed file cannot recurse without end.
	const maxDepth = 8
	var collect func(data []byte, depth int) [][]byte
	collect = func(data []byte, depth int) [][]byte {
		var out [][]byte
		if depth > maxDepth {
			return out
		}
		c := &cursor{data: data, pos: 8} // past the magic and version
		for c.pos < len(data) && !c.fail {
			id := c.byte()
			size := c.uint()
			payload := c.bytes(size)
			if c.fail {
				return out
			}
			switch id {
			case componentCoreModuleID:
				out = append(out, payload)
			case componentNestedID:
				out = append(out, collect(payload, depth+1)...)
			}
		}
		return out
	}
	return collect(data, 0)
}
