package managementstate

import (
	"fmt"
	"os"
	"path/filepath"
)

// chownFile and chmodFile are seams for failure injection in tests; production
// uses the real os.File methods.
var (
	chownFile = func(file *os.File, uid, gid int) error { return file.Chown(uid, gid) }
	chmodFile = func(file *os.File, mode os.FileMode) error { return file.Chmod(mode) }
)

func writeStoreFileAtomic(path string, body []byte, previous *fileInfo) error {
	return writeStoreFileAtomicWithSync(path, body, previous, func(file *os.File) error {
		return file.Sync()
	})
}

func writeStoreFileAtomicWithSync(path string, body []byte, previous *fileInfo, syncFile func(*os.File) error) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		_ = tmp.Close()
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(body); err != nil {
		return err
	}
	if previous != nil {
		// Fail closed: a state file written without the preserved
		// ownership/mode can lock the service account out of its own state.
		if previous.uid >= 0 || previous.gid >= 0 {
			if err := chownFile(tmp, previous.uid, previous.gid); err != nil {
				return fmt.Errorf("preserve state file ownership: %w", err)
			}
		}
		if err := chmodFile(tmp, previous.mode); err != nil {
			return fmt.Errorf("preserve state file mode: %w", err)
		}
	}
	if err := syncFile(tmp); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	committed = true
	return syncStoreDirectory(dir)
}

func syncStoreDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
