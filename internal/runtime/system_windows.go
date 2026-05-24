//go:build windows

package runtime

import "errors"

func readDiskStats(path string) (diskInfo, error) {
	return diskInfo{}, errors.New("disk stats not implemented on windows")
}
