package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/model"
	"golang.org/x/crypto/bcrypt"
)

// runAdminSetWithStdin executes `veil admin set` with the given stdin and
// returns stderr plus the command error. The running-panel notification is
// stubbed so the test never reaches the host's systemd.
func runAdminSetWithStdin(t *testing.T, stdin io.Reader, args ...string) (string, error) {
	t.Helper()
	orig := adminNotifyPanel
	t.Cleanup(func() { adminNotifyPanel = orig })
	adminNotifyPanel = func() error { return nil }

	cmd := newFastRootCommand("test")
	var stderr bytes.Buffer
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&stderr)
	cmd.SetIn(stdin)
	cmd.SetArgs(append([]string{"admin", "set"}, args...))
	err := cmd.Execute()
	return stderr.String(), err
}

func adminSetPaths(t *testing.T) (string, string) {
	t.Helper()
	statePath := filepath.Join(t.TempDir(), "state.json")
	keyPath := filepath.Join(t.TempDir(), "state.key")
	writeAdminState(t, statePath, keyPath, []model.User{{Username: "admin", PasswordHash: "old", Role: "admin"}})
	return statePath, keyPath
}

func assertAdminPassword(t *testing.T, statePath, keyPath, username, password string) {
	t.Helper()
	for _, user := range loadAdminUsers(t, statePath, keyPath) {
		if user.Username != username {
			continue
		}
		if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
			t.Fatalf("stored hash for %q does not match %q: %v", username, password, err)
		}
		return
	}
	t.Fatalf("user %q not found in state", username)
}

func assertAdminPasswordUnchanged(t *testing.T, statePath, keyPath string) {
	t.Helper()
	users := loadAdminUsers(t, statePath, keyPath)
	if len(users) != 1 || users[0].PasswordHash != "old" {
		t.Fatalf("state was mutated on a rejected password input: %+v", users)
	}
}

func TestAdminSetReadsPasswordFromStdin(t *testing.T) {
	statePath, keyPath := adminSetPaths(t)
	stderr, err := runAdminSetWithStdin(t, strings.NewReader("stdin-secure-password\n"),
		"--username", "admin", "--password-stdin", "--state", statePath, "--key-path", keyPath)
	if err != nil {
		t.Fatalf("admin set --password-stdin: %v\n%s", err, stderr)
	}
	if strings.Contains(stderr, "process list") {
		t.Fatalf("--password-stdin must not emit the argv warning: %q", stderr)
	}
	assertAdminPassword(t, statePath, keyPath, "admin", "stdin-secure-password")
}

func TestAdminSetPasswordStdinTrimsCRLFOnly(t *testing.T) {
	statePath, keyPath := adminSetPaths(t)
	// Interior and leading spaces are part of the credential; only the
	// trailing line ending is stripped.
	if _, err := runAdminSetWithStdin(t, strings.NewReader("  spaced secure pw \r\n"),
		"--password-stdin", "--state", statePath, "--key-path", keyPath); err != nil {
		t.Fatal(err)
	}
	assertAdminPassword(t, statePath, keyPath, "admin", "  spaced secure pw ")
}

func TestAdminSetPasswordStdinRejectsMultiLineAndEmpty(t *testing.T) {
	for name, input := range map[string]string{
		"multi-line": "first-secure-password\nsecond-line\n",
		"empty":      "",
		"newline":    "\n",
		"oversized":  strings.Repeat("a", int(adminPasswordInputMaxBytes)+1),
	} {
		t.Run(name, func(t *testing.T) {
			statePath, keyPath := adminSetPaths(t)
			_, err := runAdminSetWithStdin(t, strings.NewReader(input),
				"--password-stdin", "--state", statePath, "--key-path", keyPath)
			if err == nil {
				t.Fatalf("expected %s stdin to be rejected", name)
			}
			assertAdminPasswordUnchanged(t, statePath, keyPath)
		})
	}
}

