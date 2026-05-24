//go:build windows

package runtime

import "fmt"

func readDiskStats(path string) (diskInfo, error) {
	return diskInfo{}, fmt.Errorf("disk stats not supported on windows")
}
