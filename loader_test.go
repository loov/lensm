package main

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"loov.dev/lensm/internal/disasm"
)

type loaderTestFile struct{ closed *atomic.Int32 }

func (f loaderTestFile) Close() error         { f.closed.Add(1); return nil }
func (f loaderTestFile) Funcs() []disasm.Func { return nil }

// TestLoaderDebouncesWrites checks that watch mode reloads once for a
// burst of writes, and only after the file has been still for a while.
func TestLoaderDebouncesWrites(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the debounce")
	}
	path := filepath.Join(t.TempDir(), "bin")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	var loads, closed atomic.Int32
	l := newLoader(func(string) (disasm.File, error) {
		loads.Add(1)
		return loaderTestFile{&closed}, nil
	}, true)
	defer l.Close()

	l.Request(1, path)
	result := <-l.Results()
	if result.generation != 1 || result.err != nil {
		t.Fatalf("first load = %+v", result)
	}

	// Two writes in quick succession, with distinct mod times.
	start := time.Now()
	for i, data := range []string{"v2", "v3"} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
		mod := start.Add(time.Duration(i+1) * time.Second)
		if err := os.Chtimes(path, mod, mod); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	select {
	case <-l.Results():
		if wait := time.Since(start); wait < 300*time.Millisecond {
			t.Errorf("reloaded after %v, before the debounce", wait)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no reload after the writes")
	}
	time.Sleep(600 * time.Millisecond)
	if n := loads.Load(); n != 2 {
		t.Fatalf("%d loads, want 2", n)
	}
}

// TestLoaderWatchToggle checks that a change made while watching is off
// reloads once watching is turned on.
func TestLoaderWatchToggle(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the debounce")
	}
	path := filepath.Join(t.TempDir(), "bin")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	var closed atomic.Int32
	l := newLoader(func(string) (disasm.File, error) {
		return loaderTestFile{&closed}, nil
	}, false)
	defer l.Close()

	l.Request(1, path)
	<-l.Results()

	mod := time.Now().Add(time.Second)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
	select {
	case <-l.Results():
		t.Fatal("reloaded with watching off")
	case <-time.After(600 * time.Millisecond):
	}

	l.SetWatch(true)
	select {
	case <-l.Results():
	case <-time.After(5 * time.Second):
		t.Fatal("no reload after turning watching on")
	}
}

// TestLoaderWatchPackage checks that watching a package directory
// reloads when any Go file of its module changes, but not one in a
// directory the go command ignores.
func TestLoaderWatchPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the debounce")
	}
	root := t.TempDir()
	pkg := filepath.Join(root, "cmd", "app")
	for path, data := range map[string]string{
		"go.mod":            "module example.com/app\n",
		"cmd/app/main.go":   "package main\n",
		"lib/lib.go":        "package lib\n",
		".hidden/hidden.go": "package hidden\n",
	} {
		path = filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var closed atomic.Int32
	l := newLoader(func(string) (disasm.File, error) {
		return loaderTestFile{&closed}, nil
	}, true)
	defer l.Close()

	l.Request(1, pkg)
	<-l.Results()

	touch := func(path string, mod time.Time) {
		if err := os.Chtimes(filepath.Join(root, path), mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	touch(".hidden/hidden.go", time.Now().Add(time.Second))
	select {
	case <-l.Results():
		t.Fatal("reloaded for a file in an ignored directory")
	case <-time.After(600 * time.Millisecond):
	}

	touch("lib/lib.go", time.Now().Add(2*time.Second))
	select {
	case <-l.Results():
	case <-time.After(5 * time.Second):
		t.Fatal("no reload after a change in another package of the module")
	}
}
