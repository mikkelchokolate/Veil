package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

type startupRecoveryClient struct {
	*recordingPrivilegedClient
	keyPath             string
	calledBeforeKeyLoad bool
}

func (c *startupRecoveryClient) RecoverKeyRotation(context.Context, privileged.RecoverKeyRotationRequest) error {
	_, err := os.Stat(c.keyPath)
	c.calledBeforeKeyLoad = errors.Is(err, os.ErrNotExist)
	return nil
}

func TestManagementStateStartupUsesPrivilegedRecoveryBeforeCipherLoad(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "state.key")
	client := &startupRecoveryClient{
		recordingPrivilegedClient: &recordingPrivilegedClient{},
		keyPath:                   keyPath,
	}
	state := newManagementState(ServerInfo{
		StatePath:               filepath.Join(dir, "state.json"),
		KeyPath:                 keyPath,
		Mode:                    "dev",
		Privileged:              client,
		RequirePrivilegedHelper: true,
	})
	defer closeClientSubsystem(state)
	if !client.calledBeforeKeyLoad {
		t.Fatal("privileged key-rotation recovery did not run before cipher construction")
	}
	if state.cipher == nil {
		t.Fatal("startup did not construct cipher after privileged recovery")
	}
}

func TestManagementStateLifecycleReloadLockedRefreshesPersistedState(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")
	state := newManagementState(ServerInfo{StatePath: statePath, KeyPath: keyPath, Mode: "dev"})
	state.settings.Domain = "new.example.com"
	if err := NewManagementStateLifecycle(state).SaveLocked(); err != nil {
		t.Fatalf("SaveLocked: %v", err)
	}
	state.settings.Domain = "old.example.com"

	if err := NewManagementStateLifecycle(state).ReloadLocked(); err != nil {
		t.Fatalf("ReloadLocked: %v", err)
	}
	if state.settings.Domain != "new.example.com" {
		t.Fatalf("domain = %q", state.settings.Domain)
	}
}

func TestManagementStateReloadUpdatesFailClosedLifecycleStatus(t *testing.T) {
	dir := t.TempDir()
	client := &recordingPrivilegedClient{}
	state := newManagementState(ServerInfo{
		StatePath:               filepath.Join(dir, "state.json"),
		KeyPath:                 filepath.Join(dir, "state.key"),
		ApplyRoot:               dir,
		Privileged:              client,
		RequirePrivilegedHelper: true,
	})
	defer closeClientSubsystem(state)
	state.startupStateLoadFailed = true
	state.startupStateLoadErr = errors.New("stale startup error")
	if err := state.Reload(); err != nil {
		t.Fatalf("successful reload: %v", err)
	}
	if state.startupStateLoadFailed || state.startupStateLoadErr != nil {
		t.Fatal("successful reload did not clear fail-closed lifecycle status")
	}

	client.err = errors.New("helper unavailable")
	if err := state.Reload(); err == nil {
		t.Fatal("failed privileged recovery returned nil")
	}
	if !state.startupStateLoadFailed || state.startupStateLoadErr == nil {
		t.Fatal("failed reload did not retain fail-closed lifecycle cause")
	}
}
