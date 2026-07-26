package statecommit

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/storage"
	"golang.org/x/sys/unix"
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

func TestUpdateWaitsForConcurrentPanelCommitThenMergesCurrentState(t *testing.T) {
	statePath, databasePath, cipher := stateCommitFixture(t)
	keyPath := filepath.Join(filepath.Dir(statePath), "state.key")
	if err := os.WriteFile(keyPath, cipher.KeyBytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	lock, err := os.OpenFile(filepath.Join(filepath.Dir(statePath), ".veil-snapshot.lock"), os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			_ = unix.Flock(int(lock.Fd()), unix.LOCK_UN)
		}
	}()

	updateStarted := make(chan struct{})
	updateDone := make(chan error, 1)
	go func() {
		_, err := Update(UpdateOptions{
			StatePath: statePath, KeyPath: keyPath, DatabasePath: databasePath,
			beforeBarrier: func() { close(updateStarted) },
		}, func(current *model.ManagementSnapshot) error {
			if current.Settings.Domain != "panel.example.com" {
				return fmt.Errorf("CLI observed stale Panel domain %q", current.Settings.Domain)
			}
			current.Users = []model.User{{Username: "cli-admin", PasswordHash: "hash", Role: "admin"}}
			return nil
		})
		updateDone <- err
	}()
	<-updateStarted

	// The test owns the same flock that a Panel mutation owns. Commit the Panel
	// change through the unchanged state/SQLite protocol before allowing Update
	// to take the barrier and perform its authoritative read.
	panel, ok, err := managementstate.NewStore(statePath, cipher).Load()
	if err != nil || !ok {
		t.Fatalf("load Panel state: ok=%v err=%v", ok, err)
	}
	panel.Settings.Domain = "panel.example.com"
	db, err := storage.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saveWithDBLocked(db, panel, Options{
		StatePath: statePath, DatabasePath: databasePath, Cipher: cipher,
	}); err != nil {
		_ = db.Close()
		t.Fatalf("Panel commit: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	locked = false
	if err := <-updateDone; err != nil {
		t.Fatalf("concurrent Update: %v", err)
	}

	result, ok, err := managementstate.NewStore(statePath, cipher).Load()
	if err != nil || !ok {
		t.Fatalf("load result: ok=%v err=%v", ok, err)
	}
	if result.Settings.Domain != "panel.example.com" || len(result.Users) != 1 || result.Users[0].Username != "cli-admin" {
		t.Fatalf("lost concurrent update: domain=%q users=%+v", result.Settings.Domain, result.Users)
	}
}
