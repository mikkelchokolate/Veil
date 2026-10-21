// Package syspath resolves and runs host system tools (systemctl, apt-get,
// ufw, caddy, useradd, ...) from a fixed set of root-owned system directories
// instead of the ambient PATH.
//
// Root-run Veil commands (install, backup restore, uninstall, admin) can be
// invoked with an attacker-influenced PATH — `sudo -E`, provisioning scripts
// or automation that export PATH with a writable directory first. Resolving a
// privileged helper through that PATH would execute a planted shim as root,
// and a resolved path persisted into a systemd unit (veil-caddy.service
// ExecStart) would keep doing so long after the poisoned session ended.
package syspath

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TrustedDirs is the fixed search order for system tools. It matches the PATH
// pinned into Veil's rendered systemd units.
var TrustedDirs = []string{"/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin"}

// ErrNotFound reports that a tool is not present in any trusted directory.
var ErrNotFound = errors.New("executable file not found in trusted system directories")

// TrustedPATH is the PATH value handed to privileged child processes so that
// tools which themselves exec helpers (apt-get -> dpkg, systemctl ->
// systemd-tty-ask-password-agent) do not inherit a poisoned search path.
func TrustedPATH() string { return strings.Join(TrustedDirs, string(os.PathListSeparator)) }

// LookPath resolves name to an absolute executable path. A bare name is
// searched only in TrustedDirs; the ambient PATH is ignored. An absolute name
// is accepted as an explicit operator choice when it is an executable regular
// file. Relative names containing a separator are rejected.
func LookPath(name string) (string, error) {
	if name == "" {
		return "", &exec.Error{Name: name, Err: ErrNotFound}
	}
	if strings.ContainsRune(name, '/') {
		if !filepath.IsAbs(name) {
			return "", &exec.Error{Name: name, Err: fmt.Errorf("relative path is not allowed for a system tool")}
		}
		if err := checkExecutable(name); err != nil {
			return "", &exec.Error{Name: name, Err: err}
		}
		return filepath.Clean(name), nil
	}
	for _, dir := range TrustedDirs {
		candidate := filepath.Join(dir, name)
		if checkExecutable(candidate) == nil {
			return candidate, nil
		}
	}
	return "", &exec.Error{Name: name, Err: ErrNotFound}
}

func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}

// CommandContext is exec.CommandContext for a system tool: name is resolved
// with LookPath (never through the ambient PATH) and the child receives
// TrustedPATH. When the tool cannot be resolved the returned command fails on
// Start/Run with the lookup error instead of falling back to PATH.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	if ctx == nil {
		ctx = context.Background()
	}
	path, err := LookPath(name)
	if err != nil {
		cmd := exec.CommandContext(ctx, "/nonexistent/"+filepath.Base(name), args...)
		cmd.Args[0] = name
		cmd.Err = err
		cmd.Env = Env(os.Environ())
		return cmd
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = Env(os.Environ())
	return cmd
}

// Command is CommandContext without a context.
func Command(name string, args ...string) *exec.Cmd {
	return CommandContext(context.Background(), name, args...)
}

// Env returns base with any PATH entry replaced by TrustedPATH.
func Env(base []string) []string {
	out := make([]string, 0, len(base)+1)
	for _, item := range base {
		if strings.HasPrefix(item, "PATH=") {
			continue
		}
		out = append(out, item)
	}
	return append(out, "PATH="+TrustedPATH())
}
