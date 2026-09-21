package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	if !backupScheduleDropInRequired(backupSystemdDir, passphrasePath) {
		replaced, err := removeFileKeepingRollback(dropInPath)
		if err != nil {
			return nil, fmt.Errorf("remove backup passphrase systemd drop-in: %w", err)
		}
		return replaced, nil
	}
	replaced, err := publishReplacingFile(dropInPath, []byte(renderBackupScheduleDropIn(backupSystemdDir, passphrasePath)), 0o644, 0o755)
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

// backupAuxSystemdDirs are the additional systemd unit directories consulted
// when reconstructing the effective veil-backup.service. Packaged vendor
// units and any drop-ins live outside backupSystemdDir; the slice runs in
// decreasing precedence (systemd's unit load order) so the first readable
// base unit wins and lower-precedence drop-ins apply first.
var backupAuxSystemdDirs = []string{"/run/systemd/system", "/usr/lib/systemd/system", "/lib/systemd/system"}

// backupServiceUnitFragments collects the base unit plus every drop-in
// fragment that shapes the effective veil-backup.service, ordered so later
// entries win on repeated keys (the same rule systemd applies: drop-ins from
// higher-precedence directories apply last, and same-named drop-ins in a
// higher-precedence directory mask lower ones). The schedule drop-in being
// rewritten is excluded so a stale copy never feeds the derivation (issue
// #627).
func backupServiceUnitFragments(systemdDir string) []string {
	var fragments []string
	// Index 0 is the highest-precedence directory (the operator unit dir),
	// followed by the runtime and vendor dirs in unit load order.
	dirs := append([]string{systemdDir}, backupAuxSystemdDirs...)
	for _, dir := range dirs {
		body, err := os.ReadFile(filepath.Join(dir, "veil-backup.service"))
		if err == nil {
			fragments = append(fragments, string(body))
			break
		}
	}
	perDir := make([][]string, len(dirs))
	present := make([]map[string]bool, len(dirs))
	for i, dir := range dirs {
		entries, err := filepath.Glob(filepath.Join(dir, "veil-backup.service.d", "*.conf"))
		if err != nil {
			continue
		}
		sort.Strings(entries)
		perDir[i] = entries
		present[i] = make(map[string]bool, len(entries))
		for _, entry := range entries {
			present[i][filepath.Base(entry)] = true
		}
	}
	// Emit lowest precedence first; skip names that also exist in a
	// higher-precedence directory (masked) and the schedule drop-in itself.
	for i := len(dirs) - 1; i >= 0; i-- {
		for _, entry := range perDir[i] {
			name := filepath.Base(entry)
			if dirs[i] == systemdDir && name == backupScheduleDropInName {
				continue
			}
			masked := false
			for j := 0; j < i; j++ {
				if present[j][name] {
					masked = true
					break
				}
			}
			if masked {
				continue
			}
			if body, err := os.ReadFile(entry); err == nil {
				fragments = append(fragments, string(body))
			}
		}
	}
	return fragments
}

// scheduledBackupPassphraseDestination resolves where `schedule enable` must
// write the passphrase when the operator did not pass --passphrase-path: the
// path the installed unit actually reads. A custom --etc-dir/--var-dir
// install (packaged drop-in or rendered unit) points --passphrase-file at its
// own tree, so writing /etc/veil/backup.passphrase would leave the timer
// encrypting with a different secret than the operator supplied (issue #627).
func scheduledBackupPassphraseDestination(systemdDir, flagDefault string) string {
	fragments := backupServiceUnitFragments(systemdDir)
	configured := passphraseFileFromExecStart(effectiveSystemdDirective(fragments, "ExecStart"))
	if configured == "" {
		return flagDefault
	}
	clean := filepath.Clean(filepath.FromSlash(configured))
	if !filepath.IsAbs(clean) && !strings.HasPrefix(filepath.ToSlash(clean), "/") {
		return flagDefault
	}
	return clean
}

// backupScheduleDropInRequired reports whether the schedule drop-in has work
// to do: the unit must be skipped only when its configured --passphrase-file
// and ConditionPathExists already match the resolved path. A unit that cannot
// be read at all keeps the historical default-path behavior.
func backupScheduleDropInRequired(systemdDir, passphrasePath string) bool {
	fragments := backupServiceUnitFragments(systemdDir)
	execStart := effectiveSystemdDirective(fragments, "ExecStart")
	if execStart == "" {
		return !scheduledPassphrasePathIsDefault(passphrasePath)
	}
	configured := passphraseFileFromExecStart(execStart)
	condition := effectiveSystemdDirective(fragments, "ConditionPathExists")
	return !(sameScheduledPath(passphrasePath, configured) && sameScheduledPath(passphrasePath, condition))
}

