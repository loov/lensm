package syntax

import (
	"go/scanner"
	"go/token"
	"image/color"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	StyleGoLand  = "goland-light"
	StyleDarcula = "darcula"
	StyleMono    = "mono"
)

type Palette struct {
	Plain      color.NRGBA
	Keyword    color.NRGBA
	Builtin    color.NRGBA
	String     color.NRGBA
	Number     color.NRGBA
	Comment    color.NRGBA
	Operator   color.NRGBA
	Register   color.NRGBA
	Mnemonic   color.NRGBA
	Symbol     color.NRGBA
	LineNumber color.NRGBA
	CallTarget color.NRGBA
}

func NormalizeStyle(style string) string {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "", "goland", "goland-light", "jetbrains":
		return StyleGoLand
	case "darcula", "goland-dark", "jetbrains-dark":
		return StyleDarcula
	case "mono", "monochrome", "plain":
		return StyleMono
	default:
		return StyleGoLand
	}
}

func StyleLabel(style string) string {
	switch NormalizeStyle(style) {
	case StyleDarcula:
		return "Darcula"
	case StyleMono:
		return "Mono"
	default:
		return "GoLand"
	}
}

// Colors are the theme colors the palettes derive from.
type Colors struct {
	Text       color.NRGBA
	MutedText  color.NRGBA
	Background color.NRGBA
}

