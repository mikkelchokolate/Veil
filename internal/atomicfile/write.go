package atomicfile

import (
	"os"
	"path/filepath"
)

// test hooks; replaced by tests to inject errors without changing logic.
var (
	createTemp = os.CreateTemp
	chmod      = os.Chmod
	closeFile  = func(f *os.File) error { return f.Close() }
)

func Write(path string, body []byte, mode os.FileMode, dirMode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	tmp, err := createTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := closeFile(tmp); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := chmod(tmpPath, mode); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}
