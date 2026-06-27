package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCommentStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comments.json")
	store, err := NewCommentStore(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.SetSource("main.add", "/tmp/main.go", 12, "source note"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", CommentViewGoAsm, 0x1000, "go asm note"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", CommentViewNativeAsm, 0x1000, "native asm note"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewCommentStore(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.ForSource("main.add", "/tmp/main.go", 12); got != "source note" {
		t.Fatalf("source comment = %q", got)
	}
	if got := reloaded.ForAsm("main.add", CommentViewGoAsm, 0x1000); got != "go asm note" {
		t.Fatalf("go asm comment = %q", got)
	}
	if got := reloaded.ForAsm("main.add", CommentViewNativeAsm, 0x1000); got != "native asm note" {
		t.Fatalf("native asm comment = %q", got)
	}
}

func TestCommentStoreDeletesEmptyComment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comments.json")
	store, err := NewCommentStore(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", CommentViewGoAsm, 0x1000, "note"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", CommentViewGoAsm, 0x1000, "  "); err != nil {
		t.Fatal(err)
	}

	reloaded, err := NewCommentStore(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.ForAsm("main.add", CommentViewGoAsm, 0x1000); got != "" {
		t.Fatalf("deleted comment = %q", got)
	}
	if got := len(reloaded.All()); got != 0 {
		t.Fatalf("comment count = %d", got)
	}
}

func TestCommentStoreSkipsUnchangedCommentWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comments.json")
	store, err := NewCommentStore(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", CommentViewGoAsm, 0x1000, "note"); err != nil {
		t.Fatal(err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	records := store.All()
	if len(records) != 1 {
		t.Fatalf("comment count = %d", len(records))
	}
	updatedAt := records[0].UpdatedAt

	time.Sleep(time.Millisecond)
	if err := store.SetAsm("main.add", CommentViewGoAsm, 0x1000, "note"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("unchanged comment rewrote comments file")
	}
	records = store.All()
	if len(records) != 1 {
		t.Fatalf("comment count = %d", len(records))
	}
	if !records[0].UpdatedAt.Equal(updatedAt) {
		t.Fatalf("updated_at changed from %s to %s", updatedAt, records[0].UpdatedAt)
	}
}

func TestCommentStoreSkipsMissingCommentDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comments.json")
	store, err := NewCommentStore(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", CommentViewGoAsm, 0x1000, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("comments file exists after deleting missing comment: %v", err)
	}
}

func TestCommentStoreRollsBackFailedSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "comments.json")
	store, err := NewCommentStore(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", CommentViewGoAsm, 0x1000, "old"); err != nil {
		t.Fatal(err)
	}

	store.path = filepath.Join(dir, "missing", "comments.json")
	if err := os.WriteFile(filepath.Join(dir, "missing"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", CommentViewGoAsm, 0x1000, "new"); err == nil {
		t.Fatal("updating comment unexpectedly succeeded")
	}
	if got := store.ForAsm("main.add", CommentViewGoAsm, 0x1000); got != "old" {
		t.Fatalf("comment after failed update = %q, want old", got)
	}

	if err := store.SetAsm("main.add", CommentViewGoAsm, 0x1000, ""); err == nil {
		t.Fatal("deleting comment unexpectedly succeeded")
	}
	if got := store.ForAsm("main.add", CommentViewGoAsm, 0x1000); got != "old" {
		t.Fatalf("comment after failed delete = %q, want old", got)
	}
}
