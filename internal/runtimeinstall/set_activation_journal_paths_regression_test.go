package runtimeinstall

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRecoverRuntimeSetRejectsJournalPathsOutsideBinDir locks the #1267
// residual: every path in a recovered journal is used for a digest check, an
// os.Remove or a copy. A journal whose Target/Backup/Staged escape BinDir would
// turn rollback into an arbitrary root-level remove, so recovery must refuse it
// before touching any of them.
func TestRecoverRuntimeSetRejectsJournalPathsOutsideBinDir(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(root, "victim.conf")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	journal := `{"version":1,"transactionId":"planted","phase":"intent","items":[` +
		`{"name":"caddy","target":` + quote(victim) + `,"staged":` + quote(filepath.Join(binDir, ".caddy.new.planted")) +
		`,"hadOld":false,"newDigest":"deadbeef","activated":false}]}`
	if err := os.WriteFile(filepath.Join(binDir, runtimeSetJournalName), []byte(journal), 0o600); err != nil {
		t.Fatal(err)
	}

	err := recoverRuntimeSetActivation(binDir)
	if err == nil || !strings.Contains(err.Error(), "outside the runtime bin directory") {
		t.Fatalf("recovery of a journal naming %q: %v", victim, err)
	}
	if _, statErr := os.Stat(victim); statErr != nil {
		t.Fatalf("recovery removed a path outside BinDir: %v", statErr)
	}
}

func quote(value string) string {
	return `"` + value + `"`
}
