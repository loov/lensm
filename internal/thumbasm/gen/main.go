// Command gen builds the T32 (Thumb) encoding table for package thumbasm
// from ARM's AArch32 ISA XML (the "Exploration tools" release of
// developer.arm.com; see data/download.sh).
//
// For every T32 encoding it emits the fixed bits, the named bit fields,
// the encoding's selection condition, its assembler templates, the
// decode pseudocode and the symbol → field mapping from the explanations.
// Operand semantics are not generated: thumbasm evaluates the pseudocode
// at decode time with a tiny ASL interpreter and formats the template
// symbols itself.
//
//	go run ./internal/thumbasm/gen -xml data/arm32/ISA_AArch32_xml_A_profile-2025-12 -classes general,fpsimd -out internal/thumbasm/tables_gen.go
package main

import (
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"go/format"
	"html"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type docvar struct {
	Key   string `xml:"key,attr"`
	Value string `xml:"value,attr"`
}

type section struct {
	XMLName xml.Name `xml:"instructionsection"`
	Docvars []docvar `xml:"docvars>docvar"`
	AliasTo *struct {
		RefIform string `xml:"refiform,attr"`
	} `xml:"aliasto"`
	IClasses     []iclass      `xml:"classes>iclass"`
	Explanations []explanation `xml:"explanations>explanation"`
}

type iclass struct {
	Name       string   `xml:"name,attr"`
	ISA        string   `xml:"isa,attr"`
	Docvars    []docvar `xml:"docvars>docvar"`
	Regdiagram struct {
		Form   string `xml:"form,attr"`
		PSName string `xml:"psname,attr"`
		Boxes  []box  `xml:"box"`
	} `xml:"regdiagram"`
	Encodings []encodingXML `xml:"encoding"`
	PS        []struct {
		Section string `xml:"section,attr"`
		Inner   string `xml:",innerxml"`
	} `xml:"ps_section>ps>pstext"`
}

type box struct {
	Hibit      int    `xml:"hibit,attr"`
	Width      int    `xml:"width,attr"`
	Name       string `xml:"name,attr"`
	Constraint string `xml:"constraint,attr"`
	C          []struct {
		Colspan int    `xml:"colspan,attr"`
		Text    string `xml:",chardata"`
	} `xml:"c"`
}

type encodingXML struct {
	Name      string   `xml:"name,attr"`
	Bitdiffs  string   `xml:"bitdiffs,attr"`
	Docvars   []docvar `xml:"docvars>docvar"`
	Templates []struct {
		Comment string `xml:"comment,attr"`
		Inner   string `xml:",innerxml"`
	} `xml:"asmtemplate"`
	AliasCond string `xml:"equivalent_to>aliascond"`
}

type explanation struct {
	Enclist string `xml:"enclist,attr"`
	Symbol  string `xml:"symbol"`
	Account *struct {
		Encodedin string `xml:"encodedin,attr"`
	} `xml:"account"`
	Definition *struct {
		Encodedin string `xml:"encodedin,attr"`
		Head      []struct {
			Class string `xml:"class,attr"`
		} `xml:"table>tgroup>thead>row>entry"`
		Rows []struct {
			Entries []struct {
				Class string `xml:"class,attr"`
				Text  string `xml:",chardata"`
			} `xml:"entry"`
		} `xml:"table>tgroup>tbody>row"`
	} `xml:"definition"`
}

func docvarValue(vars []docvar, key string) string {
	for _, v := range vars {
		if v.Key == key {
			return v.Value
		}
	}
	return ""
}

var rxTag = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string {
	return html.UnescapeString(rxTag.ReplaceAllString(s, ""))
}

// stripComments drops ASL line comments; the decode text is multi-line.
func stripComments(s string) string {
	var lines []string
	for line := range strings.SplitSeq(s, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, " ")
}

type field struct {
	Name  string
	Lo, N int
}

type tmpl struct {
	When, Text string
}

// valueTable is an explanation's value table: the symbol's spelling for
// each value of the fields it is encoded in.
type valueTable struct {
	Encodedin string
	Rows      []valueRow
}

type valueRow struct {
	Bits, Text string
}

type encoding struct {
	Name, Mnemonic string
	Width          int
	Mask, Value    uint32
	// SBMask/SBValue are the "should be" bits, (0)/(1) in the diagrams:
	// not part of the encoding, but other values are UNPREDICTABLE.
	SBMask, SBValue uint32
	Fields          []field
	Cond            string
	Tmpl            []tmpl
	Decode          string
	Enc             map[string]string
	Tables          map[string]valueTable
	Alias           string
	IsAlias         bool
	// SIMD marks Advanced SIMD (NEON) encodings, which T32-only cores
	// lack; derived from the pseudocode section name.
	SIMD bool
	file string
}

func main() {
	xmlDir := flag.String("xml", "", "directory with the AArch32 ISA XML files")
	out := flag.String("out", "tables_gen.go", "output file")
	classes := flag.String("classes", "general", "comma-separated instr-class values to include")
	flag.Parse()
	if *xmlDir == "" {
		log.Fatal("-xml is required")
	}
	want := map[string]bool{}
	for c := range strings.SplitSeq(*classes, ",") {
		want[c] = true
	}

	files, err := filepath.Glob(filepath.Join(*xmlDir, "*.xml"))
	if err != nil {
		log.Fatal(err)
	}
	sections := map[string]section{}
	var order []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}
		if !bytes.Contains(data, []byte("<instructionsection")) {
			continue
		}
		var sec section
		if err := xml.Unmarshal(data, &sec); err != nil {
			log.Fatalf("%s: %v", f, err)
		}
		sections[filepath.Base(f)] = sec
		order = append(order, filepath.Base(f))
	}
	var encs []encoding
	for _, name := range order {
		sec := sections[name]
		if !want[docvarValue(sec.Docvars, "instr-class")] {
			continue
		}
		// Alias pages carry no decode pseudocode of their own; borrow
		// the base page's for the iclass of the same name (T2 ↔ T2).
		baseDecode := map[string]string{}
		if sec.AliasTo != nil {
			if base, ok := sections[sec.AliasTo.RefIform]; ok {
				for _, ic := range base.IClasses {
					baseDecode[ic.Name] = decodeText(ic)
				}
			}
		}
		es, err := convert(name, sec, baseDecode)
		if err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		encs = append(encs, es...)
	}
	// Aliases before base encodings, then more specific (more fixed bits)
	// first, then by name for a stable output.
	sort.SliceStable(encs, func(i, j int) bool {
		a, b := encs[i], encs[j]
		if a.IsAlias != b.IsAlias {
			return a.IsAlias
		}
		if pa, pb := popcount(a.Mask), popcount(b.Mask); pa != pb {
			return pa > pb
		}
		return a.Name < b.Name
	})

	src := render(encs)
	formatted, err := format.Source(src)
	if err != nil {
		os.WriteFile(*out, src, 0o644)
		log.Fatalf("gofmt: %v", err)
	}
	if err := os.WriteFile(*out, formatted, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "wrote %d encodings to %s\n", len(encs), *out)
}

