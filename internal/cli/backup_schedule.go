package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

func backupScheduleDropInDir(systemdDir string) string {
	return filepath.Join(systemdDir, "veil-backup.service.d")
}

func backupScheduleDropInPath(systemdDir string) string {
	return filepath.Join(backupScheduleDropInDir(systemdDir), backupScheduleDropInName)
}

func scheduledPassphrasePathIsDefault(path string) bool {
	return filepath.Clean(path) == filepath.Clean(defaultScheduledBackupPassphrasePath)
}

func normalizeScheduledPassphrasePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("passphrase path is required")
	}
	if strings.ContainsAny(path, "\x00\n\r") {
		return "", errors.New("passphrase path contains invalid characters")
	}
	if !filepath.IsAbs(path) && !strings.HasPrefix(filepath.ToSlash(path), "/") {
		return "", errors.New("passphrase path must be absolute")
	}
	return filepath.Clean(path), nil
}

func syncBackupScheduleUnit(passphrasePath string) error {
	dropInPath := backupScheduleDropInPath(backupSystemdDir)
	if scheduledPassphrasePathIsDefault(passphrasePath) {
		if err := os.Remove(dropInPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove backup passphrase systemd drop-in: %w", err)
		}
		return nil
	}
	if err := atomicfile.Write(dropInPath, []byte(renderBackupScheduleDropIn(passphrasePath)), 0o644, 0o755); err != nil {
		return fmt.Errorf("write backup passphrase systemd drop-in: %w", err)
	}
	return nil
}

func removeBackupScheduleDropIn() error {
	dropInPath := backupScheduleDropInPath(backupSystemdDir)
	if err := os.Remove(dropInPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove backup passphrase systemd drop-in: %w", err)
	}
	return nil
}

func renderBackupScheduleDropIn(passphrasePath string) string {
	unitPath := filepath.ToSlash(passphrasePath)
	execStart := filepath.ToSlash(backupVeilBinary) +
		" backup create --state /var/lib/veil/state.json --key-path /etc/veil/state.key --passphrase-file " +
		quoteSystemdExecArg(unitPath) +
		" --output-dir /var/lib/veil/backups --prune --daily 7 --weekly 4 --monthly 12"
	return "[Unit]\n" +
		"ConditionPathExists=\n" +
		"ConditionPathExists=" + unitPath + "\n" +
		"\n[Service]\n" +
		"ExecStart=\n" +
		"ExecStart=" + execStart + "\n" +
		"ReadOnlyPaths=" + unitPath + "\n"
}

func quoteSystemdExecArg(arg string) string {
	if strings.ContainsAny(arg, " \t\"") {
		return `"` + strings.ReplaceAll(arg, `"`, `\"`) + `"`
	}
	return arg
}

func scheduledPassphrasePathFromDropIn(systemdDir string) string {
	body, err := os.ReadFile(backupScheduleDropInPath(systemdDir))
	if err != nil {
		return ""
	}
	if path := passphraseFileFromExecStart(effectiveSystemdDirective([]string{string(body)}, "ExecStart")); path != "" {
		return path
	}
	if path := effectiveSystemdDirective([]string{string(body)}, "ConditionPathExists"); path != "" {
		return filepath.Clean(filepath.FromSlash(path))
	}
	return ""
}

func effectiveSystemdDirective(fragments []string, key string) string {
	last := ""
	prefix := key + "="
	for _, fragment := range fragments {
		for _, line := range strings.Split(fragment, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || !strings.HasPrefix(line, prefix) {
				continue
			}
			last = strings.TrimPrefix(line, prefix)
		}
	}
	return last
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
	return filepath.Clean(filepath.FromSlash(rest))
}
