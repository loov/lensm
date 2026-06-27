package wasmobj

import (
	"path/filepath"
	"regexp"
	"testing"

	"loov.dev/lensm/internal/disasm"
)

func TestLoadFormatsBytes(t *testing.T) {
	file, err := Load(filepath.Join("..", "..", "testdata", "c-wasm", "example.wasm"))
	if err != nil {
		t.Fatal(err)
	}

	for _, fn := range file.funcs {
		code := fn.Load(disasm.Options{})
		if len(code.Insts) == 0 {
			continue
		}
		if !regexp.MustCompile(`^BYTE 0x[0-9a-f]{2}$`).MatchString(code.Insts[0].Text) {
			t.Fatalf("instruction text = %q", code.Insts[0].Text)
		}
		return
	}
	t.Fatal("no wasm function with body found")
}
