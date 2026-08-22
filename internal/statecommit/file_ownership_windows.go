//go:build windows

package statecommit

import "os"

func fileMetadata(info os.FileInfo) rotationFileMetadata {
	return rotationFileMetadata{mode: info.Mode().Perm(), uid: -1, gid: -1}
}

func applyFileOwnership(_ *os.File, _, _ int) error { return nil }
