package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const commentsFileVersion = 1

type CommentView string

const (
	CommentViewSource    CommentView = "source"
	CommentViewGoAsm     CommentView = "go_asm"
	CommentViewNativeAsm CommentView = "native_asm"
)

type CommentCoord struct {
	Binary   string      `json:"binary,omitempty"`
	Function string      `json:"function"`
	View     CommentView `json:"view"`
	File     string      `json:"file,omitempty"`
	Line     int         `json:"line,omitempty"`
	PC       uint64      `json:"pc,omitempty"`
	PCHex    string      `json:"pc_hex,omitempty"`
}

type CommentRecord struct {
	CommentCoord
	Text      string    `json:"text"`
	UpdatedAt time.Time `json:"updated_at"`
}

type commentsDiskFile struct {
	Version  int               `json:"version"`
	Binary   string            `json:"binary,omitempty"`
	Comments []json.RawMessage `json:"comments"`
}

type CommentStore struct {
	mu      sync.RWMutex
	path    string
	binary  string
	records map[string]CommentRecord
	// dirty marks in-memory changes not yet written by Flush.
	dirty bool
	// preserved holds records this version doesn't understand (e.g. views
	// added by a newer lensm), kept verbatim so saving doesn't destroy them.
	preserved []json.RawMessage
}

func defaultCommentPath(binaryPath string) string {
	if binaryPath == "" {
		return ""
	}
	return binaryPath + ".lensm-comments.json"
}

