package main

import (
	"regexp"
	"slices"
	"strings"

	"loov.dev/lensm/internal/asmref"
)

// Builder accumulates parsed fragments and merges everything that shares a
// mnemonic into a single Entry. Both the ARM and x86 parsers feed the same
// builder so, e.g., an ADD documented in several files collapses to one key.
type Builder struct {
	entries map[string]*asmref.Entry
}

func NewBuilder() *Builder {
	return &Builder{entries: map[string]*asmref.Entry{}}
}

// Add merges one parsed fragment. Merge rules are first-non-empty for the prose
// fields, dedup-append for syntax (document order preserved), and union for
// operands (first meaning for a name wins). This keeps regeneration
// deterministic given the same input.
func (b *Builder) Add(mnemonic, brief, description string, syntax []string, variants []asmref.Variant, operands map[string]string) {
	key := strings.ToUpper(strings.TrimSpace(mnemonic))
	if key == "" {
		return
	}
	e := b.entries[key]
	if e == nil {
		e = &asmref.Entry{Operands: map[string]string{}}
		b.entries[key] = e
	}
	if e.Brief == "" {
		e.Brief = clean(brief)
	}
	if e.Description == "" {
		e.Description = clean(description)
	}
	for _, s := range syntax {
		s = normalizeSpace(s)
		if s != "" && !slices.Contains(e.Syntax, s) {
			e.Syntax = append(e.Syntax, s)
		}
	}
	for _, v := range variants {
		if i := slices.IndexFunc(e.Variants, func(x asmref.Variant) bool { return x.Form == v.Form }); i >= 0 {
			e.Variants[i].Perf = append(e.Variants[i].Perf, v.Perf...)
		} else {
			e.Variants = append(e.Variants, v)
		}
	}
	for name, meaning := range operands {
		name = strings.TrimSpace(name)
		if name != "" && e.Operands[name] == "" {
			e.Operands[name] = clean(meaning)
		}
	}
}

// Table returns the merged result. Empty operand maps are dropped so the JSON
// stays clean (omitempty handles nil, not empty maps).
func (b *Builder) Table() map[string]asmref.Entry {
	out := make(map[string]asmref.Entry, len(b.entries))
	for k, e := range b.entries {
		if len(e.Operands) == 0 {
			e.Operands = nil
		}
		out[k] = *e
	}
	return out
}

var tagPattern = regexp.MustCompile(`<[^>]+>`)

// clean strips any residual HTML/XML tags and collapses whitespace to a single
// space. Operand text like "<Wd>" reaches clean already decoded, so only real
// markup (from description paragraphs) is removed.
func clean(s string) string {
	s = tagPattern.ReplaceAllString(s, "")
	return normalizeSpace(s)
}

func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
