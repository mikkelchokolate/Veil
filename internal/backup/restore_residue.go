package backup

import (
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/safefs"
)

// restoreResidueMinAge gates residue sweeps: only entries strictly older than
// this are removed, so a temp a live in-flight backup/restore just created is
// never collected underneath it. Declared as a variable so tests can shrink it.
var restoreResidueMinAge = time.Hour

// restoreResiduePrefixes are the temp patterns a crashed restore or atomic
// write can leave in the state root. The journal and commit receipt are
// excluded by name — they are evidence, not residue.
var restoreResiduePrefixes = []string{".veil-restore-", ".tmp-", ".veil-backup-db-", ".veil-backup-inspect-"}

// sweepInterruptedRestoreResidue removes crash leftovers from the state root:
// staged .veil-restore-* member temps, atomicfile .tmp-* temps, orphaned
// .veil-backup-db-* snapshots and stale .veil-backup-inspect-* workspaces. It
// must only run when no restore journal exists — a surviving journal may still
// reference staged paths for rollback (#1125). The caller supplies the
// already-pinned root directory, so the journal check and every removal are
// descriptor-relative and cannot be redirected by a swapped path (#1219).
func sweepInterruptedRestoreResidue(rootDir *safefs.Dir) {
	if _, err := rootDir.StatAt(restoreTransactionJournalName); err == nil {
		return
	}
	sweepRestoreResidueEntries(rootDir, restoreResiduePrefixes, func(name string) bool {
		return name == restoreTransactionJournalName || name == restoreCommitReceiptName
	})
}

// sweepBackupWorkspaceResidue removes stale .veil-backup-* temp files and
// workspaces (inspection, creation, publish/pending temps, db snapshots) from
// the directory that hosts backup archives (#1125).
func sweepBackupWorkspaceResidue(dirPath string) {
	dir, err := safefs.OpenDir(dirPath)
	if err != nil {
		return
	}
	defer dir.Close()
	sweepRestoreResidueEntries(dir, []string{".veil-backup-"}, nil)
}

// sweepRestoreResidueEntries removes matched entries through the pinned
// directory handle: readdir names feed StatAt (lstat-equivalent) and
// RemoveAt/RemoveTreeAt, so a stale leaf swapped for a symlink is unlinked
// as the symlink itself — never followed — and a swapped workspace directory
// can only lose its own pinned subtree, not an attacker-chosen path (#1219).
func sweepRestoreResidueEntries(dir *safefs.Dir, prefixes []string, excluded func(string) bool) {
	entries, err := dir.ReadDir()
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-restoreResidueMinAge)
	for _, entry := range entries {
		name := entry.Name()
		if excluded != nil && excluded(name) {
			continue
		}
		match := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(name, prefix) {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		info, err := dir.StatAt(name)
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if info.IsDir() {
			_ = dir.RemoveTreeAt(name)
			continue
		}
		_ = dir.RemoveAt(name)
	}
}
