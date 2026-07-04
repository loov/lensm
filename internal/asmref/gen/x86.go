package main

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"
)

// ParseX86File parses a uops.info instructions.xml and feeds every instruction
// into the builder, keyed by its base mnemonic (the asm attribute).
//
// uops.info is a benchmark dataset, not an ISA manual: there are no <syntax> or
// <description> elements. The usable "API" lives in attributes — summary (a
// human title, the brief) and string (the operand-form, e.g. "ADD (R32, M32)",
// used as the syntax). The <operand> children are intentionally not emitted:
// their derived descriptions ("32-bit register") are just a restatement of the
// token already visible in the syntax, so storing them per instruction
// duplicated one of ~69 strings tens of thousands of times.
//
// Everything else (<architecture>/<measurement>/<latency> micro-op tables) is
// skipped. The file is ~140MB with over a million perf nodes, so this is a
// streaming token walk that Skip()s those subtrees.
func ParseX86File(b *Builder, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dec := xml.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "instruction":
			mnemonic := strings.ToUpper(strings.TrimSpace(attr(start, "asm")))
			if mnemonic == "" {
				break
			}
			var syntax []string
			if form := strings.TrimSpace(attr(start, "string")); form != "" {
				syntax = []string{form}
			}
			// summary is the only human text; it serves as the brief.
			b.Add(mnemonic, attr(start, "summary"), "", syntax, nil)
		case "architecture":
			// Perf data (measurements, latencies, ports) hangs off here.
			if err := dec.Skip(); err != nil {
				return err
			}
		}
	}
	return nil
}
