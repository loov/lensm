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
