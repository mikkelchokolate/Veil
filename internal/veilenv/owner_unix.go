//go:build !windows

package veilenv

import (
	"fmt"
	"os"
	"syscall"
)

// preserveOwner carries the live file's uid/gid onto the staged copy, so the
// Panel and the privileged helper (root:veil on 0640) can still read it after
// the rename.
func preserveOwner(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	if err := os.Chown(path, int(stat.Uid), int(stat.Gid)); err != nil {
		return fmt.Errorf("preserve owner of %s: %w", path, err)
	}
	return nil
}
