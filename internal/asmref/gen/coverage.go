package main

import (
	"sort"
	"strings"

	"golang.org/x/arch/arm64/arm64asm"
	"golang.org/x/arch/x86/x86asm"
)

// knownMnemonics enumerates every mnemonic x/arch's decoders can name. There is
// no exported count, so we probe Op values until a long run returns the
// "Op(N)" fallback, which marks the end of the name table.
func knownMnemonics() map[string]bool {
	out := map[string]bool{}
	add := func(s string) {
		if s == "" || strings.HasPrefix(s, "Op(") {
			return
		}
		out[strings.ToUpper(s)] = true
	}
	miss := 0
	for i := 1; i < 65000 && miss < 1000; i++ {
		s := x86asm.Op(i).String()
		if s == "" || strings.HasPrefix(s, "Op(") {
			miss++
			continue
		}
		miss = 0
		add(s)
	}
	miss = 0
	for i := 1; i < 65000 && miss < 1000; i++ {
		s := arm64asm.Op(i).String()
		if s == "" || strings.HasPrefix(s, "Op(") {
			miss++
			continue
		}
		miss = 0
		add(s)
	}
	return out
}

// missingCoverage returns mnemonics x/arch knows that the generated table does
// not cover. It is advisory: a gap usually just means an ISA XML file was not
// present, not that anything is wrong.
func missingCoverage(table map[string]bool) []string {
	var missing []string
	for name := range knownMnemonics() {
		if !table[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}