func PaletteFor(style string, colors Colors) Palette {
	switch NormalizeStyle(style) {
	case StyleDarcula:
		if !syntaxBackgroundDark(colors.Background) {
			return Palette{
				Plain:      color.NRGBA{R: 0x2b, G: 0x2d, B: 0x30, A: 0xff},
				Keyword:    color.NRGBA{R: 0xa8, G: 0x55, B: 0x22, A: 0xff},
				Builtin:    color.NRGBA{R: 0x3a, G: 0x72, B: 0x80, A: 0xff},
				String:     color.NRGBA{R: 0x3c, G: 0x78, B: 0x35, A: 0xff},
				Number:     color.NRGBA{R: 0x17, G: 0x50, B: 0xeb, A: 0xff},
				Comment:    color.NRGBA{R: 0x7a, G: 0x7a, B: 0x7a, A: 0xff},
				Operator:   color.NRGBA{R: 0x2b, G: 0x2d, B: 0x30, A: 0xff},
				Register:   color.NRGBA{R: 0x66, G: 0x0e, B: 0x7a, A: 0xff},
				Mnemonic:   color.NRGBA{R: 0xa8, G: 0x55, B: 0x22, A: 0xff},
				Symbol:     color.NRGBA{R: 0x2b, G: 0x68, B: 0x88, A: 0xff},
				LineNumber: colors.MutedText,
				CallTarget: color.NRGBA{R: 0x00, G: 0x00, B: 0xee, A: 0xff},
			}
		}
		return Palette{
			Plain:      color.NRGBA{R: 0xa9, G: 0xb7, B: 0xc6, A: 0xff},
			Keyword:    color.NRGBA{R: 0xcc, G: 0x78, B: 0x32, A: 0xff},
			Builtin:    color.NRGBA{R: 0x88, G: 0xc0, B: 0xd0, A: 0xff},
			String:     color.NRGBA{R: 0x6a, G: 0x87, B: 0x59, A: 0xff},
			Number:     color.NRGBA{R: 0x68, G: 0x97, B: 0xbb, A: 0xff},
			Comment:    color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff},
			Operator:   color.NRGBA{R: 0xa9, G: 0xb7, B: 0xc6, A: 0xff},
			Register:   color.NRGBA{R: 0x98, G: 0x76, B: 0xaa, A: 0xff},
			Mnemonic:   color.NRGBA{R: 0xff, G: 0xc6, B: 0x6d, A: 0xff},
			Symbol:     color.NRGBA{R: 0xa5, G: 0xc2, B: 0x61, A: 0xff},
			LineNumber: colors.MutedText,
			CallTarget: color.NRGBA{R: 0x62, G: 0x9c, B: 0xf6, A: 0xff},
		}
	case StyleMono:
		return Palette{
			Plain:      colors.Text,
			Keyword:    colors.Text,
			Builtin:    colors.Text,
			String:     colors.Text,
			Number:     colors.Text,
			Comment:    colors.MutedText,
			Operator:   colors.Text,
			Register:   colors.Text,
			Mnemonic:   colors.Text,
			Symbol:     colors.Text,
			LineNumber: colors.MutedText,
			CallTarget: colors.Text,
		}
	default:
		if syntaxBackgroundDark(colors.Background) {
			return Palette{
				Plain:      color.NRGBA{R: 0xba, G: 0xbe, B: 0xc7, A: 0xff},
				Keyword:    color.NRGBA{R: 0xcf, G: 0x8e, B: 0x6d, A: 0xff},
				Builtin:    color.NRGBA{R: 0xbc, G: 0x94, B: 0xf9, A: 0xff},
				String:     color.NRGBA{R: 0x6a, G: 0xab, B: 0x73, A: 0xff},
				Number:     color.NRGBA{R: 0x2a, G: 0xa1, B: 0xf3, A: 0xff},
				Comment:    color.NRGBA{R: 0x7a, G: 0x80, B: 0x87, A: 0xff},
				Operator:   color.NRGBA{R: 0xba, G: 0xbe, B: 0xc7, A: 0xff},
				Register:   color.NRGBA{R: 0xbc, G: 0x94, B: 0xf9, A: 0xff},
				Mnemonic:   color.NRGBA{R: 0xcf, G: 0x8e, B: 0x6d, A: 0xff},
				Symbol:     color.NRGBA{R: 0x56, G: 0xa8, B: 0xf5, A: 0xff},
				LineNumber: colors.MutedText,
				CallTarget: color.NRGBA{R: 0x56, G: 0xa8, B: 0xf5, A: 0xff},
			}
		}
		return Palette{
			Plain:      color.NRGBA{R: 0x08, G: 0x08, B: 0x08, A: 0xff},
			Keyword:    color.NRGBA{R: 0x00, G: 0x00, B: 0x80, A: 0xff},
			Builtin:    color.NRGBA{R: 0x87, G: 0x10, B: 0x94, A: 0xff},
			String:     color.NRGBA{R: 0x00, G: 0x80, B: 0x00, A: 0xff},
			Number:     color.NRGBA{R: 0x17, G: 0x50, B: 0xeb, A: 0xff},
			Comment:    color.NRGBA{R: 0x8c, G: 0x8c, B: 0x8c, A: 0xff},
			Operator:   color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xff},
			Register:   color.NRGBA{R: 0x66, G: 0x0e, B: 0x7a, A: 0xff},
			Mnemonic:   color.NRGBA{R: 0x00, G: 0x00, B: 0x80, A: 0xff},
			Symbol:     color.NRGBA{R: 0x87, G: 0x10, B: 0x94, A: 0xff},
			LineNumber: colors.MutedText,
			CallTarget: color.NRGBA{R: 0x00, G: 0x00, B: 0xee, A: 0xff},
		}
	}
}

func syntaxBackgroundDark(bg color.NRGBA) bool {
	return int(bg.R)*299+int(bg.G)*587+int(bg.B)*114 < 128000
}

func HighlightSource(lineNo int, line string, palette Palette) []Span {
	spans := []Span{{
		Text:  lineNumberPrefix(lineNo),
		Color: palette.LineNumber,
	}}
	return append(spans, HighlightGo(line, palette)...)
}

