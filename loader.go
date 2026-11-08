package main

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/gobuild"
	"loov.dev/lensm/internal/goobj"
)

type fileLoadRequest struct {
	generation uint64
	path       string
}

type fileLoadResult struct {
	generation uint64
	file       disasm.File
	err        error
}

// loader loads disassembly files on its own goroutine and, while watch
// is on, polls the current path for modifications with a debounce so
// a binary is not reloaded mid-write.
type loader struct {
	loadFile func(string) (disasm.File, error)
	watch    atomic.Bool
	requests chan fileLoadRequest
	results  chan fileLoadResult
	stop     chan struct{}
}

func newLoader(loadFile func(string) (disasm.File, error), watch bool) *loader {
	l := &loader{
		loadFile: loadFile,
		requests: make(chan fileLoadRequest, 1),
		results:  make(chan fileLoadResult, 1),
		stop:     make(chan struct{}),
	}
	l.watch.Store(watch)
	go l.run()
	return l
}

func (l *loader) Results() <-chan fileLoadResult { return l.results }

// SetWatch turns watching on or off. Turning it on reloads a binary
// that changed while it was off, since the last load's mod time stays.
func (l *loader) SetWatch(on bool) { l.watch.Store(on) }

func (l *loader) Close() { close(l.stop) }

// Request replaces any queued request; only the latest path matters.
// Main event loop only.
func (l *loader) Request(generation uint64, path string) {
	select {
	case <-l.requests:
	default:
	}
	l.requests <- fileLoadRequest{generation: generation, path: path}
}

// finish publishes a result, replacing an unconsumed previous one and
// closing its file.
func (l *loader) finish(result fileLoadResult) {
	select {
	case old := <-l.results:
		if old.file != nil {
			_ = old.file.Close()
		}
	default:
	}
	l.results <- result
}

func (l *loader) run() {
	var lastModTime time.Time
	var pendingModTime time.Time
	var pendingSince time.Time
	var path string
	var generation uint64
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()

	load := func(force bool, now time.Time) {
		if path == "" {
			return
		}

		modTime, err := modTime(path)
		if err != nil {
			l.finish(fileLoadResult{generation: generation, err: err})
			return
		}
		if !force && modTime.Equal(lastModTime) {
			return
		}
		if !force {
			if !modTime.Equal(pendingModTime) {
				pendingModTime = modTime
				pendingSince = now
				return
			}
			if now.Sub(pendingSince) < 300*time.Millisecond {
				return
			}
		}
		lastModTime = modTime
		pendingModTime = time.Time{}

		file, err := l.loadFile(path)
		l.finish(fileLoadResult{generation: generation, file: file, err: err})
	}

	for {
		select {
		case req := <-l.requests:
			path = strings.TrimSpace(req.path)
			generation = req.generation
			lastModTime = time.Time{}
			pendingModTime = time.Time{}
			load(true, time.Now())
		case now := <-tick.C:
			if l.watch.Load() {
				load(false, now)
			}
		case <-l.stop:
			return
		}
	}
}

// modTime is when path last changed, for a package directory when
// any file of its module did.
func modTime(path string) (time.Time, error) {
	if gobuild.IsPackage(path) {
		return gobuild.ModTime(path)
	}
	stat, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	return stat.ModTime(), nil
}

// loadFile opens path, building it first when it is a package
// directory, returning a nil interface on failure rather than
// a nil *goobj.File that would compare non-nil.
func loadFile(path string) (disasm.File, error) {
	if gobuild.IsPackage(path) {
		return loadPackage(path)
	}
	file, err := goobj.Load(path)
	if err != nil {
		return nil, err
	}
	return file, nil
}

func loadPackage(dir string) (disasm.File, error) {
	build, err := gobuild.Run(context.Background(), dir)
	if err != nil {
		return nil, err
	}
	file, err := goobj.Load(build.Binary)
	if err != nil {
		_ = build.Close()
		return nil, err
	}
	file.SetNotes(build.Notes)
	return builtFile{file, build}, nil
}

// builtFile is a binary lensm built, with the compiler diagnostics from
// the same build. Close removes the binary.
type builtFile struct {
	*goobj.File
	build *gobuild.Build
}

func (f builtFile) Close() error {
	err := f.File.Close()
	_ = f.build.Close()
	return err
}
