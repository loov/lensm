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
// used as the syntax). The <operand> children are not emitted (their derived
// descriptions just restate the token already visible in the syntax).
//
// Execution-port usage is read from the <measurement ports="..."> nodes, but
// only for the single reference microarchitecture x86arch: port usage differs
// per microarchitecture (~100 of them) and per operand form, so pinning one
// keeps the table bounded. Every other <architecture> subtree is skipped. The
// file is ~140MB with over a million perf nodes, so this is a streaming token
// walk.
func ParseX86File(b *Builder, path, x86arch string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dec := xml.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	var cur *x86Inst
	inTargetArch := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "instruction":
				cur = &x86Inst{
					asm:     attr(t, "asm"),
					summary: attr(t, "summary"),
					form:    attr(t, "string"),
				}
			case "architecture":
				if attr(t, "name") == x86arch {
					inTargetArch = true
				} else if err := dec.Skip(); err != nil {
					// Skip perf data for every other microarchitecture.
					return err
				}
			case "measurement":
				if cur != nil && inTargetArch {
					if p := strings.TrimSpace(attr(t, "ports")); p != "" {
						cur.ports = append(cur.ports, p)
					}
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "architecture":
				inTargetArch = false
			case "instruction":
				if cur != nil {
					addX86(b, cur)
					cur = nil
				}
			}
		}
	}
	return nil
}

type x86Inst struct {
	asm     string
	summary string
	form    string
	ports   []string
}

func addX86(b *Builder, inst *x86Inst) {
	mnemonic := strings.ToUpper(strings.TrimSpace(inst.asm))
	if mnemonic == "" {
		return
	}
	var syntax []string
	if form := strings.TrimSpace(inst.form); form != "" {
		syntax = []string{form}
	}
	// summary is the only human text; it serves as the brief.
	b.Add(mnemonic, inst.summary, "", syntax, inst.ports, nil)
}
