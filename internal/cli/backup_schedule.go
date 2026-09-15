package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	clean := filepath.Clean(path)
	if passphrasePathHiddenByProtectHome(clean) {
		return "", fmt.Errorf("passphrase path %s is hidden by veil-backup.service ProtectHome=yes", clean)
	}
	return clean, nil
}

func passphrasePathHiddenByProtectHome(path string) bool {
	slash := filepath.ToSlash(path)
	for _, prefix := range []string{"/root", "/home", "/run/user"} {
		if slash == prefix || strings.HasPrefix(slash, prefix+"/") {
			return true
		}
	}
	return false
}

func publishBackupScheduleUnit(passphrasePath string) (*fileReplace, error) {
	dropInPath := backupScheduleDropInPath(backupSystemdDir)
	if scheduledPassphrasePathIsDefault(passphrasePath) {
		replaced, err := removeFileKeepingRollback(dropInPath)
		if err != nil {
			return nil, fmt.Errorf("remove backup passphrase systemd drop-in: %w", err)
		}
		return replaced, nil
	}
	replaced, err := publishReplacingFile(dropInPath, []byte(renderBackupScheduleDropIn(passphrasePath)), 0o644, 0o755)
	if err != nil {
		return nil, fmt.Errorf("write backup passphrase systemd drop-in: %w", err)
	}
	return replaced, nil
}

func removeFileKeepingRollback(path string) (*fileReplace, error) {
	replaced := &fileReplace{path: path, backupPath: path + ".replace-backup"}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return replaced, nil
		}
		return nil, err
	}
	if err := os.Remove(replaced.backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.Rename(path, replaced.backupPath); err != nil {
		return nil, err
	}
	replaced.hadPrevious = true
	return replaced, nil
}

func restoreScheduleEnable(passReplace, unitReplace *fileReplace) error {
	unitErr := unitReplace.Restore()
	passErr := passReplace.Restore()
	if passErr != nil {
		if unitErr != nil {
			return fmt.Errorf("%v (also restore systemd drop-in: %v)", passErr, unitErr)
		}
		return passErr
	}
	return unitErr
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