func NewCommentStore(path, binaryPath string) (*CommentStore, error) {
	binaryPath = cleanPath(binaryPath)
	store := &CommentStore{
		path:    path,
		binary:  binaryPath,
		records: map[string]CommentRecord{},
	}
	if path == "" {
		return store, nil
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (store *CommentStore) Path() string {
	if store == nil {
		return ""
	}
	return store.path
}

func (store *CommentStore) Get(coord CommentCoord) string {
	if store == nil {
		return ""
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	rec, ok := store.records[store.key(coord)]
	if !ok {
		return ""
	}
	return rec.Text
}

func (store *CommentStore) Set(coord CommentCoord, text string) error {
	return store.set(coord, text, true)
}

// SetBuffered updates the comment in memory and defers the disk write
// until Flush, so typing in an editor doesn't rewrite the sidecar on
// every keystroke.
func (store *CommentStore) SetBuffered(coord CommentCoord, text string) error {
	return store.set(coord, text, false)
}

func (store *CommentStore) set(coord CommentCoord, text string, persist bool) error {
	if store == nil {
		return errors.New("comment store is not initialized")
	}
	text = strings.TrimSpace(text)
	if err := coord.validate(); err != nil {
		return err
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	key := store.key(coord)
	existing, exists := store.records[key]
	if text == "" {
		if !exists {
			return nil
		}
		delete(store.records, key)
	} else {
		coord = store.normalize(coord)
		if exists && existing.Text == text {
			return nil
		}
		store.records[key] = CommentRecord{
			CommentCoord: coord,
			Text:         text,
			UpdatedAt:    time.Now().UTC(),
		}
	}
	wasDirty := store.dirty
	store.dirty = true
	if !persist {
		return nil
	}
	if err := store.saveLocked(); err != nil {
		// Roll back so the in-memory store keeps matching the file.
		if exists {
			store.records[key] = existing
		} else {
			delete(store.records, key)
		}
		store.dirty = wasDirty
		return err
	}
	store.dirty = false
	return nil
}

// Flush writes buffered changes to disk.
func (store *CommentStore) Flush() error {
	if store == nil {
		return nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.dirty {
		return nil
	}
	if err := store.saveLocked(); err != nil {
		return err
	}
	store.dirty = false
	return nil
}

func (store *CommentStore) All() []CommentRecord {
	if store == nil {
		return nil
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	out := make([]CommentRecord, 0, len(store.records))
	for _, rec := range store.records {
		out = append(out, rec)
	}
	sortCommentRecords(out)
	return out
}

func (store *CommentStore) Filter(function string, view CommentView) []CommentRecord {
	if store == nil {
		return nil
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	var out []CommentRecord
	for _, rec := range store.records {
		if function != "" && rec.Function != function {
			continue
		}
		if view != "" && rec.View != view {
			continue
		}
		out = append(out, rec)
	}
	sortCommentRecords(out)
	return out
}

func (store *CommentStore) ForSource(function, file string, line int) string {
	return store.Get(CommentCoord{
		Function: function,
		View:     CommentViewSource,
		File:     file,
		Line:     line,
	})
}

func (store *CommentStore) ForAsm(function string, view CommentView, pc uint64) string {
	return store.Get(CommentCoord{
		Function: function,
		View:     view,
		PC:       pc,
	})
}

func (store *CommentStore) SetSource(function, file string, line int, text string) error {
	return store.Set(CommentCoord{
		Function: function,
		View:     CommentViewSource,
		File:     file,
		Line:     line,
	}, text)
}

func (store *CommentStore) SetAsm(function string, view CommentView, pc uint64, text string) error {
	return store.Set(CommentCoord{
		Function: function,
		View:     view,
		PC:       pc,
	}, text)
}

func (store *CommentStore) load() error {
	data, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var disk commentsDiskFile
	if err := json.Unmarshal(data, &disk); err != nil {
		return fmt.Errorf("load comments: %w", err)
	}
	if disk.Version != 0 && disk.Version != commentsFileVersion {
		return fmt.Errorf("unsupported comments file version %d", disk.Version)
	}
	for _, raw := range disk.Comments {
		var rec CommentRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			store.preserved = append(store.preserved, raw)
			continue
		}
		if rec.Binary == "" {
			rec.Binary = firstNonEmpty(disk.Binary, store.binary)
		}
		rec.PCHex = commentPCHex(rec.CommentCoord)
		if err := rec.CommentCoord.validate(); err != nil {
			store.preserved = append(store.preserved, raw)
			continue
		}
		if rec.Text == "" {
			continue
		}
		store.records[store.key(rec.CommentCoord)] = rec
	}
	return nil
}

func (store *CommentStore) saveLocked() error {
	if store.path == "" {
		return nil
	}
	records := make([]CommentRecord, 0, len(store.records))
	for _, rec := range store.records {
		records = append(records, rec)
	}
	sortCommentRecords(records)

	comments := make([]json.RawMessage, 0, len(records)+len(store.preserved))
	for _, rec := range records {
		raw, err := json.Marshal(rec)
		if err != nil {
			return err
		}
		comments = append(comments, raw)
	}
	comments = append(comments, store.preserved...)

	data, err := json.MarshalIndent(commentsDiskFile{
		Version:  commentsFileVersion,
		Binary:   store.binary,
		Comments: comments,
	}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(store.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".lensm-comments-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	// CreateTemp creates the file 0600; keep the sidecar shareable.
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return err
	}
	// Flush data blocks before the rename, otherwise a crash can leave a
	// zero-length or truncated file behind the already-journaled rename.
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, store.path); err != nil {
		return err
	}
	syncDir(dir)
	return nil
}

// syncDir best-effort flushes directory metadata so a rename survives a
// crash. Errors are ignored: not all platforms support syncing directories.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

func (store *CommentStore) normalize(coord CommentCoord) CommentCoord {
	coord.Binary = firstNonEmpty(coord.Binary, store.binary)
	coord.Binary = cleanPath(coord.Binary)
	coord.PCHex = commentPCHex(coord)
	return coord
}

func (store *CommentStore) key(coord CommentCoord) string {
	coord = store.normalize(coord)
	switch coord.View {
	case CommentViewSource:
		return fmt.Sprintf("%s\x00%s\x00%s\x00%d", coord.Function, coord.View, cleanPath(coord.File), coord.Line)
	case CommentViewGoAsm, CommentViewNativeAsm:
		return fmt.Sprintf("%s\x00%s\x00%x", coord.Function, coord.View, coord.PC)
	default:
		return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%x", coord.Function, coord.View, cleanPath(coord.File), coord.Line, coord.PC)
	}
}

func (coord CommentCoord) validate() error {
	if coord.Function == "" {
		return errors.New("function is required")
	}
	switch coord.View {
	case CommentViewSource:
		if coord.File == "" {
			return errors.New("file is required for source comments")
		}
		if coord.Line <= 0 {
			return errors.New("line must be positive for source comments")
		}
	case CommentViewGoAsm, CommentViewNativeAsm:
	default:
		return fmt.Errorf("unsupported comment view %q", coord.View)
	}
	return nil
}

func sortCommentRecords(records []CommentRecord) {
	slices.SortFunc(records, func(a, b CommentRecord) int {
		if c := cmp.Compare(a.Function, b.Function); c != 0 {
			return c
		}
		if c := cmp.Compare(a.View, b.View); c != 0 {
			return c
		}
		if c := cmp.Compare(a.File, b.File); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Line, b.Line); c != 0 {
			return c
		}
		return cmp.Compare(a.PC, b.PC)
	})
}

func cleanPath(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

func formatPC(pc uint64) string {
	return fmt.Sprintf("0x%x", pc)
}

func commentPCHex(coord CommentCoord) string {
	if coord.View == CommentViewGoAsm || coord.View == CommentViewNativeAsm || coord.PC != 0 {
		return formatPC(coord.PC)
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
