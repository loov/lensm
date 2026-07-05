package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// armCollected holds the syntaxes and operand meanings gathered for one
// mnemonic within a single instruction file.
type armCollected struct {
	Syntax   []string
	Operands map[string]string
}

// armFile is the reference extracted from one AArch64 instruction XML file.
type armFile struct {
	Brief       string // short title, from <desc><brief>
	Description string // first authored paragraph
	Mnemonics   map[string]*armCollected
}

// ParseARMDir walks an ARM AArch64 ISA XML directory and feeds every
// instruction file into the builder. Files whose basename starts with
// "sysreg_" (system registers) or "AArch64-" (shared pseudocode, not
// instructions) are skipped per the ARM release layout.
func ParseARMDir(b *Builder, dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".xml") {
			return nil
		}
		if strings.HasPrefix(name, "sysreg_") || strings.HasPrefix(name, "AArch64-") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		parsed, err := parseARMFile(f)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		for mnemonic, c := range parsed.Mnemonics {
			b.Add(mnemonic, parsed.Brief, parsed.Description, c.Syntax, nil, c.Operands)
		}
		return nil
	})
}

// parseARMFile extracts the factual reference from one instruction file. It is
// a token walk rather than struct unmarshaling because <asmtemplate> is mixed
// content (literal text interleaved with <a> operand links) and because
// <ps_section> (pseudocode) and <regdiagram> (bit encodings) must be skipped
// wholesale.
func parseARMFile(r io.Reader) (armFile, error) {
	dec := xml.NewDecoder(r)
	out := armFile{Mnemonics: map[string]*armCollected{}}

	var (
		curMnemonic   string
		aliasMnemonic string // set for alias files (e.g. UBFIZ aliasing UBFM)
		title         string // instructionsection title attr, a fallback brief

		inBrief     bool // inside <desc><brief>
		inBriefPara bool // capturing the first <para> of <brief>
		haveBrief   bool
		briefBuf    strings.Builder

		inAuthored bool // inside <desc><authored>
		inDescPara bool // capturing the first <para> of <authored>
		haveDesc   bool
		descBuf    strings.Builder

		inAsm    bool // inside an <asmtemplate>
		synBuf   strings.Builder
		inA      bool // inside an <a> operand link within an asmtemplate
		aHover   string
		aTextBuf strings.Builder
	)

	// An alias file documents the alias (UBFIZ), not its underlying base
	// (UBFM) — the mnemonic docvar holds the base, so the alias_mnemonic wins.
	mnemonic := func() string {
		if aliasMnemonic != "" {
			return aliasMnemonic
		}
		return curMnemonic
	}

	collected := func() *armCollected {
		key := mnemonic()
		c := out.Mnemonics[key]
		if c == nil {
			c = &armCollected{Operands: map[string]string{}}
			out.Mnemonics[key] = c
		}
		return c
	}

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return armFile{}, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "instructionsection":
				title = attr(t, "title")
			case "ps_section", "regdiagram":
				if err := dec.Skip(); err != nil {
					return armFile{}, err
				}
			case "docvar":
				switch attr(t, "key") {
				case "mnemonic":
					curMnemonic = attr(t, "value")
				case "alias_mnemonic":
					aliasMnemonic = attr(t, "value")
				}
			case "brief":
				inBrief = true
			case "authored":
				inAuthored = true
			case "para":
				if inBrief && !haveBrief {
					inBriefPara = true
					briefBuf.Reset()
				} else if inAuthored && !haveDesc {
					inDescPara = true
					descBuf.Reset()
				}
			case "asmtemplate":
				inAsm = true
				synBuf.Reset()
			case "a":
				if inAsm {
					inA = true
					aHover = attr(t, "hover")
					aTextBuf.Reset()
				}
			}
		case xml.CharData:
			if inBriefPara {
				briefBuf.Write(t)
			}
			if inDescPara {
				descBuf.Write(t)
			}
			if inAsm {
				synBuf.Write(t)
				if inA {
					aTextBuf.Write(t)
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "brief":
				inBrief = false
			case "authored":
				inAuthored = false
			case "para":
				if inBriefPara {
					out.Brief = briefBuf.String()
					haveBrief = true
					inBriefPara = false
				} else if inDescPara {
					out.Description = descBuf.String()
					haveDesc = true
					inDescPara = false
				}
			case "a":
				if inA {
					name := strings.TrimSpace(aTextBuf.String())
					if name != "" && aHover != "" && mnemonic() != "" {
						collected().Operands[name] = aHover
					}
					inA = false
				}
			case "asmtemplate":
				inAsm = false
				if mnemonic() != "" {
					if syn := normalizeSpace(synBuf.String()); syn != "" {
						c := collected()
						if !slices.Contains(c.Syntax, syn) {
							c.Syntax = append(c.Syntax, syn)
						}
					}
				}
			}
		}
	}
	// Fall back to the title attr (minus its " -- A64" profile suffix) when a
	// file has no <brief> paragraph.
	if out.Brief == "" {
		if i := strings.Index(title, " -- "); i >= 0 {
			title = title[:i]
		}
		out.Brief = title
	}
	return out, nil
}

func attr(e xml.StartElement, key string) string {
	for _, a := range e.Attr {
		if a.Name.Local == key {
			return a.Value
		}
	}
	return ""
}
