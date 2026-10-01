package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreJournalContainsNoReplayableAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	for path, body := range map[string]string{statePath: "old-state", keyPath: "old-key"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stateStage := filepath.Join(root, ".restore-state-new")
	keyStage := filepath.Join(root, ".restore-key-new")
	if err := os.WriteFile(stateStage, []byte("new-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyStage, []byte("new-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := prepareRestoreJournal(statePath, []*stagedRestoreFile{
		{target: statePath, temp: stateStage, safety: filepath.Join(root, ".restore-state-old"), hadOriginal: true},
		{target: keyPath, temp: keyStage, safety: filepath.Join(root, ".restore-key-old"), hadOriginal: true},
	}, []string{"state.json", "state.key"}, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, restoreTransactionJournalName))
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(body)
	if strings.Contains(encoded, root) {
		t.Fatalf("restore journal contains replayable absolute root %q: %s", root, encoded)
	}
	for _, forbidden := range []string{"targetPath", "stagedPath", "safetyPath"} {
		if strings.Contains(encoded, `"`+forbidden+`"`) {
			t.Errorf("restore journal exposes attacker-replayable %s", forbidden)
		}
	}
}

func TestRestoreRecoveryRejectsUntrustedSafetyObjectsBeforeMutation(t *testing.T) {
	tests := []struct {
		name        string
		makeSafety  func(t *testing.T, root, outside string) string
		assertAfter func(t *testing.T, outside string, before os.FileInfo)
	}{
		{
			name:       "absolute_outside_path",
			makeSafety: func(_ *testing.T, _ string, outside string) string { return outside },
			assertAfter: func(t *testing.T, outside string, _ os.FileInfo) {
				if body, err := os.ReadFile(outside); err != nil || string(body) != "outside-previous" {
					t.Fatalf("outside safety source changed: body=%q err=%v", body, err)
				}
			},
		},
		{
			name: "symlink_inside_root",
			makeSafety: func(t *testing.T, root, outside string) string {
				path := filepath.Join(root, "state.safety")
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
				return path
			},
			assertAfter: func(t *testing.T, outside string, before os.FileInfo) {
				after, err := os.Stat(outside)
				if err != nil {
					t.Fatal(err)
				}
				if after.Mode().Perm() != before.Mode().Perm() {
					t.Fatalf("outside mode changed through safety symlink: %o -> %o", before.Mode().Perm(), after.Mode().Perm())
				}
			},
		},
		{
			name: "hardlink_inside_root",
			makeSafety: func(t *testing.T, root, outside string) string {
				path := filepath.Join(root, "state.safety")
				if err := os.Link(outside, path); err != nil {
					t.Fatal(err)
				}
				return path
			},
			assertAfter: func(t *testing.T, outside string, before os.FileInfo) {
				after, err := os.Stat(outside)
				if err != nil {
					t.Fatal(err)
				}
				if after.Mode().Perm() != before.Mode().Perm() {
					t.Fatalf("outside hardlink inode mode changed: %o -> %o", before.Mode().Perm(), after.Mode().Perm())
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			outside := filepath.Join(t.TempDir(), "outside-safety")
			if err := os.WriteFile(outside, []byte("outside-previous"), 0o640); err != nil {
				t.Fatal(err)
			}
			outsideBefore, err := os.Stat(outside)
			if err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(root, "state.json")
			keyPath := filepath.Join(root, "state.key")
			if err := os.WriteFile(statePath, []byte("intended-state"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(keyPath, []byte("intended-key"), 0o600); err != nil {
				t.Fatal(err)
			}
			stateSafety := test.makeSafety(t, root, outside)
			keySafety := filepath.Join(root, "key.safety")
			if err := os.WriteFile(keySafety, []byte("previous-key"), 0o600); err != nil {
				t.Fatal(err)
			}
			journal := restoreTransactionJournal{
				Version: 1, TransactionID: "attacker-controlled", Phase: "state.json-intended-published",
				Files: []restoreJournalFile{
					{Name: "state.json", TargetPath: statePath, StagedPath: filepath.Join(root, "state.stage"), SafetyPath: stateSafety, HadPrevious: true, PreviousDigest: backupChecksum([]byte("outside-previous")), Mode: 0o600, UID: os.Getuid(), GID: os.Getgid(), Phase: "intended-published"},
					{Name: "state.key", TargetPath: keyPath, StagedPath: filepath.Join(root, "key.stage"), SafetyPath: keySafety, HadPrevious: true, PreviousDigest: backupChecksum([]byte("previous-key")), Mode: 0o600, UID: os.Getuid(), GID: os.Getgid(), Phase: "intended-published"},
				},
			}
			payload, err := json.Marshal(journal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, restoreTransactionJournalName), payload, 0o600); err != nil {
				t.Fatal(err)
			}
			stateBefore, _ := os.ReadFile(statePath)
			keyBefore, _ := os.ReadFile(keyPath)
			err = RecoverInterruptedRestore(statePath, keyPath, "")
			if err == nil {
				t.Error("untrusted safety object was accepted")
			}
			stateAfter, _ := os.ReadFile(statePath)
			keyAfter, _ := os.ReadFile(keyPath)
			if string(stateAfter) != string(stateBefore) || string(keyAfter) != string(keyBefore) {
				t.Errorf("protected targets changed before safety validation: state=%q key=%q", stateAfter, keyAfter)
			}
			test.assertAfter(t, outside, outsideBefore)
		})
	}
}

// TestRestoreRollbackRefusesToUnlinkForeignTargets covers the #1219 residual
// found in review: `hadPrevious` is attacker-authored, so a planted journal
// claiming "no previous file existed" must not make recovery unlink live
// state.json/state.key. Rollback only removes a target whose live digest
// equals the transaction-published intended digest.
func TestRestoreRollbackRefusesToUnlinkForeignTargets(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	for path, body := range map[string]string{statePath: "live-state", keyPath: "live-key"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	disk := restoreJournalDisk{
		Version: 2, TransactionID: "planted", Phase: "prepared", WALCleanupPhase: "pending",
		Files: []restoreJournalDiskFile{
			{Name: "state.json", TargetID: "state.json", StagedName: ".restore-state-new", SafetyName: ".restore-state-old", IntendedDigest: backupChecksum([]byte("planted-state")), Phase: "prepared"},
			{Name: "state.key", TargetID: "state.key", StagedName: ".restore-key-new", SafetyName: ".restore-key-old", IntendedDigest: backupChecksum([]byte("planted-key")), Phase: "prepared"},
		},
	}
	payload, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, restoreTransactionJournalName), payload, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RecoverInterruptedRestore(statePath, keyPath, ""); err == nil {
		t.Fatal("planted hadPrevious:false journal was silently accepted")
	}
	for path, want := range map[string]string{statePath: "live-state", keyPath: "live-key"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("recovery unlinked live member %s: body=%q err=%v", path, got, err)
		}
	}
}

// TestRestoreRollbackRejectsPlantedSafetyLeaf covers the remaining #1219
// rename-before-verify gaps: a planted journal can drop a forged
// ".restore-state-old" leaf next to the target and either claim
// hadPrevious:false or fully forge a hadPrevious/previousDigest pair.
// Rollback must refuse before any rename unless live evidence shows the
// transaction actually published the member (target or intact staged
// leaf hashing to IntendedDigest), so state.json keeps its live content.
// Rollback walks members in reverse order, so the planted-safety record
// is listed LAST to be exercised first.
func TestRestoreRollbackRejectsPlantedSafetyLeaf(t *testing.T) {
	tests := []struct {
		name        string
		record      func() restoreJournalDiskFile
		plantStaged bool
		wantErrPart string
	}{
		{
			name: "claimed_no_previous",
			record: func() restoreJournalDiskFile {
				return restoreJournalDiskFile{
					Name: "state.json", TargetID: "state.json",
					StagedName: ".restore-state-new", SafetyName: ".restore-state-old",
					IntendedDigest: backupChecksum([]byte("planted-state")), Phase: "prepared",
				}
			},
			wantErrPart: "never published",
		},
		{
			name: "forged_consistent_previous",
			record: func() restoreJournalDiskFile {
				return restoreJournalDiskFile{
					Name: "state.json", TargetID: "state.json",
					StagedName: ".restore-state-new", SafetyName: ".restore-state-old",
					HadPrevious: true, PreviousDigest: backupChecksum([]byte("forged-state")),
					IntendedDigest: backupChecksum([]byte("planted-state")), Phase: "prepared",
				}
			},
			wantErrPart: "never published",
		},
		{
			name: "staged_beside_live_target",
			record: func() restoreJournalDiskFile {
				return restoreJournalDiskFile{
					Name: "state.json", TargetID: "state.json",
					StagedName: ".restore-state-new", SafetyName: ".restore-state-old",
					HadPrevious: true, PreviousDigest: backupChecksum([]byte("forged-state")),
					IntendedDigest: backupChecksum([]byte("planted-state")), Phase: "prepared",
				}
			},
			plantStaged: true,
			wantErrPart: "never published",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			statePath := filepath.Join(root, "state.json")
			keyPath := filepath.Join(root, "state.key")
			for path, body := range map[string]string{statePath: "live-state", keyPath: "live-key"} {
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// The attacker drops a forged "previous" inode for state.json.
			if err := os.WriteFile(filepath.Join(root, ".restore-state-old"), []byte("forged-state"), 0o600); err != nil {
				t.Fatal(err)
			}
			if test.plantStaged {
				// A staged leaf hashing to IntendedDigest counts as
				// publish evidence only while the target is absent —
				// beside a live target it is forgeable noise (#1219).
				if err := os.WriteFile(filepath.Join(root, ".restore-state-new"), []byte("planted-state"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			disk := restoreJournalDisk{
				Version: 2, TransactionID: "planted", Phase: "prepared", WALCleanupPhase: "pending",
				Files: []restoreJournalDiskFile{
					{Name: "state.key", TargetID: "state.key", StagedName: ".restore-key-new", SafetyName: ".restore-key-old", IntendedDigest: backupChecksum([]byte("planted-key")), Phase: "prepared"},
					test.record(),
				},
			}
			payload, err := json.Marshal(disk)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, restoreTransactionJournalName), payload, 0o600); err != nil {
				t.Fatal(err)
			}

			err = RecoverInterruptedRestore(statePath, keyPath, "")
			if err == nil {
				t.Fatal("planted journal+safety pair was silently accepted")
			}
			if !strings.Contains(err.Error(), test.wantErrPart) {
				t.Fatalf("recovery failed for the wrong reason: %v", err)
			}
			got, err := os.ReadFile(statePath)
			if err != nil || string(got) != "live-state" {
				t.Fatalf("planted safety leaf clobbered live state.json: body=%q err=%v", got, err)
			}
		})
	}
}

// TestMalformedRestoreJournalQuarantinedOnce covers #1219: a journal that
// fails validation is renamed to a non-replayable quarantine leaf and the
// failure is reported once — recovery must neither replay the poisoned
// journal nor wedge every later startup on it.
func TestMalformedRestoreJournalQuarantinedOnce(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	for path, body := range map[string]string{statePath: "live-state", keyPath: "live-key"} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	journalPath := filepath.Join(root, restoreTransactionJournalName)
	if err := os.WriteFile(journalPath, []byte(`{"version":2,"phase":`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RecoverInterruptedRestore(statePath, keyPath, ""); err == nil {
		t.Fatal("malformed restore journal was accepted")
	}
	if _, err := os.Lstat(journalPath); !os.IsNotExist(err) {
		t.Fatalf("malformed journal was not quarantined: %v", err)
	}
	quarantined, err := filepath.Glob(filepath.Join(root, "veil-restore-journal.failed-*"))
	if err != nil || len(quarantined) != 1 {
		t.Fatalf("quarantined journal artifacts=%v err=%v", quarantined, err)
	}
	// The quarantined journal is never replayed: recovery is a clean no-op
	// on the next entry, and the live members were never touched.
	if err := RecoverInterruptedRestore(statePath, keyPath, ""); err != nil {
		t.Fatalf("quarantined journal replayed on re-entry: %v", err)
	}
	for path, want := range map[string]string{statePath: "live-state", keyPath: "live-key"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("recovery touched live member %s: body=%q err=%v", path, got, err)
		}
	}
}
