package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/api"
)

func TestCLIBackupRestoreInvalidatesBrowserSessions(t *testing.T) {
	tempVar := t.TempDir()
	tempEtc := t.TempDir()
	statePath := filepath.Join(tempVar, "state.json")
	keyPath := filepath.Join(tempEtc, "state.key")
	backupPath := filepath.Join(tempVar, "backup.tar.gz")
	sessionPath := filepath.Join(tempVar, "sessions.json")

	if err := os.WriteFile(statePath, []byte(`{"settings":{"panelListen":"127.0.0.1:2096","mode":"server"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0xab}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	createCLIBackupDatabase(t, statePath)

	registry, err := api.NewSessionRegistry(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	preRestore, err := registry.Create(api.SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath+".journal", []byte("{\"operation\":\"upsert\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	create := NewRootCommand("test")
	create.SetOut(&bytes.Buffer{})
	create.SetErr(&bytes.Buffer{})
	create.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--output", backupPath,
		"--allow-unencrypted",
	})
	if err := create.Execute(); err != nil {
		t.Fatalf("backup create: %v", err)
	}

	postBackup, err := registry.Create(api.SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}

	restore := NewRootCommand("test")
	out := &bytes.Buffer{}
	restore.SetOut(out)
	restore.SetErr(out)
	restore.SetArgs([]string{
		"backup", "restore", backupPath,
		"--state", statePath,
		"--key-path", keyPath,
		"--yes",
	})
	if err := restore.Execute(); err != nil {
		t.Fatalf("backup restore: %v\n%s", err, out.String())
	}

	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("CLI restore left sessions.json: %v", err)
	}
	if _, err := os.Stat(sessionPath + ".journal"); !os.IsNotExist(err) {
		t.Fatalf("CLI restore left session journal: %v", err)
	}
	reloaded, err := api.NewSessionRegistry(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get(preRestore.Token); ok {
		t.Fatal("pre-backup session remained authorized after CLI restore")
	}
	if _, ok := reloaded.Get(postBackup.Token); ok {
		t.Fatal("post-backup session remained authorized after CLI restore")
	}
}

func TestCLIBackupRestoreCheckOnlyKeepsSessions(t *testing.T) {
	tempVar := t.TempDir()
	tempEtc := t.TempDir()
	statePath := filepath.Join(tempVar, "state.json")
	keyPath := filepath.Join(tempEtc, "state.key")
	backupPath := filepath.Join(tempVar, "backup.tar.gz")
	sessionPath := filepath.Join(tempVar, "sessions.json")

	if err := os.WriteFile(statePath, []byte(`{"settings":{"panelListen":"127.0.0.1:2096","mode":"server"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0xab}, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	createCLIBackupDatabase(t, statePath)
	registry, err := api.NewSessionRegistry(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	session, err := registry.Create(api.SessionCreateInput{Username: "alice", Role: "admin"})
	if err != nil {
		t.Fatal(err)
	}

	create := NewRootCommand("test")
	create.SetOut(&bytes.Buffer{})
	create.SetErr(&bytes.Buffer{})
	create.SetArgs([]string{
		"backup", "create",
		"--state", statePath,
		"--key-path", keyPath,
		"--output", backupPath,
		"--allow-unencrypted",
	})
	if err := create.Execute(); err != nil {
		t.Fatalf("backup create: %v", err)
	}

	check := NewRootCommand("test")
	check.SetOut(&bytes.Buffer{})
	check.SetErr(&bytes.Buffer{})
	check.SetArgs([]string{
		"backup", "restore", backupPath,
		"--state", statePath,
		"--key-path", keyPath,
		"--yes",
		"--check-only",
	})
	if err := check.Execute(); err != nil {
		t.Fatalf("backup restore --check-only: %v", err)
	}
	reloaded, err := api.NewSessionRegistry(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reloaded.Get(session.Token); !ok {
		t.Fatal("check-only restore invalidated live sessions")
	}
}
