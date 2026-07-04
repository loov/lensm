package main

import (
	"os"
	"path/filepath"
	"testing"
	"loov.dev/lensm/internal/atomicfile"
)

func TestAtomicWriteFileReplacesContentsAndCleansTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := atomicfile.Write(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "new" {
		t.Fatalf("settings contents = %q, want new", got)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".lensm-settings-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary settings files remain: %v", matches)
	}
}
