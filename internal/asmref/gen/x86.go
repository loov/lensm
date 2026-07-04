package main

import (
	"encoding/xml"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// x86Root mirrors the uops.info (XED-derived) instructions.xml. Only the static
// "API" is mapped; <architecture>/<measurement> latency and port tables are
// simply not declared, so encoding/xml skips them.
type x86Root struct {
	Extensions []struct {
		Instructions []x86Instruction `xml:"instruction"`
	} `xml:"extension"`
}

type x86Instruction struct {
	Asm         string       `xml:"asm,attr"`
	Summary     string       `xml:"summary,attr"`
	Description string       `xml:"description"`
	Syntaxes    []x86Syntax  `xml:"syntax"`
	Operands    []x86Operand `xml:"operand"`
}

type x86Syntax struct {
	ATT  string `xml:"att,attr"` // "1" for AT&T-flavored forms
	Text string `xml:",chardata"`
}

type x86Operand struct {
	Type  string `xml:"type,attr"`  // reg, mem, imm, ...
	Width string `xml:"width,attr"` // bit width, when meaningful
	R     string `xml:"r,attr"`     // "1" if read
	W     string `xml:"w,attr"`     // "1" if written
	Name  string `xml:",chardata"`  // slot as it appears in the syntax, e.g. r/m32
}

// ParseX86File parses a uops.info instructions.xml and feeds every instruction
// into the builder, keyed by its base mnemonic (the asm attribute). Width
// variants (8/16/32/64-bit) of the same instruction merge under one key.
func ParseX86File(b *Builder, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var root x86Root
	if err := xml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for _, ext := range root.Extensions {
		for _, inst := range ext.Instructions {
			mnemonic := strings.ToUpper(strings.TrimSpace(inst.Asm))
			if mnemonic == "" {
				continue
			}
			brief := inst.Summary
			description := inst.Description

			// Intel syntax first, AT&T appended so forms stay separable.
			var intel, att []string
			for _, s := range inst.Syntaxes {
				text := strings.TrimSpace(s.Text)
				if text == "" {
					continue
				}
				if s.ATT == "1" {
					att = append(att, text)
				} else {
					intel = append(intel, text)
				}
			}
			syntax := append(intel, att...)

			operands := map[string]string{}
			for _, op := range inst.Operands {
				name := strings.TrimSpace(op.Name)
				if name == "" {
					continue
				}
				operands[name] = describeX86Operand(op)
			}

			b.Add(mnemonic, brief, description, syntax, operands)
		}
	}
	return nil
}

// describeX86Operand turns the type/width/action attributes into a short human
// string, e.g. "32-bit register, read+written".
func describeX86Operand(op x86Operand) string {
	kind := map[string]string{
		"reg":   "register",
		"mem":   "memory operand",
		"imm":   "immediate",
		"agen":  "address",
		"relbr": "relative branch target",
		"flags": "flags",
	}[strings.ToLower(op.Type)]
	if kind == "" {
		kind = strings.ToLower(op.Type)
	}
	if kind == "" {
		kind = "operand"
	}

	prefix := ""
	if w, err := strconv.Atoi(strings.TrimSpace(op.Width)); err == nil && w > 0 {
		prefix = strconv.Itoa(w) + "-bit "
	}

	action := ""
	switch {
	case op.R == "1" && op.W == "1":
		action = ", read+written"
	case op.W == "1":
		action = ", written"
	case op.R == "1":
		action = ", read"
	}
	return prefix + kind + action
}
