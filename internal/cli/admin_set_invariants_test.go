package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/api"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

func writeAdminState(t *testing.T, statePath, keyPath string, users []model.User) {
	t.Helper()
	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatal(err)
	}
	if err := managementstate.NewStore(statePath, cipher).Save(model.ManagementSnapshot{
		Settings: model.Settings{PanelListen: "127.0.0.1:2096", Mode: "server"},
		Users:    users,
	}); err != nil {
		t.Fatal(err)
	}
}

func loadAdminUsers(t *testing.T, statePath, keyPath string) []model.User {
	t.Helper()
	key, err := secrets.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, ok, err := managementstate.NewStore(statePath, cipher).Load()
	if err != nil || !ok {
		t.Fatalf("load state ok=%v err=%v", ok, err)
	}
	return snapshot.Users
}

func TestAdminSetRejectsLastAdministratorDemotion(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	keyPath := filepath.Join(t.TempDir(), "state.key")
	writeAdminState(t, statePath, keyPath, []model.User{
		{Username: "admin", PasswordHash: "old", Role: "admin", Locale: "ru"},
	})

	cmd := newFastRootCommand("test")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"admin", "set",
		"--username", "admin",
		"--password", "a-long-secure-password",
		"--role", "viewer",
		"--state", statePath,
		"--key-path", keyPath,
	})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "last administrator") {
		t.Fatalf("expected last administrator error, got %v", err)
	}
	users := loadAdminUsers(t, statePath, keyPath)
	if len(users) != 1 || users[0].Role != "admin" || users[0].Locale != "ru" {
		t.Fatalf("last admin was mutated: %+v", users)
	}
}

func TestAdminSetAllowsDemotingOneOfTwoAdmins(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	keyPath := filepath.Join(t.TempDir(), "state.key")
	writeAdminState(t, statePath, keyPath, []model.User{
		{Username: "alice", PasswordHash: "old", Role: "admin", Locale: "en"},
		{Username: "bob", PasswordHash: "old", Role: "admin", Locale: "en"},
	})

	cmd := newFastRootCommand("test")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"admin", "set",
		"--username", "bob",
		"--password", "a-long-secure-password",
		"--role", "viewer",
		"--state", statePath,
		"--key-path", keyPath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	users := loadAdminUsers(t, statePath, keyPath)
	roles := map[string]string{}
	for _, user := range users {
		roles[user.Username] = user.Role
	}
	if roles["alice"] != "admin" || roles["bob"] != "viewer" {
		t.Fatalf("roles=%v", roles)
	}

	second := newFastRootCommand("test")
	second.SetOut(&bytes.Buffer{})
	second.SetErr(&bytes.Buffer{})
	second.SetArgs([]string{
		"admin", "set",
		"--username", "alice",
		"--password", "a-long-secure-password",
		"--role", "viewer",
		"--state", statePath,
		"--key-path", keyPath,
	})
	if err := second.Execute(); err == nil || !strings.Contains(err.Error(), "last administrator") {
		t.Fatalf("expected second demotion to fail, got %v", err)
	}
}

func TestAdminSetRejectsShortPassword(t *testing.T) {
	cmd := newFastRootCommand("test")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"admin", "set",
		"--username", "admin",
		"--password", "short",
		"--state", filepath.Join(t.TempDir(), "state.json"),
		"--key-path", filepath.Join(t.TempDir(), "state.key"),
	})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "at least 12 characters") {
		t.Fatalf("expected password policy error, got %v", err)
	}
}

func TestAdminSetPasswordOnlyPreservesRoleAndLocale(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	keyPath := filepath.Join(t.TempDir(), "state.key")
	writeAdminState(t, statePath, keyPath, []model.User{
		{Username: "admin", PasswordHash: "old", Role: "admin", Locale: "ru"},
	})

	cmd := newFastRootCommand("test")
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(out)
	cmd.SetArgs([]string{
		"admin", "set",
		"--username", "admin",
		"--password", "a-long-secure-password",
		"--state", statePath,
		"--key-path", keyPath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Role: admin") {
		t.Fatalf("output=%s", out.String())
	}
	users := loadAdminUsers(t, statePath, keyPath)
	if len(users) != 1 || users[0].Role != "admin" || users[0].Locale != "ru" {
		t.Fatalf("password-only set mutated identity: %+v", users)
	}
}

func TestAdminSetRevokesPersistedSessions(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	writeAdminState(t, statePath, keyPath, []model.User{
		{Username: "alice", PasswordHash: "old", Role: "admin"},
	})
	registry, err := api.NewSessionRegistry(filepath.Join(root, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(api.SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}

	cmd := newFastRootCommand("test")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{
		"admin", "set",
		"--username", "alice",
		"--password", "a-long-secure-password",
		"--state", statePath,
		"--key-path", keyPath,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := api.NewSessionRegistry(filepath.Join(root, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get(session.Token); ok {
		t.Fatal("replaced user session remained authorized")
	}
}

func TestAdminResetRevokesAllSessionsAndNotifiesPanel(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	writeAdminState(t, statePath, keyPath, []model.User{
		{Username: "alice", PasswordHash: "old", Role: "admin"},
	})
	registry, err := api.NewSessionRegistry(filepath.Join(root, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(api.SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}

	orig := adminNotifyPanel
	defer func() { adminNotifyPanel = orig }()
	notified := false
	adminNotifyPanel = func() error {
		notified = true
		return nil
	}

	cmd := newFastRootCommand("test")
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"admin", "reset", "--state", statePath, "--key-path", keyPath})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !notified {
		t.Fatal("running Panel was not notified after reset")
	}
	reloaded, err := api.NewSessionRegistry(filepath.Join(root, "sessions.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get(session.Token); ok {
		t.Fatal("reset left pre-reset session authorized")
	}
}

func TestNotifyRunningPanelSendsHUPWhenActive(t *testing.T) {
	orig := adminSystemctlRun
	defer func() { adminSystemctlRun = orig }()
	var calls [][]string
	adminSystemctlRun = func(args ...string) error {
		calls = append(calls, append([]string{}, args...))
		return nil
	}
	if err := notifyRunningPanel(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || strings.Join(calls[0], " ") != "is-active --quiet veil.service" || strings.Join(calls[1], " ") != "kill -s HUP veil.service" {
		t.Fatalf("calls=%v", calls)
	}
}

func TestNotifyRunningPanelSkipsWhenInactive(t *testing.T) {
	orig := adminSystemctlRun
	defer func() { adminSystemctlRun = orig }()
	adminSystemctlRun = func(args ...string) error {
		if len(args) > 0 && args[0] == "is-active" {
			return os.ErrNotExist
		}
		t.Fatalf("unexpected systemctl %v", args)
		return nil
	}
	if err := notifyRunningPanel(); err != nil {
		t.Fatal(err)
	}
}
