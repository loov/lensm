package main

import (
	"os"
	"strings"
	"time"

	"loov.dev/lensm/internal/disasm"
	"loov.dev/lensm/internal/goobj"
	"loov.dev/lensm/internal/wasmobj"
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

func loadDisasmFile(path string) (disasm.File, error) {
	if workInProgressWASM {
		return wasmobj.Load(path)
	}
	return goobj.Load(path)
}

// loader loads disassembly files on its own goroutine and, when watch
// is set, polls the current path for modifications with a debounce so
// a binary is not reloaded mid-write.
type loader struct {
	loadFile func(string) (disasm.File, error)
	watch    bool
	requests chan fileLoadRequest
	results  chan fileLoadResult
	stop     chan struct{}
}

func newLoader(loadFile func(string) (disasm.File, error), watch bool) *loader {
	l := &loader{
		loadFile: loadFile,
		watch:    watch,
		requests: make(chan fileLoadRequest, 1),
		results:  make(chan fileLoadResult, 1),
		stop:     make(chan struct{}),
	}
	go l.run()
	return l
}

func (l *loader) Results() <-chan fileLoadResult { return l.results }

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

		stat, err := os.Stat(path)
		if err != nil {
			l.finish(fileLoadResult{generation: generation, err: err})
			return
		}
		if !force && stat.ModTime().Equal(lastModTime) {
			return
		}
		if !force {
			if !stat.ModTime().Equal(pendingModTime) {
				pendingModTime = stat.ModTime()
				pendingSince = now
				return
			}
			if now.Sub(pendingSince) < 300*time.Millisecond {
				return
			}
		}
		lastModTime = stat.ModTime()
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
			if l.watch {
				load(false, now)
			}
		case <-l.stop:
			return
		}
	}
}
