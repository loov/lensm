package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_TruncatedFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "short.go")
	if err := os.WriteFile(src, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refs := Refs{}
	refs.Add(src, 40, 0) // line table says 40, file has 1 line
	sources := Load(refs, src, 2)
	if len(sources) != 1 || len(sources[0].Blocks) != 0 {
		t.Fatalf("want one source with no blocks, got %+v", sources)
	}
}

func TestLoad_RelatesLines(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.go")
	if err := os.WriteFile(src, []byte("1\n2\n3\n4\n5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refs := Refs{}
	refs.Add(src, 3, 0)
	refs.Add(src, 3, 1)
	refs.Add(src, 4, 5)
	sources := Load(refs, src, 0)
	if len(sources) != 1 || len(sources[0].Blocks) != 1 {
		t.Fatalf("got %+v", sources)
	}
	block := sources[0].Blocks[0]
	if block.From != 3 || len(block.Lines) != 2 {
		t.Fatalf("block = %+v", block)
	}
	if got := block.Related[0]; len(got) != 1 || got[0].From != 0 || got[0].To != 2 {
		t.Fatalf("line 3 related = %+v", got)
	}
	if got := block.Related[1]; len(got) != 1 || got[0].From != 5 || got[0].To != 6 {
		t.Fatalf("line 4 related = %+v", got)
	}
}
