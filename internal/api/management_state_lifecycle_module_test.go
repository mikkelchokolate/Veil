package api

import (
	"path/filepath"
	"testing"
)

func TestManagementStateLifecycleSavesAndLoadsSnapshot(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")
	state := newManagementState(ServerInfo{StatePath: statePath, KeyPath: keyPath, Mode: "dev"})
	state.settings.Domain = "example.com"
	state.inbounds = []Inbound{{Name: "default", Protocol: "naiveproxy"}}

	if err := NewManagementStateLifecycle(state).SaveLocked(); err != nil {
		t.Fatalf("SaveLocked: %v", err)
	}
	reloaded := newManagementState(ServerInfo{StatePath: statePath, KeyPath: keyPath, Mode: "dev"})
	if reloaded.settings.Domain != "example.com" || len(reloaded.inbounds) != 1 || reloaded.inbounds[0].Name != "default" {
		t.Fatalf("reloaded state = %+v inbounds=%+v", reloaded.settings, reloaded.inbounds)
	}
}
