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
