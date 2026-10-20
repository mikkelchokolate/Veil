package backup

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// splitRestoreRoots mirrors the production layout behind #1256: the journal
// and state.json live in the service-writable state root (/var/lib/veil),
// while state.key lives in a separate, root-owned key directory (/etc/veil)
// the service uid can read but not write.
func splitRestoreRoots(t *testing.T) (stateRoot, keyDir, statePath, keyPath string) {
	t.Helper()
	base := t.TempDir()
	stateRoot = filepath.Join(base, "var-lib-veil")
	keyDir = filepath.Join(base, "etc-veil")
	for _, dir := range []string{stateRoot, keyDir} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	statePath = filepath.Join(stateRoot, "state.json")
	keyPath = filepath.Join(keyDir, "state.key")
	if err := os.WriteFile(statePath, []byte("live-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("live-key"), 0o640); err != nil {
		t.Fatal(err)
	}
	return stateRoot, keyDir, statePath, keyPath
}

func plantRestoreJournal(t *testing.T, stateRoot string, disk restoreJournalDisk) {
	t.Helper()
	payload, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, restoreTransactionJournalName), payload, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFileBody(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Fatalf("%s was clobbered by recovery: body=%q err=%v (want %q)", path, got, err, want)
	}
}

// TestPlantedRestoreJournalCannotDestroyStateKey covers #1256: every digest
// the rollback trusted as "this transaction published it" is computable by
// anyone who can READ the live target, and state.key is root:veil 0640. A
// journal planted in the service-writable state root must never make the
// root-side recovery unlink state.key, rename another readable file over it,
// or roll it back to a retained older key. Each case lists the bogus
// state.json record first so the reverse-order rollback reaches state.key
// before anything else can fail, and every leaf name is chosen to look like
// a real transaction so the refusal has to come from journal
// authentication, not from name hygiene alone.
func TestPlantedRestoreJournalCannotDestroyStateKey(t *testing.T) {
	bogusState := restoreJournalDiskFile{
		Name: "state.json", TargetID: "state.json",
		StagedName: ".veil-restore-planted-state", SafetyName: "state.json.pre-restore-20260101T000000.000000000Z",
		IntendedDigest: backupChecksum([]byte("never-published-state")), Phase: "prepared",
	}
	tests := []struct {
		name  string
		setup func(t *testing.T, keyDir string)
		key   restoreJournalDiskFile
		check func(t *testing.T, keyDir string)
	}{
		{
			// The issue's primary arm: hadPrevious:false, no safety
			// leaf, intendedDigest = sha256(readable live key).
			name: "no_previous_unlink",
			key: restoreJournalDiskFile{
				Name: "state.key", TargetID: "state.key",
				StagedName: ".veil-restore-planted-key", SafetyName: "state.key.pre-restore-20260101T000000.000000000Z",
				IntendedDigest: backupChecksum([]byte("live-key")), Phase: "intended-published",
			},
		},
		{
			// Retained safeties from an earlier restore are kept (and
			// readable) in the key directory: a planted pair of
			// previousDigest=sha256(old safety) and intendedDigest=
			// sha256(live key) must not roll the live key back to it.
			name: "retained_safety_rename_over",
			setup: func(t *testing.T, keyDir string) {
				if err := os.WriteFile(filepath.Join(keyDir, "state.key.pre-restore-20250101T000000.000000000Z"), []byte("older-key"), 0o640); err != nil {
					t.Fatal(err)
				}
			},
			key: restoreJournalDiskFile{
				Name: "state.key", TargetID: "state.key",
				StagedName: ".veil-restore-planted-key", SafetyName: "state.key.pre-restore-20250101T000000.000000000Z",
				HadPrevious: true, PreviousDigest: backupChecksum([]byte("older-key")),
				IntendedDigest: backupChecksum([]byte("live-key")), Phase: "intended-published",
			},
			check: func(t *testing.T, keyDir string) {
				assertFileBody(t, filepath.Join(keyDir, "state.key.pre-restore-20250101T000000.000000000Z"), "older-key")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateRoot, keyDir, statePath, keyPath := splitRestoreRoots(t)
			if test.setup != nil {
				test.setup(t, keyDir)
			}
			plantRestoreJournal(t, stateRoot, restoreJournalDisk{
				Version: 2, TransactionID: "planted", Phase: "prepared", WALCleanupPhase: "pending",
				Files: []restoreJournalDiskFile{bogusState, test.key},
			})
			if err := RecoverInterruptedRestore(statePath, keyPath, ""); err == nil {
				t.Fatal("planted restore journal was accepted by root-side recovery")
			}
			assertFileBody(t, keyPath, "live-key")
			assertFileBody(t, statePath, "live-state")
			if test.check != nil {
				test.check(t, keyDir)
			}
		})
	}
}