func popcount(x uint32) int {
	n := 0
	for ; x != 0; x &= x - 1 {
		n++
	}
	return n
}

func decodeText(ic iclass) string {
	for _, ps := range ic.PS {
		if ps.Section == "Decode" {
			return stripComments(stripTags(ps.Inner))
		}
	}
	return ""
}

func convert(file string, sec section, baseDecode map[string]string) ([]encoding, error) {
	// symbol → encodedin and value tables, keyed by encoding name.
	encodedin := map[string]map[string]string{}
	tables := map[string]map[string]valueTable{}
	for _, ex := range sec.Explanations {
		var ein string
		var table *valueTable
		switch {
		case ex.Account != nil:
			ein = ex.Account.Encodedin
		case ex.Definition != nil:
			ein = ex.Definition.Encodedin
			if len(ex.Definition.Rows) > 0 {
				table = &valueTable{Encodedin: ein}
				for _, row := range ex.Definition.Rows {
					var bits, text string
					for _, e := range row.Entries {
						switch e.Class {
						case "bitfield":
							bits += strings.TrimSpace(e.Text)
						case "symbol":
							text = strings.TrimSpace(html.UnescapeString(e.Text))
						}
					}
					if bits != "" && text != "" {
						table.Rows = append(table.Rows, valueRow{bits, text})
					}
				}
			}
		}
		sym := strings.TrimSpace(html.UnescapeString(ex.Symbol))
		for name := range strings.SplitSeq(ex.Enclist, ",") {
			name = strings.TrimSpace(name)
			if encodedin[name] == nil {
				encodedin[name] = map[string]string{}
				tables[name] = map[string]valueTable{}
			}
			encodedin[name][sym] = ein
			if table != nil {
				tables[name][sym] = *table
			}
		}
	}

	var out []encoding
	for _, ic := range sec.IClasses {
		if ic.ISA != "T32" {
			continue
		}
		width := 16
		if ic.Regdiagram.Form == "16x2" {
			width = 32
		}
		var mask, value, sbMask, sbValue uint32
		var fields []field
		var constraints []string
		for _, b := range ic.Regdiagram.Boxes {
			w := b.Width
			if w == 0 {
				w = 1
			}
			lo := b.Hibit - w + 1
			if b.Name != "" {
				fields = append(fields, field{b.Name, lo, w})
			}
			if b.Constraint != "" {
				// "!= 1101" → "Rn != 1101"
				constraints = append(constraints, b.Name+" "+strings.TrimSpace(b.Constraint))
			}
			bit := b.Hibit
			for _, c := range b.C {
				span := c.Colspan
				if span == 0 {
					span = 1
				}
				txt := strings.TrimSpace(c.Text)
				if span == 1 && (txt == "0" || txt == "1") {
					mask |= 1 << uint(bit)
					if txt == "1" {
						value |= 1 << uint(bit)
					}
				} else if span == 1 && (txt == "(0)" || txt == "(1)") {
					sbMask |= 1 << uint(bit)
					if txt == "(1)" {
						sbValue |= 1 << uint(bit)
					}
				} else if strings.HasPrefix(txt, "!=") && b.Constraint == "" {
					constraints = append(constraints, b.Name+" "+txt)
				}
				bit -= span
			}
			if bit != lo-1 {
				return nil, fmt.Errorf("%s %s: box %s spans %d..%d but cells cover to %d", file, ic.Name, b.Name, b.Hibit, lo, bit+1)
			}
		}
		decode := decodeText(ic)
		if decode == "" {
			decode = baseDecode[ic.Name]
		}
		mnemonicIC := docvarValue(ic.Docvars, "mnemonic")
		for _, e := range ic.Encodings {
			if strings.EqualFold(strings.TrimSpace(e.AliasCond), "Never") {
				continue
			}
			var conds []string
			conds = append(conds, constraints...)
			if bd := strings.TrimSpace(html.UnescapeString(e.Bitdiffs)); bd != "" {
				conds = append(conds, "("+bd+")")
			}
			mn := docvarValue(e.Docvars, "mnemonic")
			if mn == "" {
				mn = mnemonicIC
			}
			enc := encoding{
				Name:     e.Name,
				Mnemonic: mn,
				Width:    width,
				Mask:     mask,
				Value:    value,
				SBMask:   sbMask,
				SBValue:  sbValue,
				Fields:   fields,
				Cond:     strings.Join(conds, " && "),
				Decode:   decode,
				Enc:      encodedin[e.Name],
				Tables:   tables[e.Name],
				Alias:    strings.TrimSpace(html.UnescapeString(e.AliasCond)),
				IsAlias:  sec.AliasTo != nil,
				SIMD:     isSIMD(ic.Regdiagram.PSName),
				file:     file,
			}
			for _, t := range e.Templates {
				enc.Tmpl = append(enc.Tmpl, tmpl{When: t.Comment, Text: strings.Join(strings.Fields(stripTags(t.Inner)), " ")})
			}
			if len(enc.Tmpl) == 0 {
				return nil, fmt.Errorf("%s: %s has no asmtemplate", file, e.Name)
			}
			out = append(out, enc)
		}
	}
	return out, nil
}

