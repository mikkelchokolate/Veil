// Package veilenv owns discovery and safe mutation of the installed Panel
// environment file (veil.env). That file is what the Panel unit, the
// privileged helper and the backup unit load, so the paths it records —
// VEIL_KEY_PATH above all — are the live configuration, not a hint.
package veilenv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

// KeyPathKey is the environment variable the installed units read the state
// encryption key from.
const KeyPathKey = "VEIL_KEY_PATH"

// DefaultSystemdDir is where the installed veil.service drop-ins live.
const DefaultSystemdDir = "/etc/systemd/system"

// DefaultPath is the packaged environment file.
func DefaultPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(programData(), "Veil", "veil.env")
	}
	return filepath.Join(hostenv.DefaultEtcDir, "veil.env")
}

// Candidates lists the locations the installed environment file may live at,
// most specific first, using the same discovery order as the `veil status`
// probe: VEIL_ETC_DIR is explicit operator intent, then the root implied by
// VEIL_LIVE_ROOT/VEIL_KEY_PATH, then the EnvironmentFile recorded in the
// installed veil.service drop-in (custom --etc-dir installs), and finally the
// packaged path (issue #635).
func Candidates(systemdSystemDir string) []string {
	if runtime.GOOS == "windows" {
		return []string{DefaultPath()}
	}
	if dir := strings.TrimSpace(os.Getenv("VEIL_ETC_DIR")); dir != "" {
		return []string{filepath.Join(dir, "veil.env")}
	}
	candidates := []string{}
	if etc := hostenv.EtcDir(); etc != hostenv.DefaultEtcDir {
		candidates = append(candidates, filepath.Join(etc, "veil.env"))
	}
	if path := systemdEnvFilePath(systemdSystemDir); path != "" {
		candidates = append(candidates, path)
	}
	return append(candidates, DefaultPath())
}

// Resolve returns the environment file the installed units load: the first
// existing candidate, or the most generic one so error messages name a real
// location.
func Resolve(systemdSystemDir string) string {
	candidates := Candidates(systemdSystemDir)
	for _, candidate := range candidates[:len(candidates)-1] {
		if fileExists(candidate) {
			return candidate
		}
	}
	return candidates[len(candidates)-1]
}

// Value returns the value recorded for key in body, with systemd-style
// quoting and the EnvironmentFile ignore-missing marker stripped.
func Value(body []byte, key string) string {
	found := ""
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		found = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return found
}

// PreparedUpdate is an environment-file rewrite staged next to the live file
// but not yet visible to the units. The caller rotates the key first and only
// then commits, so the environment never points at a key whose state has not
// been re-encrypted — and, when it cannot be prepared at all, nothing has been
// rotated yet (#1453).
type PreparedUpdate struct {
	path     string
	tempPath string
	keyPath  string
}

// PrepareKeyPath stages an atomic rewrite of envPath that points VEIL_KEY_PATH
// at keyPath. Every other line is preserved, and the file keeps its owner and
// mode. It fails before anything is rotated when the environment file is
// missing or its directory cannot be written.
func PrepareKeyPath(envPath, keyPath string) (*PreparedUpdate, error) {
	body, err := os.ReadFile(envPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("panel environment file %s does not exist, so VEIL_KEY_PATH cannot be repointed", envPath)
		}
		return nil, fmt.Errorf("read panel environment file %s: %w", envPath, err)
	}
	// An installed veil.env may be a symlink to an operator-controlled location;
	// rewrite the target instead of replacing the link with a regular file.
	if resolved, resolveErr := filepath.EvalSymlinks(envPath); resolveErr == nil {
		envPath = resolved
	}
	info, err := os.Stat(envPath)
	if err != nil {
		return nil, fmt.Errorf("stat panel environment file %s: %w", envPath, err)
	}

	updated, err := setValue(strings.ReplaceAll(string(body), "\r\n", "\n"), KeyPathKey, keyPath)
	if err != nil {
		return nil, err
	}
	tempPath, err := writeStaged(envPath, []byte(updated), info)
	if err != nil {
		return nil, err
	}
	return &PreparedUpdate{path: envPath, tempPath: tempPath, keyPath: keyPath}, nil
}

// Path reports the environment file this update rewrites.
func (u *PreparedUpdate) Path() string { return u.path }

