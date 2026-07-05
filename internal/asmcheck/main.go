// Command asmcheck reads `go tool objdump` output on stdin, extracts every Go
// (Plan 9) instruction mnemonic, and reports which ones asmhelp fails to
// resolve to real reference data. Throwaway analysis tool.
package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"

	"loov.dev/lensm/internal/asmhelp"
)

func main() {
	arch := "arm64"
	if len(os.Args) > 1 {
		arch = os.Args[1]
	}

	count := map[string]int{}     // mnemonic -> occurrences
	sample := map[string]string{} // mnemonic -> one full instruction line

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "TEXT ") {
			continue // function header pseudo-op, not an instruction
		}
		// objdump instruction lines are tab-separated (with empty padding
		// fields): file:line, addr, hex, instr. The instruction is the last
		// non-empty field.
		var instr string
		for _, f := range strings.Split(line, "\t") {
			if f = strings.TrimSpace(f); f != "" {
				instr = f
			}
		}
		if !strings.HasPrefix(strings.TrimSpace(line), " ") && !strings.Contains(line, "0x") {
			continue // function header line, not an instruction
		}
		toks := strings.Fields(instr)
		if len(toks) == 0 {
			continue
		}
		m := toks[0]
		count[m]++
		if sample[m] == "" {
			sample[m] = instr
		}
	}

	var unresolved, notPlausible []string
	resolved := 0
	for m := range count {
		help, ok := asmhelp.ForInstruction(arch, "", sample[m])
		switch {
		case ok && help.Note == "":
			resolved++
		case ok && help.Note != "":
			unresolved = append(unresolved, m)
		default:
			notPlausible = append(notPlausible, m)
		}
	}

	byCountDesc := func(list []string) {
		sort.Slice(list, func(i, j int) bool {
			if count[list[i]] != count[list[j]] {
				return count[list[i]] > count[list[j]]
			}
			return list[i] < list[j]
		})
	}
	byCountDesc(unresolved)
	byCountDesc(notPlausible)

	fmt.Printf("unique mnemonics: %d  resolved: %d  unresolved: %d  not-plausible: %d\n\n",
		len(count), resolved, len(unresolved), len(notPlausible))
	fmt.Println("UNRESOLVED (plausible mnemonics with no reference data), by frequency:")
	for _, m := range unresolved {
		fmt.Printf("  %6d  %-12s  %s\n", count[m], m, sample[m])
	}
	if len(notPlausible) > 0 {
		fmt.Println("\nNOT-PLAUSIBLE (skipped: directives, undecodable, etc.):")
		for _, m := range notPlausible {
			fmt.Printf("  %6d  %s\n", count[m], m)
		}
	}
}
