//go:build unix

package source

import (
	"path/filepath"
	"syscall"
	"testing"
)

// TestLoad_SkipsNonRegularFiles checks that a line table naming a device
// or a FIFO doesn't read forever or block.
func TestLoad_SkipsNonRegularFiles(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skip(err)
	}
	refs := Refs{}
	refs.Add("/dev/zero", 1, 0)
	refs.Add(fifo, 1, 1)
	if sources := Load(refs, "/dev/zero", 0); len(sources) != 0 {
		t.Fatalf("want no sources, got %d", len(sources))
	}
}
