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
	if !strings.Contains(err.Error(), "remove it to recover") {
		t.Fatalf("refusal does not name the journal or how to clear it: %v", err)
	}
	if _, statErr := os.Stat(victim); statErr != nil {
		t.Fatalf("recovery removed a path outside BinDir: %v", statErr)
	}
}

func quote(value string) string {
	return `"` + value + `"`
}

// TestRuntimeSetActivationAcceptsRelativeBinDir covers `veil runtime install
// --bin-dir ./bin`: the writer used to store the caller's relative paths while
// the validator compared them with an absolute root, so the package rejected its
// own journal (#1267 review). Every entry point normalises the directory once
// and the journal carries absolute paths.
func TestRuntimeSetActivationAcceptsRelativeBinDir(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)

	payload := []byte("runtime relative")
	runtime := regressionRuntime("v1.0.0", payload)
	staged := installOne(t.Context(), regressionInstallOptions(t.TempDir(), runtime, payload), runtime)
	if staged.Err != nil {
		t.Fatal(staged.Err)
	}
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}

	journal, err := prepareRuntimeSetActivation("bin", []Result{staged})
	if err != nil {
		t.Fatalf("prepare with a relative bin dir: %v", err)
	}
	if !filepath.IsAbs(journal.Items[0].Target) {
		t.Fatalf("journal target %q is not absolute", journal.Items[0].Target)
	}
	// Recovery with the relative directory must read that journal instead of
	// rejecting it, so roll it back before activating.
	if err := recoverRuntimeSetActivation("bin"); err != nil {
		t.Fatalf("recovery with a relative bin dir: %v", err)
	}

	journal, err = prepareRuntimeSetActivation("bin", []Result{staged})
	if err != nil {
		t.Fatalf("second prepare with a relative bin dir: %v", err)
	}
	if err := activateRuntimeSet("bin", &journal); err != nil {
		t.Fatalf("activation with a relative bin dir: %v", err)
	}
}