func HighlightGo(line string, palette Palette) []Span {
	if line == "" {
		return nil
	}

	var spans []Span
	fset := token.NewFileSet()
	file := fset.AddFile("", -1, len(line))
	var scan scanner.Scanner
	scan.Init(file, []byte(line), nil, scanner.ScanComments)

	cursor := 0
	for {
		pos, tok, lit := scan.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON && lit == "\n" {
			continue
		}

		start := file.Offset(pos)
		if start > len(line) {
			break
		}
		if start < cursor {
			continue
		}
		if start > cursor {
			appendSyntaxSpan(&spans, line[cursor:start], palette.Plain, false, false)
		}

		text := lit
		if text == "" {
			text = tok.String()
		}
		if text == "" {
			continue
		}

		end := start + len(text)
		if end > len(line) {
			end = len(line)
			text = line[start:end]
		}
		col, italic := goTokenStyle(tok, text, palette)
		appendSyntaxSpan(&spans, text, col, false, italic)
		cursor = end
	}
	if cursor < len(line) {
		appendSyntaxSpan(&spans, line[cursor:], palette.Plain, false, false)
	}
	return spans
}

func HighlightAsm(line string, callTarget string, palette Palette) []Span {
	return highlightAsmLine(line, callTarget, palette)
}

func lineNumberPrefix(lineNo int) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(lineNo))
	for b.Len() < 4 {
		b.WriteByte(' ')
	}
	b.WriteByte(' ')
	return b.String()
}

func goTokenStyle(tok token.Token, text string, palette Palette) (color.NRGBA, bool) {
	switch {
	case tok.IsKeyword():
		return palette.Keyword, false
	case tok == token.COMMENT:
		return palette.Comment, true
	case tok == token.STRING || tok == token.CHAR:
		return palette.String, false
	case tok == token.INT || tok == token.FLOAT || tok == token.IMAG:
		return palette.Number, false
	case tok == token.IDENT && goBuiltinNames[text]:
		return palette.Builtin, false
	case tok.IsOperator():
		return palette.Operator, false
	default:
		return palette.Plain, false
	}
}

func highlightAsmLine(line string, callTarget string, palette Palette) []Span {
	if line == "" {
		return nil
	}

	code, comment := splitAsmComment(line)
	spans := highlightAsmCode(code, callTarget, palette)
	if comment != "" {
		appendSyntaxSpan(&spans, comment, palette.Comment, false, true)
	}
	return spans
}

func splitAsmComment(line string) (string, string) {
	commentAt := len(line)
	for _, marker := range []string{"//", ";"} {
		if ix := strings.Index(line, marker); ix >= 0 && ix < commentAt {
			commentAt = ix
		}
	}
	if commentAt == len(line) {
		return line, ""
	}
	return line[:commentAt], line[commentAt:]
}

func highlightAsmCode(code string, callTarget string, palette Palette) []Span {
	var spans []Span
	seenMnemonic := false
	for i := 0; i < len(code); {
		r, size := runeAt(code, i)
		if unicode.IsSpace(r) {
			start := i
			i += size
			for i < len(code) {
				r, size = runeAt(code, i)
				if !unicode.IsSpace(r) {
					break
				}
				i += size
			}
			appendSyntaxSpan(&spans, code[start:i], palette.Plain, false, false)
			continue
		}
		if isAsmPunctuation(r) {
			appendSyntaxSpan(&spans, code[i:i+size], palette.Operator, false, false)
			i += size
			continue
		}

		start := i
		i += size
		for i < len(code) {
			r, size = runeAt(code, i)
			if unicode.IsSpace(r) || isAsmPunctuation(r) {
				break
			}
			i += size
		}
		word := code[start:i]
		col := asmWordColor(word, seenMnemonic, callTarget, palette)
		bold := false
		if !seenMnemonic && strings.TrimSpace(word) != "" {
			seenMnemonic = true
			bold = true
		}
		appendSyntaxSpan(&spans, word, col, bold, false)
	}
	return spans
}

func asmWordColor(word string, seenMnemonic bool, callTarget string, palette Palette) color.NRGBA {
	trimmed := strings.TrimSpace(word)
	trimmed = strings.Trim(trimmed, "[]{}")
	upper := strings.ToUpper(strings.TrimPrefix(trimmed, "%"))

	switch {
	case trimmed == "":
		return palette.Plain
	case !seenMnemonic:
		return palette.Mnemonic
	case callTarget != "" && strings.Contains(trimmed, callTarget):
		return palette.CallTarget
	case isAsmNumber(trimmed):
		return palette.Number
	case isAsmRegister(upper):
		return palette.Register
	case strings.ContainsAny(trimmed, "./:<>@") || strings.Contains(trimmed, "(SB)"):
		return palette.Symbol
	default:
		return palette.Plain
	}
}

