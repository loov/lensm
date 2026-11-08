// Package gobuild builds a Go main package and collects the compiler's
// optimization diagnostics, escapes, inlining and bounds checks among
// them, for the packages of its main module. Building the binary and
// the diagnostics in one go keeps them in sync.
package gobuild

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"loov.dev/lensm/internal/disasm"
)

// Build is a built binary in a temporary directory, removed by Close.
type Build struct {
	Dir    string
	Binary string
	Notes  []disasm.Note
}

func (b *Build) Close() error { return os.RemoveAll(b.Dir) }

// IsPackage reports whether path is a directory with Go files, which
// lensm builds instead of opening as a binary.
func IsPackage(path string) bool {
	matches, _ := filepath.Glob(filepath.Join(path, "*.go"))
	return len(matches) > 0
}

// Run builds the main package in dir with the go command.
func Run(ctx context.Context, dir string) (*Build, error) {
	out, err := goCommand(ctx, dir, "list", "-m", "-f", "{{.Path}}")
	if err != nil {
		return nil, err
	}
	modules := strings.Fields(out)

	tmp, err := os.MkdirTemp("", "lensm-build-")
	if err != nil {
		return nil, err
	}
	b := &Build{Dir: tmp, Binary: filepath.Join(tmp, "bin")}
	// A cached compile writes no diagnostics. The new directory in the
	// flags makes the module's packages recompile, and only those.
	diag := filepath.Join(tmp, "diag")
	args := []string{"build", "-o", b.Binary}
	for _, mod := range modules {
		args = append(args, "-gcflags="+mod+"/...=-json=0,"+diag)
	}
	if _, err := goCommand(ctx, dir, append(args, ".")...); err != nil {
		_ = b.Close()
		return nil, err
	}
	b.Notes, err = readNotes(diag)
	if err != nil {
		_ = b.Close()
		return nil, err
	}
	return b, nil
}

func goCommand(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("go %s: %w", args[0], err)
		}
		return "", fmt.Errorf("go %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}

// The compiler writes one file per source file, a header line and
// then a line per diagnostic in the LSP format, with lines and columns
// counted from 1.
type position struct{ Line, Character int }

type lspRange struct{ Start position }

type lspDiagnostic struct {
	File               string // header only
	Range              lspRange
	Code               string
	Message            string
	RelatedInformation []struct {
		Location struct {
			URI   string
			Range lspRange
		}
		Message string
	}
}

func readNotes(dir string) ([]disasm.Note, error) {
	var notes []disasm.Note
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && path == dir {
			return fs.SkipAll // the module had nothing to compile
		}
		if err != nil || d.IsDir() || filepath.Ext(path) != ".json" {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		dec := json.NewDecoder(f)
		var file string
		for {
			var ld lspDiagnostic
			if err := dec.Decode(&ld); err == io.EOF {
				return nil
			} else if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if ld.File != "" {
				file = ld.File
				continue
			}
			note := disasm.Note{
				File:    file,
				Line:    ld.Range.Start.Line,
				Col:     ld.Range.Start.Character,
				Code:    ld.Code,
				Message: ld.Message,
			}
			for _, r := range ld.RelatedInformation {
				note.Related = append(note.Related, disasm.Note{
					File:    uriPath(r.Location.URI),
					Line:    r.Location.Range.Start.Line,
					Col:     r.Location.Range.Start.Character,
					Message: r.Message,
				})
			}
			// The escape analysis repeats a note without a message.
			if last := len(notes) - 1; note.Message == "" && last >= 0 &&
				notes[last].Code == note.Code && notes[last].Line == note.Line && notes[last].Col == note.Col {
				continue
			}
			notes = append(notes, note)
		}
	})
	return notes, err
}

func uriPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	path := u.Path
	if len(path) > 2 && path[2] == ':' {
		path = path[1:] // /C:/dir on Windows
	}
	return filepath.FromSlash(path)
}

// ModTime is the newest modification time of the Go files, go.mod and
// go.sum in the module that contains dir, for watch mode to rebuild on.
// It walks the whole module on every call; other workspace modules are
// not watched.
func ModTime(dir string) (time.Time, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return time.Time{}, err
	}
	root := moduleRoot(dir)
	var newest time.Time
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			// The go command ignores these directories too.
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(name) != ".go" && name != "go.mod" && name != "go.sum" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	})
	return newest, err
}

func moduleRoot(dir string) string {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return dir
		}
		d = parent
	}
}
