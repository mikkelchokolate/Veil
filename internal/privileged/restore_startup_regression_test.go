package privileged

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultRecoveryRunsRestoreJournalBeforeKeyRotation(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	if err := os.WriteFile(filepath.Join(root, ".veil-restore-journal.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := NewProductionExecutor(ProductionConfig{StatePath: statePath, KeyPath: keyPath})
	err := executor.RecoverKeyRotation(context.Background())
	if err == nil || !strings.Contains(err.Error(), "backup restore") {
		t.Fatalf("recovery error = %v, want restore-journal failure before key rotation", err)
	}
}
