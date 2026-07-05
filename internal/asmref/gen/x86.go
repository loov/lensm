package main

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"loov.dev/lensm/internal/asmref"
)

// ParseX86File parses a uops.info instructions.xml and feeds every instruction
// into the builder, keyed by its base mnemonic (the asm attribute).
//
// uops.info is a benchmark dataset, not an ISA manual: there are no <syntax> or
// <description> elements. The usable "API" lives in attributes — summary (a
// human title, the brief) and string (the operand-form). The <operand> children
// are not emitted (their derived descriptions just restate the token already in
// the form).
//
// Performance data is captured per operand form across every measured
// microarchitecture: for each <architecture> the first <measurement> yields
// uops, ports, throughput and worst-case latency. <IACA> estimate nodes are
// skipped in favour of the real measurements. The file is ~140MB, so this is a
// streaming token walk.
func ParseX86File(b *Builder, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dec := xml.NewDecoder(bufio.NewReaderSize(f, 1<<20))
	var (
		cur      *x86Inst
		curArch  string
		took     bool // a measurement was already taken for curArch
		building bool // inside the measurement we are keeping
		m        asmref.ArchPerf
	)
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
				cur = &x86Inst{asm: attr(t, "asm"), summary: attr(t, "summary"), form: attr(t, "string")}
			case "architecture":
				curArch, took = attr(t, "name"), false
			case "IACA":
				_ = dec.Skip() // estimate, not a measurement
			case "measurement":
				if cur == nil || curArch == "" || took {
					_ = dec.Skip()
					break
				}
				building = true
				m = asmref.ArchPerf{
					Arch:  curArch,
					Uops:  atoi(attr(t, "uops")),
					Ports: strings.TrimSpace(attr(t, "ports")),
					TP:    measuredThroughput(t),
				}
			case "latency":
				if building {
					if c := atoi(attr(t, "cycles")); c > m.Latency {
						m.Latency = c
					}
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "measurement":
				if building {
					cur.perf = append(cur.perf, m)
					building, took = false, true
				}
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
	perf    []asmref.ArchPerf
}

func addX86(b *Builder, inst *x86Inst) {
	mnemonic := strings.ToUpper(strings.TrimSpace(inst.asm))
	if mnemonic == "" {
		return
	}
	form := strings.TrimSpace(inst.form)
	var syntax []string
	var variants []asmref.Variant
	if form != "" {
		syntax = []string{form}
		if len(inst.perf) > 0 {
			variants = []asmref.Variant{{Form: form, Perf: inst.perf}}
		}
	}
	// summary is the only human text; it serves as the brief.
	b.Add(mnemonic, inst.summary, "", syntax, variants, nil)
}

// measuredThroughput prefers the loop-measured throughput, falling back to the
// unrolled measurement.
func measuredThroughput(t xml.StartElement) float64 {
	for _, key := range []string{"TP_loop", "TP_unrolled", "TP"} {
		if v := attr(t, key); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				return f
			}
		}
	}
	return 0
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
