package runtimeinstall

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
