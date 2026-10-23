package statecommit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
)

func TestUpdateLoadsCurrentStateUnderBarrierInsteadOfSavingCallerStaleSnapshot(t *testing.T) {
	statePath, databasePath, cipher := stateCommitFixture(t)
	keyPath := filepath.Join(filepath.Dir(statePath), "state.key")
	if err := os.WriteFile(keyPath, cipher.KeyBytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	// This is the stale snapshot the old admin/repair paths loaded before Save
	// acquired the barrier.
	stale, ok, err := managementstate.NewStore(statePath, cipher).Load()
	if err != nil || !ok {
		t.Fatalf("load stale snapshot: ok=%v err=%v", ok, err)
	}

	panel := stale
	panel.Settings.Domain = "panel.example.com"
	if _, err := Save(panel, Options{StatePath: statePath, DatabasePath: databasePath, Cipher: cipher}); err != nil {
		t.Fatalf("panel mutation: %v", err)
	}

	if _, err := Update(UpdateOptions{
		StatePath: statePath, KeyPath: keyPath, DatabasePath: databasePath,
	}, func(current *model.ManagementSnapshot) error {
		if current.Settings.Domain != "panel.example.com" {
			t.Fatalf("Update observed stale domain %q", current.Settings.Domain)
		}
		current.Users = []model.User{{Username: "cli-admin", PasswordHash: "hash", Role: "admin"}}
		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	result, ok, err := managementstate.NewStore(statePath, cipher).Load()
	if err != nil || !ok {
		t.Fatalf("load result: ok=%v err=%v", ok, err)
	}
	if result.Settings.Domain != "panel.example.com" {
		t.Fatalf("CLI update reverted unrelated Panel domain to %q", result.Settings.Domain)
	}
	if len(result.Users) != 1 || result.Users[0].Username != "cli-admin" {
		t.Fatalf("CLI mutation missing from merged state: %+v", result.Users)
	}
}
