package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
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

// veilDirs lists the Veil-managed directories to measure. The configuration
// and state roots follow VEIL_ETC_DIR/VEIL_VAR_DIR (including their *_PATH
// fallbacks) so a custom --etc-dir/--var-dir install reports its own tree
// instead of the packaged /etc/veil + /var/lib/veil pair (issue #638). The
// optional Caddy and Mita state directories are included when explicitly
// configured or present on disk. System log roots like /var/log are
// deliberately excluded: they are not Veil-managed, they mix unrelated log
// volume into the Veil disk card, and a full recursive walk of them made
// every GET /api/disk disproportionately expensive (#641).
func veilDirs() []string {
	dirs := []string{hostenv.VarDir(), hostenv.EtcDir()}
	for _, candidate := range []struct {
		env      string
		fallback string
	}{
		{"VEIL_CADDY_STATE_DIR", "/var/lib/caddy"},
		{"VEIL_MITA_STATE_DIR", "/var/lib/mita"},
	} {
		dir := strings.TrimSpace(os.Getenv(candidate.env))
		configured := dir != ""
		if !configured {
			dir = candidate.fallback
		}
		if info, err := os.Stat(dir); !configured && (err != nil || !info.IsDir()) {
			continue
		}
		dirs = append(dirs, dir)
	}
	seen := map[string]struct{}{}
	out := dirs[:0]
	for _, dir := range dirs {
		clean := filepath.Clean(dir)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, dir)
	}
	return out
}

// readDirDiskStats returns disk usage for Veil-managed directories.
func readDirDiskStats() DiskStats {
	stats := DiskStats{}
	for _, dir := range veilDirs() {
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
