package main

import (
	"slices"
	"strings"

	"golang.org/x/arch/arm64/arm64asm"
	"golang.org/x/arch/x86/x86asm"

	"loov.dev/lensm/internal/asmref"
)

// knownMnemonics enumerates every mnemonic x/arch's decoders can name. There is
// no exported count, so we probe Op values until a long run returns the
// "Op(N)" fallback, which marks the end of the name table.
func knownMnemonics() map[string]bool {
	out := map[string]bool{}
	for _, name := range []func(int) string{
		func(i int) string { return x86asm.Op(i).String() },
		func(i int) string { return arm64asm.Op(i).String() },
	} {
		miss := 0
		for i := 1; i < 65000 && miss < 1000; i++ {
			s := name(i)
			if s == "" || strings.HasPrefix(s, "Op(") {
				miss++
				continue
			}
			miss = 0
			out[strings.ToUpper(s)] = true
		}
	}
	return out
}

// missingCoverage returns mnemonics x/arch knows that the generated table does
// not cover. It is advisory: a gap usually just means an ISA XML file was not
// present, not that anything is wrong.
func missingCoverage(table map[string]asmref.Entry) []string {
	var missing []string
	for name := range knownMnemonics() {
		if _, ok := table[name]; !ok {
			missing = append(missing, name)
		}
	}
	slices.Sort(missing)
	return missing
}
