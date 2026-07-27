package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPanelDoesNotOpenDatabaseWhenPrivilegedRestoreRecoveryFails(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	if err := os.WriteFile(filepath.Join(root, ".veil-restore-journal.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, reloader := NewRouter(ServerInfo{StatePath: statePath})
	state := reloader.(*managementState)
	if !state.startupStateLoadFailed {
		t.Fatal("startup must fail closed when privileged restore recovery fails")
	}
	if state.db != nil {
		t.Fatal("Panel opened veil.db before privileged restore recovery completed")
	}
	if _, err := os.Stat(filepath.Join(root, "state.key")); !os.IsNotExist(err) {
		t.Fatalf("Panel opened/created state.key before restore recovery: %v", err)
	}
}
