//go:build windows

package runtime

func readDiskStats(path string) (diskInfo, error) {
	// Dummy implementation for Windows local development and testing
	return diskInfo{total: 100 * 1024 * 1024 * 1024, used: 50 * 1024 * 1024 * 1024}, nil
}
