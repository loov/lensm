// Command gen flattens CPU ISA XML into internal/asmref/table.json.
//
// It is run via `go generate ./internal/asmref`. By default it reads the small
// checked-in fixtures so a bare run stays reproducible; point -arm and -x86 at
// a real ARM AArch64 ISA XML release and a uops.info instructions.xml to
// produce the full table. See ../README.md for source versions and URLs.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
)

func main() {
	armDir := flag.String("arm", "gen/testdata/arm", "directory of ARM AArch64 ISA XML files")
	x86File := flag.String("x86", "gen/testdata/x86/instructions.xml", "uops.info instructions.xml path")
	x86Arch := flag.String("x86arch", "ADL-P", "uops.info microarchitecture to source port usage from")
	out := flag.String("out", "table.json", "output JSON path")
	flag.Parse()

	b := NewBuilder()
	if *armDir != "" {
		if err := ParseARMDir(b, *armDir); err != nil {
			log.Fatalf("arm: %v", err)
		}
	}
	if *x86File != "" {
		if err := ParseX86File(b, *x86File, *x86Arch); err != nil {
			log.Fatalf("x86: %v", err)
		}
	}

	table := b.Table()

	covered := make(map[string]bool, len(table))
	for k := range table {
		covered[k] = true
	}
	if missing := missingCoverage(covered); len(missing) > 0 {
		sample := missing
		if len(sample) > 20 {
			sample = sample[:20]
		}
		fmt.Fprintf(os.Stderr, "asmref: %d mnemonic(s) known to x/arch are not in the table (e.g. %s)\n",
			len(missing), strings.Join(sample, ", "))
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false) // keep "<Wd>" and "&" readable in the committed file
	if err := enc.Encode(table); err != nil {
		log.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(*out, buf.Bytes(), 0o644); err != nil {
		log.Fatalf("write %s: %v", *out, err)
	}
	fmt.Fprintf(os.Stderr, "asmref: wrote %d mnemonics to %s\n", len(table), *out)
}
