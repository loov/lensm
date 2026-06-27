package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	Version  int             `json:"version"`
	Binary   string          `json:"binary,omitempty"`
	Comments []CommentRecord `json:"comments"`
}

type CommentStore struct {
	mu      sync.RWMutex
	path    string
	binary  string
	records map[string]CommentRecord
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
	if store == nil {
		return errors.New("comment store is not initialized")
	}
	text = strings.TrimSpace(text)
	if err := coord.validate(); err != nil {
		return err
	}

	store.mu.Lock()
	key := store.key(coord)
	existing, exists := store.records[key]
	if text == "" {
		if !exists {
			store.mu.Unlock()
			return nil
		}
		delete(store.records, key)
	} else {
		coord = store.normalize(coord)
		if exists && existing.Text == text {
			store.mu.Unlock()
			return nil
		}
		store.records[key] = CommentRecord{
			CommentCoord: coord,
			Text:         text,
			UpdatedAt:    time.Now().UTC(),
		}
	}
	err := store.saveLocked()
	if err != nil {
		if exists {
			store.records[key] = existing
		} else {
			delete(store.records, key)
		}
	}
	store.mu.Unlock()
	return err
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
	for _, rec := range disk.Comments {
		if rec.Text == "" {
			continue
		}
		if rec.Binary == "" {
			rec.Binary = firstNonEmpty(disk.Binary, store.binary)
		}
		rec.PCHex = commentPCHex(rec.CommentCoord)
		if err := rec.CommentCoord.validate(); err != nil {
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

	data, err := json.MarshalIndent(commentsDiskFile{
		Version:  commentsFileVersion,
		Binary:   store.binary,
		Comments: records,
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
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, store.path)
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
	sort.Slice(records, func(i, j int) bool {
		a, b := records[i], records[j]
		if a.Function != b.Function {
			return a.Function < b.Function
		}
		if a.View != b.View {
			return a.View < b.View
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.PC < b.PC
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
