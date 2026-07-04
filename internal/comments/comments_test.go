package comments

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCommentStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comments.json")
	store, err := Open(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.SetSource("main.add", "/tmp/main.go", 12, "source note"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", ViewGoAsm, 0x1000, "go asm note"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", ViewNativeAsm, 0x1000, "native asm note"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Open(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.ForSource("main.add", "/tmp/main.go", 12); got != "source note" {
		t.Fatalf("source comment = %q", got)
	}
	if got := reloaded.ForAsm("main.add", ViewGoAsm, 0x1000); got != "go asm note" {
		t.Fatalf("go asm comment = %q", got)
	}
	if got := reloaded.ForAsm("main.add", ViewNativeAsm, 0x1000); got != "native asm note" {
		t.Fatalf("native asm comment = %q", got)
	}
}

func TestCommentStoreDeletesEmptyComment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comments.json")
	store, err := Open(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", ViewGoAsm, 0x1000, "note"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", ViewGoAsm, 0x1000, "  "); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Open(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.ForAsm("main.add", ViewGoAsm, 0x1000); got != "" {
		t.Fatalf("deleted comment = %q", got)
	}
	if got := len(reloaded.All()); got != 0 {
		t.Fatalf("comment count = %d", got)
	}
}

func TestCommentStoreSkipsUnchangedCommentWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comments.json")
	store, err := Open(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", ViewGoAsm, 0x1000, "note"); err != nil {
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
	if err := store.SetAsm("main.add", ViewGoAsm, 0x1000, "note"); err != nil {
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

func TestCommentStorePreservesUnknownRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comments.json")
	seed := `{
  "version": 1,
  "comments": [
    {"function": "main.add", "view": "hologram", "pc": 16, "text": "from the future"},
    {"function": "main.add", "view": "go_asm", "pc_hex": "0x1000", "pc": 4096, "text": "known"}
  ]
}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := Open(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	// Trigger a full rewrite of the file.
	if err := store.SetAsm("main.add", ViewGoAsm, 0x2000, "new note"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("hologram")) {
		t.Fatalf("unknown-view record dropped on save:\n%s", data)
	}
	if !bytes.Contains(data, []byte("known")) || !bytes.Contains(data, []byte("new note")) {
		t.Fatalf("known records missing after save:\n%s", data)
	}
}

func TestCommentStoreSaveKeepsFileShareable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comments.json")
	store, err := Open(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", ViewGoAsm, 0x1000, "note"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("comments file mode = %o, want 644", got)
	}
}

func TestCommentStoreSkipsMissingCommentDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comments.json")
	store, err := Open(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", ViewGoAsm, 0x1000, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("comments file exists after deleting missing comment: %v", err)
	}
}

func TestCommentStoreMergesConcurrentProcessWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "comments.json")
	first, err := Open(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Open(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}

	// Each store simulates a separate lensm process holding a stale
	// full-file snapshot of the same sidecar.
	if err := first.SetAsm("main.add", ViewGoAsm, 0x1000, "from first"); err != nil {
		t.Fatal(err)
	}
	if err := second.SetAsm("main.add", ViewGoAsm, 0x2000, "from second"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Open(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.ForAsm("main.add", ViewGoAsm, 0x1000); got != "from first" {
		t.Fatalf("first process's comment = %q, want it to survive the second save", got)
	}
	if got := reloaded.ForAsm("main.add", ViewGoAsm, 0x2000); got != "from second" {
		t.Fatalf("second process's comment = %q", got)
	}

	// A deletion must not be resurrected by the other process's stale copy.
	if err := first.SetAsm("main.add", ViewGoAsm, 0x2000, ""); err != nil {
		t.Fatal(err)
	}
	reloaded, err = Open(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.ForAsm("main.add", ViewGoAsm, 0x2000); got != "" {
		t.Fatalf("deleted comment resurrected: %q", got)
	}
}

func TestCommentStoreRollsBackFailedSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "comments.json")
	store, err := Open(path, "/tmp/app")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", ViewGoAsm, 0x1000, "old"); err != nil {
		t.Fatal(err)
	}

	store.path = filepath.Join(dir, "missing", "comments.json")
	if err := os.WriteFile(filepath.Join(dir, "missing"), []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.SetAsm("main.add", ViewGoAsm, 0x1000, "new"); err == nil {
		t.Fatal("updating comment unexpectedly succeeded")
	}
	if got := store.ForAsm("main.add", ViewGoAsm, 0x1000); got != "old" {
		t.Fatalf("comment after failed update = %q, want old", got)
	}

	if err := store.SetAsm("main.add", ViewGoAsm, 0x1000, ""); err == nil {
		t.Fatal("deleting comment unexpectedly succeeded")
	}
	if got := store.ForAsm("main.add", ViewGoAsm, 0x1000); got != "old" {
		t.Fatalf("comment after failed delete = %q, want old", got)
	}
}
