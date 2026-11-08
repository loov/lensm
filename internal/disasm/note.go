package disasm

// Note is a compiler optimization note about a source position, such as
// a value escaping to the heap. Lines and columns count from 1.
type Note struct {
	File      string
	Line, Col int
	// Code is the compiler's name for the note, e.g. "escapes",
	// "cannotInlineFunction" or "isInBounds".
	Code    string
	Message string
	// Related explains the note, for example the flow that makes a
	// value escape. Its notes have no Code.
	Related []Note
}

// NoteKind groups note codes so they can be shown or hidden together.
type NoteKind uint8

const (
	NoteEscape NoteKind = 1 << iota
	NoteLeak
	NoteInline
	NoteBounds
	NoteNil
	NoteCopy

	AllNotes = NoteEscape | NoteLeak | NoteInline | NoteBounds | NoteNil | NoteCopy
)

// NoteKinds lists every kind in display order.
var NoteKinds = [...]NoteKind{NoteEscape, NoteLeak, NoteInline, NoteBounds, NoteNil, NoteCopy}

// Kind is the group of n's code, or 0 for a code lensm does not know.
func (n *Note) Kind() NoteKind {
	switch n.Code {
	case "escape", "escapes":
		return NoteEscape
	case "leak":
		return NoteLeak
	case "canInlineFunction", "cannotInlineFunction", "cannotInlineCall":
		return NoteInline
	case "isInBounds", "isSliceInBounds":
		return NoteBounds
	case "nilcheck":
		return NoteNil
	case "copy":
		return NoteCopy
	}
	return 0
}

// Good reports whether n is about a cost the compiler avoided.
func (n *Note) Good() bool { return n.Code == "canInlineFunction" }

// Text is the note as a line of text.
func (n *Note) Text() string {
	label := map[string]string{
		"canInlineFunction":    "can inline",
		"cannotInlineFunction": "cannot inline",
		"cannotInlineCall":     "cannot inline call",
		"isInBounds":           "bounds check",
		"isSliceInBounds":      "slice bounds check",
		"nilcheck":             "nil check",
		"copy":                 "copy",
	}[n.Code]
	switch {
	case label == "":
		return n.Message
	case n.Message == "":
		return label
	}
	return label + ": " + n.Message
}
