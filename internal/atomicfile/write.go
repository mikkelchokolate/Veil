package atomicfile

import (
	"os"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/testguard"
)

// test hooks; replaced by tests to inject errors without changing logic.
var (
	createTemp = os.CreateTemp
	// chmodFile and preserveOwner operate on the open temp descriptor, not the
	// .tmp-* path: the parent directory is service-writable, so the leaf name
	// can be swapped for a symlink between a path-based stat and a path-based
	// chmod/chown — redirecting root's metadata write onto an attacker-chosen
	// target (#1219). The descriptor stays pinned to the inode createTemp made.
	chmodFile     = func(f *os.File, mode os.FileMode) error { return f.Chmod(mode) }
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
	if err := chmodFile(tmp, mode); err != nil {
		return err
	}
	if err := preserveOwner(tmp, path); err != nil {
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
