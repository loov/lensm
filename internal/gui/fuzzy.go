package gui

import (
	"strings"
)

// FuzzyScore reports whether the runes of pattern appear in name in
// order, ignoring ASCII case, and scores the match: higher is better.
// Matches at word starts and runs of adjacent runes score more, and so
// does matching inside the last part of a qualified name, so "lj"
// finds "goobj.layoutJumps" before "lensm/jumps.Load".
func FuzzyScore(pattern, name string) (int, bool) {
	if pattern == "" {
		return 0, true
	}
	pattern = strings.ToLower(pattern)
	best, found := fuzzyFrom(pattern, name, 0)
	// Greedy matching from the left can spend the pattern on the
	// package path; also try from where the function name starts.
	if start := nameStart(name); start > 0 {
		if score, ok := fuzzyFrom(pattern, name, start); ok && (!found || score > best) {
			best, found = score, true
		}
	}
	if !found {
		return 0, false
	}
	// Prefer shorter names among equal matches.
	return best*64 - len(name), true
}

func fuzzyFrom(pattern, name string, start int) (int, bool) {
	tail := nameStart(name)
	score, p, last := 0, 0, -2
	for i := start; i < len(name) && p < len(pattern); i++ {
		if lower(name[i]) != pattern[p] {
			continue
		}
		s := 1
		if i == last+1 {
			s += 4
		}
		if isWordStart(name, i) {
			s += 6
		}
		if i >= tail {
			s += 2
		}
		score += s
		last = i
		p++
	}
	return score, p == len(pattern)
}

// nameStart is the index after the last '.' or '/' outside of type
// arguments, where the unqualified name begins.
func nameStart(name string) int {
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	return strings.LastIndexAny(name, "./") + 1
}

func isWordStart(name string, i int) bool {
	if i == 0 {
		return true
	}
	prev, cur := name[i-1], name[i]
	switch prev {
	case '.', '/', '_', '(', '*', ' ', ':', '[', '-':
		return true
	}
	return 'a' <= prev && prev <= 'z' && 'A' <= cur && cur <= 'Z'
}

func lower(b byte) byte {
	if 'A' <= b && b <= 'Z' {
		return b + 'a' - 'A'
	}
	return b
}

// ShortName drops the package path but its last element, so
// "loov.dev/lensm/internal/goobj.layoutJumps" becomes "goobj.layoutJumps".
func ShortName(name string) string {
	pkg, rest := SplitPackage(name)
	if i := strings.LastIndexByte(pkg, '/'); i >= 0 {
		pkg = pkg[i+1:]
	}
	return pkg + rest
}

// SplitPackage splits a qualified function name into the package path
// with its trailing dot and the rest, for example
// "loov.dev/lensm/internal/goobj." and "(*Code).layoutJumps".
func SplitPackage(name string) (pkg, rest string) {
	end := len(name)
	if i := strings.IndexByte(name, '['); i >= 0 {
		end = i
	}
	slash := strings.LastIndexByte(name[:end], '/')
	dot := strings.IndexByte(name[slash+1:end], '.')
	if dot < 0 {
		return "", name
	}
	split := slash + 1 + dot + 1
	return name[:split], name[split:]
}
