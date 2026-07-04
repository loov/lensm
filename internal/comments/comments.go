package comments

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"loov.dev/lensm/internal/atomicfile"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const commentsFileVersion = 1

type View string

const (
	ViewSource    View = "source"
	ViewGoAsm     View = "go_asm"
	ViewNativeAsm View = "native_asm"
)

type Coord struct {
	Binary   string `json:"binary,omitempty"`
	Function string `json:"function"`
	View     View   `json:"view"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	PC       uint64 `json:"pc,omitempty"`
	PCHex    string `json:"pc_hex,omitempty"`
}

type Record struct {
	Coord
	Text      string    `json:"text"`
	UpdatedAt time.Time `json:"updated_at"`
}

type commentsDiskFile struct {
	Version  int               `json:"version"`
	Binary   string            `json:"binary,omitempty"`
	Comments []json.RawMessage `json:"comments"`
}

type Store struct {
	mu      sync.RWMutex
	path    string
	binary  string
	records map[string]Record
	// touched marks keys mutated since the last sync with the file;
	// saves merge the on-disk state for all other keys, so another lensm
	// process writing the same sidecar loses only conflicting edits to
	// the same record, not its whole set of comments.
	touched map[string]bool
	// dirty marks in-memory changes not yet written by Flush.
	dirty bool
	// preserved holds records this version doesn't understand (e.g. views
	// added by a newer lensm), kept verbatim so saving doesn't destroy them.
	preserved []json.RawMessage
}

func DefaultPath(binaryPath string) string {
	if binaryPath == "" {
		return ""
	}
	return binaryPath + ".lensm-comments.json"
}

func Open(path, binaryPath string) (*Store, error) {
	binaryPath = CleanPath(binaryPath)
	store := &Store{
		path:    path,
		binary:  binaryPath,
		records: map[string]Record{},
		touched: map[string]bool{},
	}
	if path == "" {
		return store, nil
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (store *Store) Path() string {
	if store == nil {
		return ""
	}
	return store.path
}

func (store *Store) Get(coord Coord) string {
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

func (store *Store) Set(coord Coord, text string) error {
	return store.set(coord, text, true)
}

// SetBuffered updates the comment in memory and defers the disk write
// until Flush, so typing in an editor doesn't rewrite the sidecar on
// every keystroke.
func (store *Store) SetBuffered(coord Coord, text string) error {
	return store.set(coord, text, false)
}

func (store *Store) set(coord Coord, text string, persist bool) error {
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
		// Even when the record isn't in memory it may exist on disk,
		// written by another process sharing the sidecar; fall through so
		// the deletion is recorded and the next save drops it instead of
		// re-adopting it.
		delete(store.records, key)
	} else {
		coord = store.Normalize(coord)
		if exists && existing.Text == text {
			return nil
		}
		store.records[key] = Record{
			Coord:     coord,
			Text:      text,
			UpdatedAt: time.Now().UTC(),
		}
	}
	wasTouched := store.touched[key]
	store.touched[key] = true
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
		if !wasTouched {
			delete(store.touched, key)
		}
		store.dirty = wasDirty
		return err
	}
	store.dirty = false
	return nil
}

// Flush writes buffered changes to disk.
func (store *Store) Flush() error {
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

func (store *Store) All() []Record {
	if store == nil {
		return nil
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	out := make([]Record, 0, len(store.records))
	for _, rec := range store.records {
		out = append(out, rec)
	}
	sortCommentRecords(out)
	return out
}

func (store *Store) Filter(function string, view View) []Record {
	if store == nil {
		return nil
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	var out []Record
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

func (store *Store) ForSource(function, file string, line int) string {
	return store.Get(Coord{
		Function: function,
		View:     ViewSource,
		File:     file,
		Line:     line,
	})
}

func (store *Store) ForAsm(function string, view View, pc uint64) string {
	return store.Get(Coord{
		Function: function,
		View:     view,
		PC:       pc,
	})
}

func (store *Store) SetSource(function, file string, line int, text string) error {
	return store.Set(Coord{
		Function: function,
		View:     ViewSource,
		File:     file,
		Line:     line,
	}, text)
}

func (store *Store) SetAsm(function string, view View, pc uint64, text string) error {
	return store.Set(Coord{
		Function: function,
		View:     view,
		PC:       pc,
	}, text)
}

func (store *Store) load() error {
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
		var rec Record
		if err := json.Unmarshal(raw, &rec); err != nil {
			store.preserved = append(store.preserved, raw)
			continue
		}
		if rec.Binary == "" {
			rec.Binary = firstNonEmpty(disk.Binary, store.binary)
		}
		rec.PCHex = commentPCHex(rec.Coord)
		if err := rec.Coord.validate(); err != nil {
			store.preserved = append(store.preserved, raw)
			continue
		}
		if rec.Text == "" {
			continue
		}
		store.records[store.key(rec.Coord)] = rec
	}
	return nil
}

func (store *Store) saveLocked() error {
	if store.path == "" {
		return nil
	}
	store.mergeExternalLocked()
	records := make([]Record, 0, len(store.records))
	for _, rec := range store.records {
		records = append(records, rec)
	}
	sortCommentRecords(records)

	// Don't create a file just to record that nothing exists, e.g. after
	// deleting a comment that was never saved.
	if len(records) == 0 && len(store.preserved) == 0 {
		if _, err := os.Stat(store.path); errors.Is(err, os.ErrNotExist) {
			clear(store.touched)
			return nil
		}
	}

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

	if err := os.MkdirAll(filepath.Dir(store.path), 0o755); err != nil {
		return err
	}
	if err := atomicfile.Write(store.path, data, 0o644); err != nil {
		return err
	}
	// The in-memory state now matches the file again.
	clear(store.touched)
	return nil
}

// mergeExternalLocked folds records another lensm process wrote to the
// sidecar into the in-memory store before a full-file rewrite. Keys
// mutated locally since the last sync win; everything else — additions,
// edits, and deletions from the other process — is adopted, so two
// processes sharing a sidecar lose at most conflicting edits to the
// same record instead of each other's whole comment sets.
func (store *Store) mergeExternalLocked() {
	data, err := os.ReadFile(store.path)
	if err != nil {
		return // nothing on disk to merge
	}
	var disk commentsDiskFile
	if err := json.Unmarshal(data, &disk); err != nil {
		return
	}
	if disk.Version != 0 && disk.Version != commentsFileVersion {
		return
	}

	merged := make(map[string]Record, len(store.records))
	for key, rec := range store.records {
		if store.touched[key] {
			merged[key] = rec
		}
	}
	store.preserved = nil
	for _, raw := range disk.Comments {
		var rec Record
		if err := json.Unmarshal(raw, &rec); err != nil {
			store.preserved = append(store.preserved, raw)
			continue
		}
		if rec.Binary == "" {
			rec.Binary = firstNonEmpty(disk.Binary, store.binary)
		}
		rec.PCHex = commentPCHex(rec.Coord)
		if err := rec.Coord.validate(); err != nil {
			store.preserved = append(store.preserved, raw)
			continue
		}
		if rec.Text == "" {
			continue
		}
		key := store.key(rec.Coord)
		if store.touched[key] {
			continue
		}
		merged[key] = rec
	}
	store.records = merged
}

func (store *Store) Normalize(coord Coord) Coord {
	coord.Binary = firstNonEmpty(coord.Binary, store.binary)
	coord.Binary = CleanPath(coord.Binary)
	coord.PCHex = commentPCHex(coord)
	return coord
}

func (store *Store) key(coord Coord) string {
	coord = store.Normalize(coord)
	switch coord.View {
	case ViewSource:
		return fmt.Sprintf("%s\x00%s\x00%s\x00%d", coord.Function, coord.View, CleanPath(coord.File), coord.Line)
	case ViewGoAsm, ViewNativeAsm:
		return fmt.Sprintf("%s\x00%s\x00%x", coord.Function, coord.View, coord.PC)
	default:
		return fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%x", coord.Function, coord.View, CleanPath(coord.File), coord.Line, coord.PC)
	}
}

func (coord Coord) validate() error {
	if coord.Function == "" {
		return errors.New("function is required")
	}
	switch coord.View {
	case ViewSource:
		if coord.File == "" {
			return errors.New("file is required for source comments")
		}
		if coord.Line <= 0 {
			return errors.New("line must be positive for source comments")
		}
	case ViewGoAsm, ViewNativeAsm:
	default:
		return fmt.Errorf("unsupported comment view %q", coord.View)
	}
	return nil
}

func sortCommentRecords(records []Record) {
	slices.SortFunc(records, func(a, b Record) int {
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

func CleanPath(path string) string {
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(path)
}

func FormatPC(pc uint64) string {
	return fmt.Sprintf("0x%x", pc)
}

func commentPCHex(coord Coord) string {
	if coord.View == ViewGoAsm || coord.View == ViewNativeAsm || coord.PC != 0 {
		return FormatPC(coord.PC)
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
