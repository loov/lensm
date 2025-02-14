package main

//go:generate go run update.go

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func main() {
	must0(os.RemoveAll("src"))
	must0(os.Mkdir("src", 0755))

	goroot := strings.TrimSpace(must(run("go", "env", "GOROOT")))
	for _, subdir := range []string{"archive", "bio", "goobj", "objabi", "objfile", "src", "sys", "disasm"} {
		dir := filepath.Join(goroot, "src/cmd/internal", subdir)
		copydir(dir, filepath.Join("src", subdir))
	}
	for _, subdir := range []string{"abi", "goarch", "bisect", "buildcfg", "goexperiment", "saferio", "unsafeheader", "xcoff"} {
		dir := filepath.Join(goroot, "src/internal", subdir)
		copydir(dir, filepath.Join("src", subdir))
	}

	must0(os.WriteFile("src/disasm/expose.go", must(os.ReadFile("expose.go_")), 0644))
	must0(os.Remove("src/abi/abi_test.s"))
}

func copydir(srcdir, dstdir string) {
	must0(os.MkdirAll(dstdir, 0755))
	must0(filepath.WalkDir(srcdir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		base := must(filepath.Rel(srcdir, path))

		if d.IsDir() {
			if d.Name() == "testdata" {
				return fs.SkipDir
			}
			return os.MkdirAll(filepath.Join(dstdir, base), 0755)
		}
		if strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		dstfile := filepath.Join(dstdir, base)
		data := must(os.ReadFile(path))

		data = bytes.ReplaceAll(data, []byte(`	"cmd/internal/`), []byte(`	"loov.dev/lensm/internal/go/src/`))
		data = bytes.ReplaceAll(data, []byte(`import "cmd/internal/`), []byte(`import "loov.dev/lensm/internal/go/src/`))
		data = bytes.ReplaceAll(data, []byte(`	"internal/`), []byte(`	"loov.dev/lensm/internal/go/src/`))
		data = bytes.ReplaceAll(data, []byte(`import "internal/`), []byte(`import "loov.dev/lensm/internal/go/src/`))

		return os.WriteFile(dstfile, data, 0755)
	}))
}

func run(cmd string, args ...string) (string, error) {
	out, err := exec.Command(cmd, args...).Output()
	return string(out), err
}

func must0(err error) {
	if err != nil {
		panic(err)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
