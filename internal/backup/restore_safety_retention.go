package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

func PruneRestoreSafetyFiles(statePath, keyPath, databasePath string, keep int) ([]string, error) {
	if keep < 0 {
		return nil, fmt.Errorf("restore safety retention must be nonnegative")
	}
	var deleted []string
	for _, target := range []string{statePath, keyPath, databasePath} {
		if target == "" {
			continue
		}
		matches, err := filepath.Glob(target + ".pre-restore-*")
		if err != nil {
			return nil, err
		}
		type candidate struct {
			path string
			mod  int64
		}
		items := make([]candidate, 0, len(matches))
		for _, match := range matches {
			info, err := os.Lstat(match)
			if err != nil {
				return nil, err
			}
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("restore safety path is not a regular file")
			}
			items = append(items, candidate{path: match, mod: info.ModTime().UnixNano()})
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].mod == items[j].mod {
				return items[i].path > items[j].path
			}
			return items[i].mod > items[j].mod
		})
		deleteFrom := keep
		if deleteFrom > len(items) {
			deleteFrom = len(items)
		}
		for _, item := range items[deleteFrom:] {
			if err := secureRemoveRestoreSafetyFile(item.path); err != nil {
				return deleted, err
			}
			deleted = append(deleted, filepath.Base(item.path))
		}
	}
	return deleted, nil
}

func secureRemoveRestoreSafetyFile(path string) error {
	if !strings.Contains(filepath.Base(path), ".pre-restore-") {
		return fmt.Errorf("refusing to delete non-safety file")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		if err != nil {
			return err
		}
		return fmt.Errorf("restore safety path is not a regular file")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		_ = file.Close()
		return fmt.Errorf("restore safety path has unsafe link count")
	}
	zeros := make([]byte, 1024*1024)
	remaining := info.Size()
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return err
	}
	for remaining > 0 {
		chunk := int64(len(zeros))
		if remaining < chunk {
			chunk = remaining
		}
		if _, err := file.Write(zeros[:chunk]); err != nil {
			_ = file.Close()
			return err
		}
		remaining -= chunk
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
