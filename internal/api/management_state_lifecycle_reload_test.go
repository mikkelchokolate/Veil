package api

import (
	"path/filepath"
	"testing"
)

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
