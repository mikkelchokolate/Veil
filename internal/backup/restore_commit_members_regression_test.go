package backup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreJournalRequiresExactUniqueMemberSet(t *testing.T) {
	root := t.TempDir()
	expected := map[string]string{
		"state.json": filepath.Join(root, "state.json"),
		"state.key":  filepath.Join(root, "state.key"),
		"veil.db":    filepath.Join(root, "veil.db"),
	}
	base := restoreJournalDisk{
		Version: 2, TransactionID: "tx", Phase: "prepared",
		Files: []restoreJournalDiskFile{
			{Name: "state.json", TargetID: "state.json", StagedName: ".state.stage", SafetyName: ".state.old"},
			{Name: "state.key", TargetID: "state.key", StagedName: ".key.stage", SafetyName: ".key.old"},
			{Name: "veil.db", TargetID: "veil.db", StagedName: ".db.stage", SafetyName: ".db.old"},
		},
	}
	for _, tc := range []struct {
		name   string
		mutate func(*restoreJournalDisk)
		want   string
	}{
		{"duplicate", func(j *restoreJournalDisk) { j.Files[2] = j.Files[1] }, "duplicate"},
		{"missing", func(j *restoreJournalDisk) { j.Files = j.Files[:2] }, "missing"},
		{"unknown", func(j *restoreJournalDisk) { j.Files[2].Name, j.Files[2].TargetID = "other", "other" }, "target mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			journal := base
			journal.Files = append([]restoreJournalDiskFile(nil), base.Files...)
			tc.mutate(&journal)
			body, _ := json.Marshal(journal)
			if _, err := decodeRestoreJournal(body, expected); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("decode error=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestCommittedRestoreMarkerDeletionFailureFinalizesWithoutRollback(t *testing.T) {
	fixture := prepareRestoreTripleFixture(t)
	originalRemove := restoreJournalRemove
	failed := false
	restoreJournalRemove = func(path string) error {
		if !failed && filepath.Base(path) == restoreTransactionJournalName {
			failed = true
			return errors.New("injected committed restore marker unlink failure")
		}
		return os.Remove(path)
	}
	defer func() { restoreJournalRemove = originalRemove }()

	if _, err := RestoreBackupFileWithOptions(fixture.archive, fixture.statePath, fixture.keyPath, "",
		RestoreOptions{DatabasePath: fixture.databasePath}); err != nil {
		t.Fatalf("committed restore was reported failed: %v", err)
	}
	got := classifyRestoreTriple(t, fixture)
	if got == "mixed" {
		got = classifyRestoreTripleWithFencingFloor(t, fixture, 0)
	}
	if got != "intended" {
		t.Fatalf("committed restore was rolled back: %s", got)
	}

	restoreJournalRemove = originalRemove
	if _, err := RestoreBackupFileWithOptions(fixture.archive, fixture.statePath, fixture.keyPath, "",
		RestoreOptions{DatabasePath: fixture.databasePath, CheckOnly: true}); err != nil {
		t.Fatalf("finalize committed restore marker: %v", err)
	}
	got = classifyRestoreTriple(t, fixture)
	if got == "mixed" {
		got = classifyRestoreTripleWithFencingFloor(t, fixture, 0)
	}
	if got != "intended" {
		t.Fatalf("committed restore changed during recovery: %s", got)
	}
}
