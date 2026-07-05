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
	Title       string
	Description string
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
			b.Add(mnemonic, parsed.Title, parsed.Description, c.Syntax, nil, c.Operands)
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
		curMnemonic string
		haveDesc    bool
		inPara      bool // capturing the first <para> of <desc><authored>
		descBuf     strings.Builder
		inAuthored  bool

		inAsm    bool // inside an <asmtemplate>
		synBuf   strings.Builder
		inA      bool // inside an <a> operand link within an asmtemplate
		aHover   string
		aTextBuf strings.Builder
	)

	collected := func() *armCollected {
		c := out.Mnemonics[curMnemonic]
		if c == nil {
			c = &armCollected{Operands: map[string]string{}}
			out.Mnemonics[curMnemonic] = c
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
				out.Title = attr(t, "title")
			case "ps_section", "regdiagram":
				if err := dec.Skip(); err != nil {
					return armFile{}, err
				}
			case "docvar":
				if attr(t, "key") == "mnemonic" {
					curMnemonic = attr(t, "value")
				}
			case "authored":
				inAuthored = true
			case "para":
				if inAuthored && !haveDesc {
					inPara = true
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
			if inPara {
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
			case "authored":
				inAuthored = false
			case "para":
				if inPara {
					out.Description = descBuf.String()
					haveDesc = true
					inPara = false
				}
			case "a":
				if inA {
					name := strings.TrimSpace(aTextBuf.String())
					if name != "" && aHover != "" && curMnemonic != "" {
						collected().Operands[name] = aHover
					}
					inA = false
				}
			case "asmtemplate":
				inAsm = false
				if curMnemonic != "" {
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