func TestAdminSetReadsPasswordFromFile(t *testing.T) {
	statePath, keyPath := adminSetPaths(t)
	passwordFile := filepath.Join(t.TempDir(), "admin.pw")
	if err := os.WriteFile(passwordFile, []byte("file-secure-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr, err := runAdminSetWithStdin(t, strings.NewReader(""),
		"--password-file", passwordFile, "--state", statePath, "--key-path", keyPath)
	if err != nil {
		t.Fatalf("admin set --password-file: %v\n%s", err, stderr)
	}
	if strings.Contains(stderr, "process list") {
		t.Fatalf("--password-file must not emit the argv warning: %q", stderr)
	}
	assertAdminPassword(t, statePath, keyPath, "admin", "file-secure-password")
}

func TestAdminSetPasswordFileRefusesSymlink(t *testing.T) {
	statePath, keyPath := adminSetPaths(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "target.pw")
	if err := os.WriteFile(target, []byte("file-secure-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.pw")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := runAdminSetWithStdin(t, strings.NewReader(""),
		"--password-file", link, "--state", statePath, "--key-path", keyPath); err == nil {
		t.Fatal("expected a symlinked --password-file to be refused")
	}
	assertAdminPasswordUnchanged(t, statePath, keyPath)
}

func TestAdminSetPasswordSourcesAreMutuallyExclusive(t *testing.T) {
	passwordFile := filepath.Join(t.TempDir(), "admin.pw")
	if err := os.WriteFile(passwordFile, []byte("file-secure-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"argv+stdin": {"--password", "argv-secure-password", "--password-stdin"},
		"argv+file":  {"--password", "argv-secure-password", "--password-file", passwordFile},
		"stdin+file": {"--password-stdin", "--password-file", passwordFile},
	} {
		t.Run(name, func(t *testing.T) {
			statePath, keyPath := adminSetPaths(t)
			_, err := runAdminSetWithStdin(t, strings.NewReader("stdin-secure-password\n"),
				append(args, "--state", statePath, "--key-path", keyPath)...)
			if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
				t.Fatalf("expected mutually exclusive error, got %v", err)
			}
			assertAdminPasswordUnchanged(t, statePath, keyPath)
		})
	}
}

func TestAdminSetArgvPasswordStillWorksButWarns(t *testing.T) {
	statePath, keyPath := adminSetPaths(t)
	stderr, err := runAdminSetWithStdin(t, strings.NewReader(""),
		"--password", "argv-secure-password", "--state", statePath, "--key-path", keyPath)
	if err != nil {
		t.Fatalf("legacy --password must keep working: %v", err)
	}
	if !strings.Contains(stderr, "/proc/<pid>/cmdline") || !strings.Contains(stderr, "--password-stdin") {
		t.Fatalf("expected argv exposure warning on stderr, got %q", stderr)
	}
	assertAdminPassword(t, statePath, keyPath, "admin", "argv-secure-password")
}

func TestAdminSetPromptsOnTerminal(t *testing.T) {
	orig := adminPasswordTerminal
	t.Cleanup(func() { adminPasswordTerminal = orig })
	answers := []string{"prompt-secure-password", "prompt-secure-password"}
	adminPasswordTerminal = func(io.Reader) (func() ([]byte, error), bool) {
		return func() ([]byte, error) {
			if len(answers) == 0 {
				return nil, errors.New("prompt read past scripted answers")
			}
			next := answers[0]
			answers = answers[1:]
			return []byte(next), nil
		}, true
	}

	statePath, keyPath := adminSetPaths(t)
	stderr, err := runAdminSetWithStdin(t, strings.NewReader(""),
		"--state", statePath, "--key-path", keyPath)
	if err != nil {
		t.Fatalf("interactive admin set: %v\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "New password:") || !strings.Contains(stderr, "Confirm password:") {
		t.Fatalf("expected new/confirm prompts on stderr, got %q", stderr)
	}
	if strings.Contains(stderr, "prompt-secure-password") {
		t.Fatalf("prompted password was echoed to stderr: %q", stderr)
	}
	assertAdminPassword(t, statePath, keyPath, "admin", "prompt-secure-password")
}

func TestAdminSetPromptRejectsMismatch(t *testing.T) {
	orig := adminPasswordTerminal
	t.Cleanup(func() { adminPasswordTerminal = orig })
	answers := []string{"prompt-secure-password", "different-secure-password"}
	adminPasswordTerminal = func(io.Reader) (func() ([]byte, error), bool) {
		return func() ([]byte, error) {
			next := answers[0]
			answers = answers[1:]
			return []byte(next), nil
		}, true
	}

	statePath, keyPath := adminSetPaths(t)
	_, err := runAdminSetWithStdin(t, strings.NewReader(""), "--state", statePath, "--key-path", keyPath)
	if err == nil || !strings.Contains(err.Error(), "do not match") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
	assertAdminPasswordUnchanged(t, statePath, keyPath)
}

func TestAdminSetWithoutPasswordSourceOnNonTerminalFails(t *testing.T) {
	statePath, keyPath := adminSetPaths(t)
	// A non-terminal stdin without --password-stdin must not be consumed
	// implicitly: the operator has to opt into reading a secret from it.
	_, err := runAdminSetWithStdin(t, strings.NewReader("implicit-secure-password\n"),
		"--state", statePath, "--key-path", keyPath)
	if !errors.Is(err, errAdminPasswordRequired) {
		t.Fatalf("expected password-required error, got %v", err)
	}
	assertAdminPasswordUnchanged(t, statePath, keyPath)
}
