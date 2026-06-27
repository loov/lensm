package main

import (
	"image/color"
	"strings"
	"testing"
)

func TestNormalizeSyntaxStyle(t *testing.T) {
	if got := NormalizeSyntaxStyle("jetbrains-dark"); got != SyntaxStyleDarcula {
		t.Fatalf("expected darcula alias, got %q", got)
	}
	if got := NormalizeSyntaxStyle("unknown"); got != SyntaxStyleGoLand {
		t.Fatalf("expected unknown style to fall back to goland, got %q", got)
	}
}

func TestHighlightGoLine(t *testing.T) {
	palette := testSyntaxPalette()
	spans := HighlightGoLine(`if len(s) > 0 { return "x" // ok }`, palette)

	assertSpanColor(t, spans, "if", palette.Keyword)
	assertSpanColor(t, spans, "len", palette.Builtin)
	assertSpanColor(t, spans, "0", palette.Number)
	assertSpanColor(t, spans, `"x"`, palette.String)
	assertSpanStyle(t, spans, "// ok }", palette.Comment, false, true)
}

func TestHighlightAsmLine(t *testing.T) {
	palette := testSyntaxPalette()
	spans := HighlightAsmLine("CALL runtime.morestack_noctxt(SB); tail", "runtime.morestack_noctxt", palette)

	assertSpanStyle(t, spans, "CALL", palette.Mnemonic, true, false)
	assertSpanColor(t, spans, "runtime.morestack_noctxt", palette.CallTarget)
	assertSpanColor(t, spans, "SB", palette.Register)
	assertSpanStyle(t, spans, "; tail", palette.Comment, false, true)
}

func TestHighlightNativeAsmLine(t *testing.T) {
	palette := testSyntaxPalette()
	spans := HighlightAsmLine("MOVQ $0X10, %RAX", "", palette)

	assertSpanStyle(t, spans, "MOVQ", palette.Mnemonic, true, false)
	assertSpanColor(t, spans, "$0X10", palette.Number)
	assertSpanColor(t, spans, "%RAX", palette.Register)
}

func testSyntaxPalette() SyntaxPalette {
	return SyntaxPalette{
		Plain:      testColor(1),
		Keyword:    testColor(2),
		Builtin:    testColor(3),
		String:     testColor(4),
		Number:     testColor(5),
		Comment:    testColor(6),
		Operator:   testColor(7),
		Register:   testColor(8),
		Mnemonic:   testColor(9),
		Symbol:     testColor(10),
		LineNumber: testColor(11),
		CallTarget: testColor(12),
	}
}

func testColor(v uint8) color.NRGBA {
	return color.NRGBA{R: v, G: v, B: v, A: 0xff}
}

func assertSpanColor(t *testing.T, spans []SourceSpan, text string, col color.NRGBA) {
	t.Helper()
	span, ok := findSpan(spans, text)
	if !ok {
		t.Fatalf("span %q not found in %q", text, joinedSpanText(spans))
	}
	if span.Color != col {
		t.Fatalf("span %q color = %#v, want %#v", text, span.Color, col)
	}
}

func assertSpanStyle(t *testing.T, spans []SourceSpan, text string, col color.NRGBA, bold, italic bool) {
	t.Helper()
	span, ok := findSpan(spans, text)
	if !ok {
		t.Fatalf("span %q not found in %q", text, joinedSpanText(spans))
	}
	if span.Color != col || span.Bold != bold || span.Italic != italic {
		t.Fatalf("span %q style = %#v, bold=%v italic=%v; want %#v, bold=%v italic=%v",
			text, span.Color, span.Bold, span.Italic, col, bold, italic)
	}
}

func findSpan(spans []SourceSpan, text string) (SourceSpan, bool) {
	for _, span := range spans {
		if span.Text == text {
			return span, true
		}
	}
	return SourceSpan{}, false
}

func joinedSpanText(spans []SourceSpan) string {
	var b strings.Builder
	for _, span := range spans {
		b.WriteString(span.Text)
	}
	return b.String()
}
