package backup

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	ScheduleDropInDir  = "veil-backup.service.d"
	ScheduleDropInName = "passphrase-path.conf"
)

// ScheduledPassphrasePath returns the live --passphrase-file path from the
// systemd drop-in written by `veil backup schedule enable --passphrase-path`.
// An empty result means the packaged default path is in effect.
func ScheduledPassphrasePath(systemdDir string) string {
	if strings.TrimSpace(systemdDir) == "" {
		return ""
	}
	body, err := os.ReadFile(filepath.Join(systemdDir, ScheduleDropInDir, ScheduleDropInName))
	if err != nil {
		return ""
	}
	return passphraseFileFromExecStart(string(body))
}

func passphraseFileFromExecStart(execStart string) string {
	const flag = "--passphrase-file"
	idx := strings.Index(execStart, flag)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(execStart[idx+len(flag):])
	if rest == "" {
		return ""
	}
	if rest[0] == '"' {
		end := strings.IndexByte(rest[1:], '"')
		if end < 0 {
			return ""
		}
		return filepath.Clean(filepath.FromSlash(rest[1 : 1+end]))
	}
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		rest = rest[:i]
	}
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest = rest[:i]
	}
	return filepath.Clean(filepath.FromSlash(rest))
}
