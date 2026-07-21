package api

import (
	"path/filepath"
	"testing"
)

func TestManagementStateWithoutPathsUsesEphemeralRuntimeRoots(t *testing.T) {
	state := newManagementState(ServerInfo{Mode: "dev"})
	if state.applyRoot == "/etc/veil" || state.liveRoot == "/etc/veil/live" {
		t.Fatalf("bare state used production roots: apply=%q live=%q", state.applyRoot, state.liveRoot)
	}
	if filepath.Dir(state.applyRoot) != filepath.Dir(state.keyPath) {
		t.Fatalf("ephemeral apply root %q is not beside key %q", state.applyRoot, state.keyPath)
	}
}

func TestManagementStateWithExplicitStatePathDerivesIsolatedRuntimeRoots(t *testing.T) {
	dir := t.TempDir()
	state := newManagementState(ServerInfo{StatePath: filepath.Join(dir, "state.json"), Mode: "dev"})

	wantApplyRoot := filepath.Join(dir, "staging")
	if state.applyRoot != wantApplyRoot {
		t.Fatalf("applyRoot = %q, want %q", state.applyRoot, wantApplyRoot)
	}
	wantLiveRoot := filepath.Join(wantApplyRoot, "live")
	if state.liveRoot != wantLiveRoot {
		t.Fatalf("liveRoot = %q, want %q", state.liveRoot, wantLiveRoot)
	}
}

func TestManagementStateLifecycleSavesAndLoadsSnapshot(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	keyPath := filepath.Join(dir, "state.key")
	state := newManagementState(ServerInfo{StatePath: statePath, KeyPath: keyPath, Mode: "dev"})
	state.setup = SetupState{Completed: true, CompletedAt: "2026-06-05T12:00:00Z"}
	state.settings.Domain = "example.com"
	state.inbounds = []Inbound{{Name: "default", Protocol: "naiveproxy"}}

	if err := NewManagementStateLifecycle(state).SaveLocked(); err != nil {
		t.Fatalf("SaveLocked: %v", err)
	}
	reloaded := newManagementState(ServerInfo{StatePath: statePath, KeyPath: keyPath, Mode: "dev"})
	if reloaded.setup != state.setup || reloaded.settings.Domain != "example.com" || len(reloaded.inbounds) != 1 || reloaded.inbounds[0].Name != "default" {
		t.Fatalf("reloaded setup=%+v state=%+v inbounds=%+v", reloaded.setup, reloaded.settings, reloaded.inbounds)
	}
}
