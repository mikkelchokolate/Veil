package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DirSizeInfo holds disk usage for a directory.
type DirSizeInfo struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
	SizeHuman string `json:"sizeHuman"`
}

// DiskStats holds disk usage for Veil-managed directories.
type DiskStats struct {
	Dirs []DirSizeInfo `json:"dirs"`
}

// veilDirs lists Veil-managed directories to measure.
var veilDirs = []string{
	"/var/lib/veil",
	"/etc/veil",
	"/var/log",
}

// readDirDiskStats returns disk usage for Veil-managed directories.
func readDirDiskStats() DiskStats {
	stats := DiskStats{}
	for _, dir := range veilDirs {
		d := DirSizeInfo{Path: dir}
		d.SizeBytes = dirSizeRecursive(dir)
		d.SizeHuman = formatBytes(d.SizeBytes)
		stats.Dirs = append(stats.Dirs, d)
	}
	return stats
}

// DirSize returns disk usage for directories.
func DirSize(root string) []DirSizeInfo {
	var result []DirSizeInfo
	entries, err := os.ReadDir(root)
	if err != nil {
		return result
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(root, entry.Name())
		size := dirSizeRecursive(path)
		result = append(result, DirSizeInfo{
			Path:      path,
			SizeBytes: size,
			SizeHuman: formatBytes(size),
		})
	}
	return result
}

func dirSizeRecursive(path string) int64 {
	var size int64
	filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return size
}

func formatBytes(bytes int64) string {
	if bytes < 1024 {
		return strings.TrimSpace(fmt.Sprintf("%d B", bytes))
	}
	if bytes < 1024*1024 {
		return strings.TrimSpace(fmt.Sprintf("%.1f KB", float64(bytes)/1024))
	}
	if bytes < 1024*1024*1024 {
		return strings.TrimSpace(fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024)))
	}
	return strings.TrimSpace(fmt.Sprintf("%.1f GB", float64(bytes)/(1024*1024*1024)))
}