// isSIMD classifies by the pseudocode section: "simddp" (data
// processing), "asimldst" (structure loads/stores) and "simd_dup"
// (element moves) are Advanced SIMD; "fpdp", "fp_msr" and
// "simdfp_ldst" are the floating-point extension.
func isSIMD(psname string) bool {
	for _, part := range []string{".simddp.", ".asim", ".simd_dup"} {
		if strings.Contains(psname, part) {
			return true
		}
	}
	return false
}

func render(encs []encoding) []byte {
	var b bytes.Buffer
	b.WriteString("// Code generated by internal/thumbasm/gen; DO NOT EDIT.\n\n")
	b.WriteString("package thumbasm\n\n")
	b.WriteString("var encodings = []encoding{\n")
	for _, e := range encs {
		fmt.Fprintf(&b, "\t{name: %q, mnemonic: %q, width: %d, mask: %#x, value: %#x,\n", e.Name, e.Mnemonic, e.Width, e.Mask, e.Value)
		if e.SBMask != 0 {
			fmt.Fprintf(&b, "\t\tsbMask: %#x, sbValue: %#x,\n", e.SBMask, e.SBValue)
		}
		b.WriteString("\t\tfields: []field{")
		for i, f := range e.Fields {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "{%q, %d, %d}", f.Name, f.Lo, f.N)
		}
		b.WriteString("},\n")
		if e.Cond != "" {
			fmt.Fprintf(&b, "\t\tcond: %q,\n", e.Cond)
		}
		b.WriteString("\t\ttmpl: []tmpl{")
		for i, t := range e.Tmpl {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "{%q, %q}", t.When, t.Text)
		}
		b.WriteString("},\n")
		if e.Decode != "" {
			fmt.Fprintf(&b, "\t\tdecode: %q,\n", e.Decode)
		}
		if len(e.Enc) > 0 {
			keys := make([]string, 0, len(e.Enc))
			for k := range e.Enc {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteString("\t\tenc: map[string]string{")
			for i, k := range keys {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "%q: %q", k, e.Enc[k])
			}
			b.WriteString("},\n")
		}
		if len(e.Tables) > 0 {
			keys := make([]string, 0, len(e.Tables))
			for k := range e.Tables {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteString("\t\ttables: map[string]valueTable{\n")
			for _, k := range keys {
				t := e.Tables[k]
				fmt.Fprintf(&b, "\t\t\t%q: {%q, []valueRow{", k, t.Encodedin)
				for i, r := range t.Rows {
					if i > 0 {
						b.WriteString(", ")
					}
					fmt.Fprintf(&b, "{%q, %q}", r.Bits, r.Text)
				}
				b.WriteString("}},\n")
			}
			b.WriteString("\t\t},\n")
		}
		if e.Alias != "" {
			fmt.Fprintf(&b, "\t\talias: %q,\n", e.Alias)
		}
		if e.IsAlias {
			b.WriteString("\t\tisAlias: true,\n")
		}
		if e.SIMD {
			b.WriteString("\t\tsimd: true,\n")
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n")
	return b.Bytes()
}
