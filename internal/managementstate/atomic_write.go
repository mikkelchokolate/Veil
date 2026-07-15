package managementstate

import (
	"os"
	"path/filepath"
	"runtime"
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
		if previous.uid >= 0 || previous.gid >= 0 {
			_ = tmp.Chown(previous.uid, previous.gid)
		}
		_ = tmp.Chmod(previous.mode)
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
	bestEffortSyncStoreDirectory(dir)
	return nil
}

func bestEffortSyncStoreDirectory(path string) {
	if runtime.GOOS == "windows" {
		return
	}
	dir, err := os.Open(path)
	if err != nil {
		return
	}
	defer dir.Close()
	_ = dir.Sync()
}
