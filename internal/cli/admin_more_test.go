package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/storage"
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

func TestAdminSetCommitsDesiredRevisionAndStateDigest(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatal(err)
	}
	if err := managementstate.NewStore(statePath, cipher).Save(model.ManagementSnapshot{
		Users: []model.User{{Username: "admin", PasswordHash: "old", Role: "admin"}},
	}); err != nil {
		t.Fatal(err)
	}
	databasePath := filepath.Join(root, "veil.db")
	db, err := storage.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	cmd := NewRootCommand("test")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admin", "set", "--state", statePath, "--key-path", keyPath, "--password", "a-new-long-password"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	stateBody, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	db, err = storage.OpenExisting(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	revisions, err := apply.NewRevisionStore(db).Get()
	if err != nil {
		t.Fatal(err)
	}
	if revisions.Desired != 1 {
		t.Fatalf("desired revision=%d want=1", revisions.Desired)
	}
	digest, err := apply.NewSnapshotStore(db).StateDigest(revisions.Desired)
	if err != nil {
		t.Fatal(err)
	}
	if digest != managementstate.EncodedStateSHA256(stateBody) {
		t.Fatalf("snapshot digest=%q", digest)
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