func sameScheduledPath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(filepath.FromSlash(a)) == filepath.Clean(filepath.FromSlash(b))
}

func renderBackupScheduleDropIn(systemdDir, passphrasePath string) string {
	unitPath := filepath.ToSlash(passphrasePath)
	execStart := effectiveSystemdDirective(backupServiceUnitFragments(systemdDir), "ExecStart")
	if execStart != "" {
		// Preserve the installed unit's binary, state/key/output trees, and
		// retention flags verbatim — only the --passphrase-file value is
		// substituted. Rewriting a fixed command here would stomp a custom
		// --etc-dir/--var-dir install back onto the packaged defaults, which
		// is exactly what this drop-in previously did (issue #627).
		execStart = rewriteExecStartPassphraseFile(execStart, unitPath)
	} else {
		execStart = filepath.ToSlash(backupVeilBinary) +
			" backup create --state /var/lib/veil/state.json --key-path /etc/veil/state.key --passphrase-file " +
			quoteSystemdExecArg(unitPath) +
			" --output-dir /var/lib/veil/backups --prune --daily 7 --weekly 4 --monthly 12"
	}
	return "[Unit]\n" +
		"ConditionPathExists=\n" +
		"ConditionPathExists=" + unitPath + "\n" +
		"\n[Service]\n" +
		"ExecStart=\n" +
		"ExecStart=" + execStart + "\n" +
		"ReadOnlyPaths=" + unitPath + "\n"
}

// rewriteExecStartPassphraseFile replaces the value of --passphrase-file in
// an ExecStart line, preserving every other token (including quoted ones).
// When the flag is absent it is appended so the oneshot always reads the
// scheduled passphrase.
func rewriteExecStartPassphraseFile(execStart, newPath string) string {
	const flag = "--passphrase-file"
	quoted := quoteSystemdExecArg(newPath)
	offset := 0
	for {
		idx := strings.Index(execStart[offset:], flag)
		if idx < 0 {
			return strings.TrimRight(execStart, " \t") + " " + flag + " " + quoted
		}
		abs := offset + idx
		rest := execStart[abs+len(flag):]
		switch {
		case rest == "":
			return execStart[:abs] + flag + " " + quoted
		case rest[0] == '=':
			_, end := scanExecArgValue(rest[1:])
			return execStart[:abs] + flag + "=" + quoted + rest[1+end:]
		case rest[0] == ' ' || rest[0] == '\t':
			i := 0
			for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t') {
				i++
			}
			_, end := scanExecArgValue(rest[i:])
			return execStart[:abs] + flag + " " + quoted + rest[i+end:]
		default:
			// A longer flag that merely shares the prefix (e.g.
			// --passphrase-file-extra); keep searching past it.
			offset = abs + len(flag)
		}
	}
}

// scanExecArgValue measures one ExecStart value token starting at s[0],
// returning its unquoted extent length. Double-quoted tokens honor backslash
// escapes so a quoted value is replaced as a whole.
func scanExecArgValue(s string) (value string, end int) {
	if s == "" {
		return "", 0
	}
	if s[0] == '"' {
		i := 1
		for i < len(s) {
			if s[i] == '\\' && i+1 < len(s) {
				i += 2
				continue
			}
			if s[i] == '"' {
				return s[1:i], i + 1
			}
			i++
		}
		return s[1:], len(s)
	}
	i := 0
	for i < len(s) && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' {
		i++
	}
	return s[:i], i
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
	offset := 0
	for {
		idx := strings.Index(execStart[offset:], flag)
		if idx < 0 {
			return ""
		}
		abs := offset + idx
		raw := execStart[abs+len(flag):]
		if raw != "" {
			switch raw[0] {
			case '=', ' ', '\t', '\n':
			default:
				// A longer flag that merely shares the prefix.
				offset = abs + len(flag)
				continue
			}
		}
		rest := strings.TrimSpace(raw)
		if rest == "" {
			return ""
		}
		if rest[0] == '=' {
			rest = strings.TrimSpace(rest[1:])
			if rest == "" {
				return ""
			}
		}
		// scanExecArgValue honors backslash escapes inside quoted tokens,
		// so the parsed extent matches what rewriteExecStartPassphraseFile
		// writes — the reader and the rewriter share one token grammar.
		value, _ := scanExecArgValue(rest)
		if value == "" {
			return ""
		}
		return filepath.Clean(filepath.FromSlash(value))
	}
}