// KeyPath reports the VEIL_KEY_PATH value the update installs.
func (u *PreparedUpdate) KeyPath() string { return u.keyPath }

// Commit installs the staged file with a same-directory rename, so a reader
// sees either the old or the new environment, never a partial write.
func (u *PreparedUpdate) Commit() error {
	if u.tempPath == "" {
		return nil
	}
	if err := os.Rename(u.tempPath, u.path); err != nil {
		return fmt.Errorf("install rewritten panel environment file %s: %w", u.path, err)
	}
	u.tempPath = ""
	return nil
}

// Abort drops the staged copy. It is safe to call after Commit.
func (u *PreparedUpdate) Abort() {
	if u.tempPath != "" {
		_ = os.Remove(u.tempPath)
		u.tempPath = ""
	}
}

// setValue replaces the last assignment of key (appending one when absent),
// keeping every other line byte for byte.
func setValue(body, key, value string) (string, error) {
	rendered, err := envValue(value)
	if err != nil {
		return "", fmt.Errorf("%s: %w", key, err)
	}
	line := key + "=" + rendered
	lines := strings.Split(body, "\n")
	replaced := false
	for i := len(lines) - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		name, _, ok := strings.Cut(trimmed, "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		lines[i] = line
		replaced = true
		break
	}
	if !replaced {
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = append(lines[:len(lines)-1], line, "")
		} else {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n"), nil
}

// envValue renders value as an environment-file assignment, quoting and
// escaping when needed. A newline or NUL cannot be represented in one line and
// is refused before anything is staged, so an operator-supplied path can never
// inject another variable or split the assignment.
func envValue(value string) (string, error) {
	if strings.ContainsAny(value, "\n\r\x00") {
		return "", fmt.Errorf("value %q contains a line break or NUL and cannot be written to an environment file", value)
	}
	if value != "" && !strings.ContainsAny(value, " \t#\"'\\") {
		return value, nil
	}
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
	return `"` + escaped + `"`, nil
}

// writeStaged writes content to a temporary file in the target's directory,
// carrying the target's mode and ownership, and returns its path.
func writeStaged(target string, content []byte, info os.FileInfo) (string, error) {
	dir := filepath.Dir(target)
	temp, err := os.CreateTemp(dir, ".veil.env-*")
	if err != nil {
		return "", fmt.Errorf("panel environment file %s cannot be rewritten: %w", target, err)
	}
	tempPath := temp.Name()
	cleanup := func(err error) (string, error) {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return "", err
	}
	if _, err := temp.Write(content); err != nil {
		return cleanup(fmt.Errorf("stage panel environment file %s: %w", target, err))
	}
	if err := temp.Sync(); err != nil {
		return cleanup(fmt.Errorf("sync staged panel environment file %s: %w", target, err))
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("close staged panel environment file %s: %w", target, err)
	}
	if err := os.Chmod(tempPath, info.Mode().Perm()); err != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("preserve mode of %s: %w", target, err)
	}
	if err := preserveOwner(tempPath, info); err != nil {
		_ = os.Remove(tempPath)
		return "", err
	}
	return tempPath, nil
}

func systemdEnvFilePath(systemdSystemDir string) string {
	if runtime.GOOS == "windows" {
		return ""
	}
	dropInDir := filepath.Join(systemdSystemDir, "veil.service.d")
	entries, err := os.ReadDir(dropInDir)
	if err != nil {
		return ""
	}
	found := ""
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".conf") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dropInDir, entry.Name()))
		if err != nil {
			continue
		}
		if path := EnvFileDirective(body); path != "" {
			found = path
		}
	}
	return found
}

// EnvFileDirective extracts the EnvironmentFile= target from a systemd unit
// fragment. A leading "-" (ignore-missing marker) and systemd-style quoting
// are stripped.
func EnvFileDirective(body []byte) string {
	found := ""
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(name) != "EnvironmentFile" {
			continue
		}
		// systemd applies every EnvironmentFile= line in order and an empty
		// assignment resets the list; keep the last directive, matching the
		// last-drop-in-wins ordering used across .conf fragments.
		value = strings.TrimPrefix(strings.TrimSpace(value), "-")
		found = strings.Trim(value, `"'`)
	}
	return found
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func programData() string {
	if pd := strings.TrimSpace(os.Getenv("ProgramData")); pd != "" {
		return pd
	}
	return `C:\ProgramData`
}