// TestPlantedRestoreJournalLeafNamesStayInTransactionNamespace covers the
// #1256 addendum: stagedName/safetyName accepted any basename, so a planted
// journal could point the safety leaf at a sibling secret (veil.env) or the
// staged leaf at the target itself and have root rename/unlink them. Only
// the names a real restore generates (".veil-restore-*" staged temps and
// "<member>.pre-restore-*" safeties) may be referenced.
func TestPlantedRestoreJournalLeafNamesStayInTransactionNamespace(t *testing.T) {
	tests := []struct {
		name string
		key  restoreJournalDiskFile
	}{
		{
			name: "safety_points_at_sibling_secret",
			key: restoreJournalDiskFile{
				Name: "state.key", TargetID: "state.key",
				StagedName: ".veil-restore-planted-key", SafetyName: "veil.env",
				HadPrevious: true, PreviousDigest: backupChecksum([]byte("env-secrets")),
				IntendedDigest: backupChecksum([]byte("live-key")), Phase: "intended-published",
			},
		},
		{
			name: "staged_points_at_target",
			key: restoreJournalDiskFile{
				Name: "state.key", TargetID: "state.key",
				StagedName: "state.key", SafetyName: "state.key.pre-restore-20260101T000000.000000000Z",
				HadPrevious: true, PreviousDigest: backupChecksum([]byte("live-key")),
				IntendedDigest: backupChecksum([]byte("never-published-key")), Phase: "safety-published",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stateRoot, keyDir, statePath, keyPath := splitRestoreRoots(t)
			envPath := filepath.Join(keyDir, "veil.env")
			if err := os.WriteFile(envPath, []byte("env-secrets"), 0o640); err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(restoreJournalDisk{
				Version: 2, TransactionID: "planted", Phase: "prepared", WALCleanupPhase: "pending",
				Files: []restoreJournalDiskFile{
					{Name: "state.json", TargetID: "state.json", StagedName: ".veil-restore-planted-state", SafetyName: "state.json.pre-restore-x", IntendedDigest: backupChecksum([]byte("never-published-state")), Phase: "prepared"},
					test.key,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			// Name hygiene is enforced at decode time, independent of
			// journal authentication.
			if _, err := decodeRestoreJournal(body, map[string]string{"state.json": statePath, "state.key": keyPath}); err == nil {
				t.Fatal("journal leaf outside the restore transaction namespace was decoded")
			}
			// End to end, sealed with a valid key so the refusal must come
			// from the name check behind journal authentication.
			var disk restoreJournalDisk
			if err := json.Unmarshal(body, &disk); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stateRoot, restoreTransactionJournalName), sealTestRestoreJournal(t, keyDir, disk), 0o600); err != nil {
				t.Fatal(err)
			}
			err = RecoverInterruptedRestore(statePath, keyPath, "")
			if err == nil || !strings.Contains(err.Error(), "unsafe member names") {
				t.Fatalf("planted out-of-namespace journal was not refused by name: %v", err)
			}
			assertFileBody(t, keyPath, "live-key")
			assertFileBody(t, envPath, "env-secrets")
		})
	}
}

// TestReplayedRestoreJournalCannotUnlinkFirstRestoreKey covers the replay
// variant of #1256: a genuine journal from a first-ever restore (no previous
// state.key) captured while it was live must not, once the transaction has
// finished, be replayed to make recovery "roll back" — i.e. unlink — the
// key that restore installed. The attacker only needs to perturb the
// service-writable state.json so the intended set no longer matches.
func TestReplayedRestoreJournalCannotUnlinkFirstRestoreKey(t *testing.T) {
	stateRoot, _, statePath, keyPath := splitRestoreRoots(t)
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	restoredKey := bytes.Repeat([]byte{0x42}, 32)
	stateStaged, err := stageRestoreFile(statePath, []byte("restored-state"), statePath+".pre-restore-first")
	if err != nil {
		t.Fatal(err)
	}
	keyStaged, err := stageRestoreFile(keyPath, restoredKey, keyPath+".pre-restore-first")
	if err != nil {
		t.Fatal(err)
	}
	journal, err := prepareRestoreJournal(statePath, []*stagedRestoreFile{stateStaged, keyStaged}, []string{"state.json", "state.key"}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	dirs := restoreJournalDirs{}
	defer dirs.Close()
	for index := range journal.Files {
		if err := publishRestoreJournalFile(dirs, stateRoot, &journal, index); err != nil {
			t.Fatal(err)
		}
	}
	captured, err := os.ReadFile(filepath.Join(stateRoot, restoreTransactionJournalName))
	if err != nil {
		t.Fatal(err)
	}
	if err := completeRestoreJournal(stateRoot, "", &journal); err != nil {
		t.Fatal(err)
	}
	assertFileBody(t, keyPath, string(restoredKey))

	// Replay: perturb state.json, put the captured journal back.
	if err := os.WriteFile(statePath, []byte("attacker-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateRoot, restoreTransactionJournalName), captured, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecoverInterruptedRestore(statePath, keyPath, ""); err == nil {
		t.Fatal("replayed journal of a finished restore was accepted")
	}
	assertFileBody(t, keyPath, string(restoredKey))
}

// sealTestRestoreJournal mints a fresh seal key in keyDir (as a real prepare
// would) and returns the sealed encoding of disk. Tests use it to keep the
// guards BEHIND journal authentication exercised with a journal that passes
// authentication.
func sealTestRestoreJournal(t *testing.T, keyDir string, disk restoreJournalDisk) []byte {
	t.Helper()
	key, err := newRestoreJournalSealKey(keyDir)
	if err != nil {
		t.Fatal(err)
	}
	disk.Seal, err = computeRestoreJournalSeal(key, disk)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// stageSplitCrashJournal runs a genuine restore transaction across the split
// state root / key directory layout and "crashes" after publishCount members.
func stageSplitCrashJournal(t *testing.T, stateRoot, statePath, keyPath string, publishCount int) restoreTransactionJournal {
	t.Helper()
	stateStaged, err := stageRestoreFile(statePath, []byte("restored-state"), statePath+".pre-restore-crash")
	if err != nil {
		t.Fatal(err)
	}
	keyStaged, err := stageRestoreFile(keyPath, []byte("restored-key"), keyPath+".pre-restore-crash")
	if err != nil {
		t.Fatal(err)
	}
	journal, err := prepareRestoreJournal(statePath, []*stagedRestoreFile{stateStaged, keyStaged}, []string{"state.json", "state.key"}, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	dirs := restoreJournalDirs{}
	defer dirs.Close()
	for index := 0; index < publishCount; index++ {
		if err := publishRestoreJournalFile(dirs, stateRoot, &journal, index); err != nil {
			t.Fatal(err)
		}
	}
	return journal
}

// TestSealedRestoreJournalStillRollsBackGenuineCrash is the positive control
// for #1256: a genuine interrupted transaction with state.key in a separate
// key directory still rolls back to the exact previous pair, and the seal
// key is retired with the journal so the transaction cannot be replayed.
func TestSealedRestoreJournalStillRollsBackGenuineCrash(t *testing.T) {
	stateRoot, keyDir, statePath, keyPath := splitRestoreRoots(t)
	// Crash after state.json was published but before state.key.
	stageSplitCrashJournal(t, stateRoot, statePath, keyPath, 1)
	assertFileBody(t, statePath, "restored-state")
	if _, err := os.Stat(filepath.Join(keyDir, restoreJournalSealName)); err != nil {
		t.Fatalf("prepare did not mint the journal seal beside state.key: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateRoot, restoreJournalSealName)); !os.IsNotExist(err) {
		t.Fatalf("journal seal leaked into the service-writable state root: %v", err)
	}
	if err := RecoverInterruptedRestore(statePath, keyPath, ""); err != nil {
		t.Fatalf("genuine sealed journal was refused: %v", err)
	}
	assertFileBody(t, statePath, "live-state")
	assertFileBody(t, keyPath, "live-key")
	if _, err := os.Stat(filepath.Join(stateRoot, restoreTransactionJournalName)); !os.IsNotExist(err) {
		t.Fatalf("journal survived a completed rollback: %v", err)
	}
	if _, err := os.Stat(filepath.Join(keyDir, restoreJournalSealName)); !os.IsNotExist(err) {
		t.Fatalf("seal key survived its journal: %v", err)
	}
}

// TestTamperedSealedRestoreJournalIsRefused covers #1256: the seal covers
// the whole journal. Taking a genuine mid-flight journal and rewriting one
// member record into the destructive shape (hadPrevious:false, intended =
// sha256(live key)) while keeping the original seal must be refused before
// any mutation, and the seal key is retired with the quarantined journal.
func TestTamperedSealedRestoreJournalIsRefused(t *testing.T) {
	stateRoot, keyDir, statePath, keyPath := splitRestoreRoots(t)
	stageSplitCrashJournal(t, stateRoot, statePath, keyPath, 0)
	journalPath := filepath.Join(stateRoot, restoreTransactionJournalName)
	body, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	var disk restoreJournalDisk
	if err := json.Unmarshal(body, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.Seal == "" {
		t.Fatal("writer did not seal the journal")
	}
	for index := range disk.Files {
		if disk.Files[index].Name == "state.key" {
			disk.Files[index].HadPrevious = false
			disk.Files[index].PreviousDigest = ""
			disk.Files[index].IntendedDigest = backupChecksum([]byte("live-key"))
		}
	}
	tampered, err := json.Marshal(disk)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, tampered, 0o600); err != nil {
		t.Fatal(err)
	}
	err = RecoverInterruptedRestore(statePath, keyPath, "")
	if err == nil || !strings.Contains(err.Error(), "seal does not verify") {
		t.Fatalf("tampered sealed journal was not refused by its seal: %v", err)
	}
	assertFileBody(t, keyPath, "live-key")
	assertFileBody(t, statePath, "live-state")
	if _, err := os.Stat(filepath.Join(keyDir, restoreJournalSealName)); !os.IsNotExist(err) {
		t.Fatalf("seal key survived its quarantined journal: %v", err)
	}
}

// TestRestoreJournalSealMustBeOwnerOnly covers #1256: a seal key the
// service uid could read (group/other bits) proves nothing, so a journal
// "authenticated" by it is refused.
func TestRestoreJournalSealMustBeOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not observable on Windows")
	}
	stateRoot, keyDir, statePath, keyPath := splitRestoreRoots(t)
	stageSplitCrashJournal(t, stateRoot, statePath, keyPath, 1)
	if err := os.Chmod(filepath.Join(keyDir, restoreJournalSealName), 0o640); err != nil {
		t.Fatal(err)
	}
	err := RecoverInterruptedRestore(statePath, keyPath, "")
	if err == nil || !strings.Contains(err.Error(), "not a private owner-only file") {
		t.Fatalf("journal sealed by a group-readable key was accepted: %v", err)
	}
	assertFileBody(t, keyPath, "live-key")
}
