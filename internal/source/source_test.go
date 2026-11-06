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
	sources := Load(Files{}, refs, src, 2)
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
	sources := Load(Files{}, refs, src, 0)
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

func TestLoad_IgnoresLineZero(t *testing.T) {
	src := filepath.Join(t.TempDir(), "a.c")
	if err := os.WriteFile(src, []byte("1\n2\n3\n4\n5\n6\n7\n8\n9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	refs := Refs{}
	refs.Add(src, 0, 0)
	refs.Add(src, 8, 1)
	sources := Load(Files{}, refs, src, 1)
	if len(sources) != 1 || len(sources[0].Blocks) != 1 || sources[0].Blocks[0].From != 7 {
		t.Fatalf("want one block around line 8, got %+v", sources)
	}
}

func TestLoad_ClampsRangeToFile(t *testing.T) {
	src := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(src, []byte("1\n2\n3"), 0o644); err != nil {
		t.Fatal(err)
	}
	refs := Refs{}
	refs.Add(src, 3, 0)
	block := Load(Files{}, refs, src, 2)[0].Blocks[0]
	if block.To != block.From+len(block.Lines) {
		t.Fatalf("range %d-%d holds %d lines", block.From, block.To, len(block.Lines))
	}
}

func TestReplaceEnvironmentVariables(t *testing.T) {
	t.Setenv("X", "/x")
	t.Setenv("GOROOT_TEST", "/goroot")
	for in, want := range map[string]string{
		"$X/a.go":                "/x/a.go",
		"$GOROOT_TEST/src/os.go": "/goroot/src/os.go",
		"$UNSET_LENSM/a.go":      "$UNSET_LENSM/a.go",
		"/plain/a.go":            "/plain/a.go",
	} {
		if got := replaceEnvironmentVariables(in); got != want {
			t.Errorf("replaceEnvironmentVariables(%q) = %q, want %q", in, got, want)
		}
	}
}
