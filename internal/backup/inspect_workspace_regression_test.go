package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestInspectWorkspaceLeavesTheArchiveDirectory locks #1254 F2: the decrypted
// tarball and the extracted members were staged in a .veil-backup-inspect-*
// directory created next to the archive — that is /var/lib/veil/backups, a
// directory the panel service owns and can write. A local writer could rename
// the workspace away and substitute bytes between the create and the member
// reads, forging the verification report and the journaled commit, so the
// workspace must live in the private temp root the service cannot modify.
func TestInspectWorkspaceLeavesTheArchiveDirectory(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	archiveDir := t.TempDir()
	archivePath := filepath.Join(archiveDir, "backup.tar.gz.enc")
	passphrase := strings.Repeat("inspect-workspace-", 2)
	if err := CreateBackupFileWithOptions(archivePath, statePath, keyPath, passphrase, ArchiveOptions{}); err != nil {
		t.Fatalf("create backup: %v", err)
	}

	privateRoot := t.TempDir()
	t.Setenv("TMPDIR", privateRoot)
	var workspaces []string
	previous := inspectWorkspaceCreated
	inspectWorkspaceCreated = func(path string) { workspaces = append(workspaces, path) }
	t.Cleanup(func() { inspectWorkspaceCreated = previous })

	if _, err := VerifyBackupFile(archivePath, passphrase, 0); err != nil {
		t.Fatalf("verify backup: %v", err)
	}
	if len(workspaces) == 0 {
		t.Fatal("verification created no inspection workspace to inspect")
	}
	for _, workspace := range workspaces {
		if within(archiveDir, workspace) {
			t.Fatalf("inspection workspace %q is inside the archive directory %q", workspace, archiveDir)
		}
		if !within(privateRoot, workspace) {
			t.Fatalf("inspection workspace %q is outside the private temp root %q", workspace, privateRoot)
		}
	}
}

// TestInspectWorkspaceResidueIsSweptFromTheTempRoot keeps a crashed inspection
// from leaving decrypted state behind now that the workspace lives there: a
// stale workspace is collected, a fresh one (which may belong to a live
// operation) is kept.
func TestInspectWorkspaceResidueIsSweptFromTheTempRoot(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	archiveDir := t.TempDir()
	archivePath := filepath.Join(archiveDir, "backup.tar.gz.enc")
	passphrase := strings.Repeat("inspect-residue-", 2)
	if err := CreateBackupFileWithOptions(archivePath, statePath, keyPath, passphrase, ArchiveOptions{}); err != nil {
		t.Fatalf("create backup: %v", err)
	}

	privateRoot := t.TempDir()
	stale := filepath.Join(privateRoot, ".veil-backup-inspect-stale")
	fresh := filepath.Join(privateRoot, ".veil-backup-inspect-fresh")
	for _, dir := range []string{stale, fresh} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("decrypted"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * restoreResidueMinAge)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", privateRoot)

	if _, err := VerifyBackupFile(archivePath, passphrase, 0); err != nil {
		t.Fatalf("verify backup: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale inspection workspace survived the sweep: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh inspection workspace was removed: %v", err)
	}
}

// within reports whether path is root itself or a path below it.
func within(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

// TestInspectWorkspaceRejectsASharedTempRoot keeps the F2 window closed when an
// operator points TMPDIR at a directory other users can write: without the
// sticky bit such a directory does not protect the workspace from a rename.
func TestInspectWorkspaceRejectsASharedTempRoot(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	archiveDir := t.TempDir()
	archivePath := filepath.Join(archiveDir, "backup.tar.gz.enc")
	passphrase := strings.Repeat("inspect-shared-", 2)
	if err := CreateBackupFileWithOptions(archivePath, statePath, keyPath, passphrase, ArchiveOptions{}); err != nil {
		t.Fatalf("create backup: %v", err)
	}

	shared := t.TempDir()
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", shared)

	if _, err := VerifyBackupFile(archivePath, passphrase, 0); err == nil || !strings.Contains(err.Error(), "sticky bit") {
		t.Fatalf("shared temp root accepted: %v", err)
	}
}

// TestInspectWorkspaceRejectsAForeignOwnedTempRoot keeps the F2 window closed
// when TMPDIR points at a directory another user owns: such a directory can be
// renamed away and replaced between the check and the workspace creation, even
// though it is not group/other-writable. The ownership seams stand in for the
// chown a non-root CI cannot make.
func TestInspectWorkspaceRejectsAForeignOwnedTempRoot(t *testing.T) {
	statePath, keyPath := writeValidBackupSource(t)
	archiveDir := t.TempDir()
	archivePath := filepath.Join(archiveDir, "backup.tar.gz.enc")
	passphrase := strings.Repeat("inspect-foreign-", 2)
	if err := CreateBackupFileWithOptions(archivePath, statePath, keyPath, passphrase, ArchiveOptions{}); err != nil {
		t.Fatalf("create backup: %v", err)
	}

	foreign := t.TempDir()
	t.Setenv("TMPDIR", foreign)
	current := workspaceCurrentUID()
	previousOwner, previousUID := workspaceRootOwner, workspaceCurrentUID
	workspaceRootOwner = func(os.FileInfo) int { return current + 1 }
	t.Cleanup(func() { workspaceRootOwner, workspaceCurrentUID = previousOwner, previousUID })

	if _, err := VerifyBackupFile(archivePath, passphrase, 0); err == nil || !strings.Contains(err.Error(), "is owned by uid") {
		t.Fatalf("foreign-owned temp root accepted: %v", err)
	}
}
