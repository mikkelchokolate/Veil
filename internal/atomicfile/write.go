package atomicfile

import (
	"os"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/testguard"
)

// test hooks; replaced by tests to inject errors without changing logic.
var (
	createTemp    = os.CreateTemp
	chmod         = os.Chmod
	syncFile      = func(f *os.File) error { return f.Sync() }
	closeFile     = func(f *os.File) error { return f.Close() }
	syncDirectory = func(path string) error {
		dir, err := os.Open(path)
		if err != nil {
			return err
		}
		defer dir.Close()
		return dir.Sync()
	}
)

func Write(path string, body []byte, mode os.FileMode, dirMode os.FileMode) error {
	testguard.CheckPath(path)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	tmp, err := createTemp(dir, ".tmp-*")
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
	if err := chmod(tmpPath, mode); err != nil {
		return err
	}
	if err := syncFile(tmp); err != nil {
		return err
	}
	if err := closeFile(tmp); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	committed = true
	return syncDirectory(dir)
}
