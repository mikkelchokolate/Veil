package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

func TestAdminSetRejectsMissingPassword(t *testing.T) {
	cmd := NewRootCommand("test")
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	cmd.SetArgs([]string{
		"admin", "set",
		"--state", filepath.Join(t.TempDir(), "state.json"),
		"--key-path", filepath.Join(t.TempDir(), "state.key"),
	})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--password is required") {
		t.Fatalf("expected missing password error, got %v", err)
	}
}

func TestAdminShowReportsNoUsers(t *testing.T) {
	tempEtc := t.TempDir()
	tempVar := t.TempDir()
	statePath := filepath.Join(tempVar, "state.json")
	keyPath := filepath.Join(tempEtc, "state.key")

	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}
	if err := managementstate.NewStore(statePath, cipher).Save(model.ManagementSnapshot{}); err != nil {
		t.Fatalf("save empty state: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"admin", "show", "--state", statePath, "--key-path", keyPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "No users registered in state.") {
		t.Fatalf("expected no users message, got:\n%s", out.String())
	}
}

func TestAdminRotateKeyRejectsMissingKeyFile(t *testing.T) {
	cmd := NewRootCommand("test")
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	cmd.SetArgs([]string{
		"admin", "rotate-key",
		"--state", filepath.Join(t.TempDir(), "state.json"),
		"--key-path", filepath.Join(t.TempDir(), "missing.key"),
	})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "read old key file") {
		t.Fatalf("expected read old key error, got %v", err)
	}
}

func TestAdminRotateKeyRejectsWrongKeyLength(t *testing.T) {
	tempEtc := t.TempDir()
	tempVar := t.TempDir()
	keyPath := filepath.Join(tempEtc, "state.key")
	if err := os.WriteFile(keyPath, []byte("short"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	cmd := NewRootCommand("test")
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	cmd.SetArgs([]string{
		"admin", "rotate-key",
		"--state", filepath.Join(tempVar, "state.json"),
		"--key-path", keyPath,
	})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "old key file has wrong length") {
		t.Fatalf("expected wrong key length error, got %v", err)
	}
}

func TestAdminRotateKeyRejectsMissingState(t *testing.T) {
	tempEtc := t.TempDir()
	tempVar := t.TempDir()
	keyPath := filepath.Join(tempEtc, "state.key")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0xab}, secrets.KeySize), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	cmd := NewRootCommand("test")
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	cmd.SetArgs([]string{
		"admin", "rotate-key",
		"--state", filepath.Join(tempVar, "state.json"),
		"--key-path", keyPath,
	})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no state found") {
		t.Fatalf("expected no state error, got %v", err)
	}
}

func TestAdminSetUpdatesFirstAdminWhenUsernameOmitted(t *testing.T) {
	tempEtc := t.TempDir()
	tempVar := t.TempDir()
	statePath := filepath.Join(tempVar, "state.json")
	keyPath := filepath.Join(tempEtc, "state.key")

	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}
	if err := managementstate.NewStore(statePath, cipher).Save(model.ManagementSnapshot{
		Users: []model.User{
			{Username: "existing_admin", PasswordHash: "hash", Role: "admin"},
			{Username: "viewer1", PasswordHash: "hash", Role: "viewer"},
		},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"admin", "set",
		"--password", "new-password-123",
		"--role", "viewer",
		"--state", statePath,
		"--key-path", keyPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "Username: existing_admin") {
		t.Fatalf("expected first admin username, got:\n%s", got)
	}
	if !strings.Contains(got, "Role: viewer") {
		t.Fatalf("expected viewer role, got:\n%s", got)
	}
}

func TestAdminSetCreatesUserWhenUsernameNotFound(t *testing.T) {
	tempEtc := t.TempDir()
	tempVar := t.TempDir()
	statePath := filepath.Join(tempVar, "state.json")
	keyPath := filepath.Join(tempEtc, "state.key")

	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}
	if err := managementstate.NewStore(statePath, cipher).Save(model.ManagementSnapshot{
		Users: []model.User{
			{Username: "old_admin", PasswordHash: "hash", Role: "admin"},
		},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{
		"admin", "set",
		"--username", "new_admin",
		"--password", "new-password-123",
		"--state", statePath,
		"--key-path", keyPath,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Username: new_admin") {
		t.Fatalf("expected new admin username, got:\n%s", out.String())
	}
}
