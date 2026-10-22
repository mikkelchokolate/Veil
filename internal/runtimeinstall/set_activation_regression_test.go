package runtimeinstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRuntimeInstallCancellationAfterVerificationDoesNotPublish(t *testing.T) {
	binDir := t.TempDir()
	oldPayload := []byte("old runtime")
	oldRuntime := regressionRuntime("v1.2.3", oldPayload)
	if result := Install(t.Context(), regressionInstallOptions(binDir, oldRuntime, oldPayload), oldRuntime); result.Err != nil {
		t.Fatal(result.Err)
	}
	manifestPath := filepath.Join(binDir, runtimeSetManifestName)
	oldManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	newPayload := []byte("new runtime")
	newRuntime := regressionRuntime("v1.2.4", newPayload)
	opts := regressionInstallOptions(binDir, newRuntime, newPayload)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	opts.RunVersion = func(context.Context, string, []string) (string, error) {
		cancel()
		return "regression-runtime v1.2.4", nil
	}
	if result := Install(ctx, opts, newRuntime); !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("expected cancellation after successful verification, got %+v", result)
	}
	active, err := os.ReadFile(filepath.Join(binDir, oldRuntime.Binary))
	if err != nil || string(active) != string(oldPayload) {
		t.Fatalf("active runtime changed: %q %v", active, err)
	}
	manifest, err := os.ReadFile(manifestPath)
	if err != nil || string(manifest) != string(oldManifest) {
		t.Fatalf("generation manifest changed: %s %v", manifest, err)
	}
	if _, err := os.Stat(filepath.Join(binDir, runtimeSetJournalName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled install prepared a live transaction: %v", err)
	}
}

func TestRuntimeSetRecoveryRollsBackEntirePartiallyActivatedGeneration(t *testing.T) {
	binDir := t.TempDir()
	transactionID := "0123456789abcdef0123456789abcdef"
	items := make([]runtimeSetItem, 0, 2)
	for index, name := range []string{"one", "two"} {
		target := filepath.Join(binDir, name)
		backup := filepath.Join(binDir, "."+name+".old."+transactionID)
		staged := filepath.Join(binDir, "."+name+".new."+transactionID)
		oldBody := []byte("old-" + name)
		newBody := []byte("new-" + name)
		if err := os.WriteFile(target, oldBody, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(backup, oldBody, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(staged, newBody, 0o755); err != nil {
			t.Fatal(err)
		}
		oldDigest, _ := digestRuntimeFile(target)
		newDigest, _ := digestRuntimeFile(staged)
		items = append(items, runtimeSetItem{Name: name, Target: target, Backup: backup, Staged: staged, HadOld: true, OldDigest: oldDigest, NewDigest: newDigest, Activated: index == 0})
	}
	if err := os.Rename(items[0].Staged, items[0].Target); err != nil {
		t.Fatal(err)
	}
	journal := runtimeSetJournal{Version: 1, TransactionID: transactionID, Phase: "intent", Items: items, UpdatedAt: time.Now().Unix()}
	if err := writeRuntimeJSONAtomic(filepath.Join(binDir, runtimeSetJournalName), journal, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := recoverRuntimeSetActivation(binDir); err != nil {
		t.Fatalf("recover interrupted generation: %v", err)
	}
	for _, item := range items {
		digest, err := digestRuntimeFile(item.Target)
		if err != nil {
			t.Fatal(err)
		}
		if digest != item.OldDigest {
			t.Fatalf("target %s was not rolled back atomically", item.Name)
		}
	}
}

func TestCleanupRuntimeStagesRemovesAbandonedSetTransactions(t *testing.T) {
	binDir := t.TempDir()
	tx := "0123456789abcdef0123456789abcdef"
	stale := []string{
		".veil-runtime-set-stage-abandoned",
		".hysteria.new." + tx,
		".caddy.old." + tx,
		".mieru.new." + tx + ".tmp",
	}
	for _, name := range stale {
		path := filepath.Join(binDir, name)
		if strings.Contains(name, "set-stage") {
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keep := filepath.Join(binDir, ".operator-runtime-note")
	if err := os.WriteFile(keep, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cleanupRuntimeStages(binDir); err != nil {
		t.Fatalf("cleanupRuntimeStages: %v", err)
	}
	for _, name := range stale {
		if _, err := os.Stat(filepath.Join(binDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stale runtime scratch %q remains: %v", name, err)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("unrelated operator file was removed: %v", err)
	}
}