func isAsmNumber(word string) bool {
	word = strings.TrimPrefix(word, "$")
	word = strings.TrimPrefix(word, "#")
	word = strings.TrimPrefix(word, "-")
	word = strings.TrimPrefix(word, "+")
	if word == "" {
		return false
	}
	if strings.HasPrefix(word, "0x") || strings.HasPrefix(word, "0X") {
		if len(word) <= 2 {
			return false
		}
		for _, r := range word[2:] {
			if !('0' <= r && r <= '9') && !('a' <= r && r <= 'f') && !('A' <= r && r <= 'F') {
				return false
			}
		}
		return true
	}
	for _, r := range word {
		if !('0' <= r && r <= '9') {
			return false
		}
	}
	return true
}

func isAsmRegister(word string) bool {
	if asmRegisterNames[word] {
		return true
	}
	if len(word) >= 2 {
		prefix := word[0]
		if prefix == 'R' || prefix == 'X' || prefix == 'W' || prefix == 'V' || prefix == 'Q' || prefix == 'D' || prefix == 'S' || prefix == 'B' {
			for _, r := range word[1:] {
				if r < '0' || r > '9' {
					return false
				}
			}
			return true
		}
	}
	return false
}

func isAsmPunctuation(r rune) bool {
	switch r {
	case ',', '(', ')', '[', ']', '{', '}', '+', '-', '*':
		return true
	default:
		return false
	}
}

func appendSyntaxSpan(spans *[]Span, text string, col color.NRGBA, bold, italic bool) {
	if text == "" {
		return
	}
	last := len(*spans) - 1
	if last >= 0 {
		prev := &(*spans)[last]
		if prev.Color == col && prev.Bold == bold && prev.Italic == italic {
			prev.Text += text
			return
		}
	}
	*spans = append(*spans, Span{
		Text:   text,
		Color:  col,
		Bold:   bold,
		Italic: italic,
	})
}

func runeAt(s string, i int) (rune, int) {
	r := rune(s[i])
	if r < utf8.RuneSelf {
		return r, 1
	}
	return utf8.DecodeRuneInString(s[i:])
}

var goBuiltinNames = map[string]bool{
	"any": true, "append": true, "bool": true, "byte": true, "cap": true,
	"clear": true, "close": true, "comparable": true, "complex": true,
	"complex64": true, "complex128": true, "copy": true, "delete": true,
	"error": true, "false": true, "float32": true, "float64": true,
	"imag": true, "int": true, "int8": true, "int16": true, "int32": true,
	"int64": true, "iota": true, "len": true, "make": true, "max": true,
	"min": true, "new": true, "nil": true, "panic": true, "print": true,
	"println": true, "real": true, "recover": true, "rune": true,
	"string": true, "true": true, "uint": true, "uint8": true,
	"uint16": true, "uint32": true, "uint64": true, "uintptr": true,
}

var asmRegisterNames = map[string]bool{
	"AH": true, "AL": true, "AX": true, "BH": true, "BL": true, "BP": true,
	"BX": true, "CH": true, "CL": true, "CS": true, "CX": true, "DH": true,
	"DI": true, "DL": true, "DS": true, "DX": true, "ES": true, "FLAGS": true,
	"FP": true, "FS": true, "GS": true, "IP": true, "LR": true, "PC": true,
	"SB": true, "SI": true, "SP": true, "TLS": true, "ZR": true,
	"EAX": true, "EBP": true, "EBX": true, "ECX": true, "EDI": true,
	"EDX": true, "EFLAGS": true, "EIP": true, "ESI": true, "ESP": true,
	"RAX": true, "RBP": true, "RBX": true, "RCX": true, "RDI": true,
	"RDX": true, "RIP": true, "RSB": true, "RSI": true, "RSP": true,
}

type Span struct {
	Text   string
	Color  color.NRGBA
	Italic bool
	Bold   bool
}
