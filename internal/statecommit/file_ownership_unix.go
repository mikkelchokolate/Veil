//go:build !windows

package statecommit

import (
	"os"
	"syscall"
)

func fileMetadata(info os.FileInfo) rotationFileMetadata {
	metadata := rotationFileMetadata{mode: info.Mode().Perm(), uid: -1, gid: -1}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		metadata.uid = int(stat.Uid)
		metadata.gid = int(stat.Gid)
	}
	return metadata
}

func applyFileOwnership(file *os.File, uid, gid int) error {
	if uid < 0 && gid < 0 {
		return nil
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	current := fileMetadata(info)
	if current.uid == uid && current.gid == gid {
		return nil
	}
	return file.Chown(uid, gid)
}
