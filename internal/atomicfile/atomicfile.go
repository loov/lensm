package atomicfile

import (
	"os"
	"path/filepath"
)

// atomicWriteFile replaces path with data via a temp file in the same
// directory: the temp file is chmodded (CreateTemp creates it 0600),
// fsynced so a crash cannot leave a truncated file behind the
// already-journaled rename, renamed over the target, and the parent
// directory is best-effort synced so the rename itself survives a crash.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
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
