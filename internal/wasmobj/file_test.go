package wasmobj

import (
	"path/filepath"
	"regexp"
	"testing"

	"loov.dev/lensm/internal/disasm"
)

var rxByteInst = regexp.MustCompile(`^BYTE 0x[0-9a-f]{2}$`)

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
		for _, inst := range code.Insts {
			if !rxByteInst.MatchString(inst.Text) {
				t.Fatalf("instruction text = %q", inst.Text)
			}
		}
		return
	}
	t.Fatal("no wasm function with body found")
}
