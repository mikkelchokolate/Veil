package backup

import (
	"os"
	"path/filepath"
	"strings"
	"time"
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
// reference staged paths for rollback (#1125).
func sweepInterruptedRestoreResidue(root string) {
	if _, err := os.Lstat(filepath.Join(root, restoreTransactionJournalName)); err == nil {
		return
	}
	sweepRestoreResidueEntries(root, restoreResiduePrefixes, func(name string) bool {
		return name == restoreTransactionJournalName || name == restoreCommitReceiptName
	})
}

// sweepBackupWorkspaceResidue removes stale .veil-backup-* temp files and
// workspaces (inspection, creation, publish/pending temps, db snapshots) from
// the directory that hosts backup archives (#1125).
func sweepBackupWorkspaceResidue(dir string) {
	sweepRestoreResidueEntries(dir, []string{".veil-backup-"}, nil)
}

func sweepRestoreResidueEntries(dir string, prefixes []string, excluded func(string) bool) {
	entries, err := os.ReadDir(dir)
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
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		path := filepath.Join(dir, name)
		if entry.IsDir() {
			_ = os.RemoveAll(path)
			continue
		}
		_ = os.Remove(path)
	}
}
