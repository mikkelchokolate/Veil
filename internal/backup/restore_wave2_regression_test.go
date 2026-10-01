package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/storage"
	"golang.org/x/sys/unix"
)

// stageTwoMemberJournal simulates a crash mid-restore of a legacy archive
// (state.json + state.key only — no veil.db member): it stages both members,
// writes the durable journal, and publishes exactly publishCount members
// before "crashing" with the journal left behind.
func stageTwoMemberJournal(t *testing.T, statePath, keyPath string, intendedState, intendedKey []byte, publishCount int) restoreTransactionJournal {
	t.Helper()
	root := filepath.Dir(statePath)
	stateStaged, err := stageRestoreFile(statePath, intendedState, statePath+".pre-restore-crash")
	if err != nil {
		t.Fatal(err)
	}
	keyStaged, err := stageRestoreFile(keyPath, intendedKey, keyPath+".pre-restore-crash")
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
		if err := publishRestoreJournalFile(dirs, root, &journal, index); err != nil {
			t.Fatal(err)
		}
	}
	return journal
}

func writeLiveDatabase(t *testing.T, databasePath string) []byte {
	t.Helper()
	db, err := storage.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO migration_markers(key, version, applied_at, details) VALUES ('live-db', 1, 1, '{}')`); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	// A stale-but-live WAL sidecar: cleanupRestoreDatabaseSidecars must not
	// delete it for a journal that never staged veil.db (#1116 companion bug).
	if err := os.WriteFile(databasePath+"-wal", []byte("live-wal-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	return body
}

// TestRecoverInterruptedRestoreAcceptsTwoMemberJournal covers #1116: a
// legacy-archive restore legitimately journals only state.json+state.key.
// Recovery must decode that journal instead of demanding a veil.db member the
// interrupted restore never staged — the strict member set permanently wedged
// every later restore and panel startup on "missing restore journal member
// veil.db".
func TestRecoverInterruptedRestoreAcceptsTwoMemberJournal(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	databasePath := filepath.Join(root, "veil.db")

	oldState := []byte(`{"schemaVersion":4,"settings":{"mode":"server"}}`)
	oldKey := bytes.Repeat([]byte{0x11}, 32)
	if err := os.WriteFile(statePath, oldState, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, oldKey, 0o600); err != nil {
		t.Fatal(err)
	}
	liveDatabase := writeLiveDatabase(t, databasePath)

	// Crash after state.json was published but before state.key — the exact
	// "kill -9 during publish" window from the issue.
	stageTwoMemberJournal(t, statePath, keyPath,
		[]byte(`{"schemaVersion":4,"settings":{"mode":"server","domain":"restored.example"}}`),
		bytes.Repeat([]byte{0x22}, 32), 1)

	if err := RecoverInterruptedRestore(statePath, keyPath, databasePath); err != nil {
		t.Fatalf("recover interrupted two-member restore: %v", err)
	}

	gotState, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(gotState, oldState) {
		t.Fatalf("rollback did not restore previous state.json: %v", err)
	}
	gotKey, err := os.ReadFile(keyPath)
	if err != nil || !bytes.Equal(gotKey, oldKey) {
		t.Fatalf("rollback did not restore previous state.key: %v", err)
	}
	gotDatabase, err := os.ReadFile(databasePath)
	if err != nil || !bytes.Equal(gotDatabase, liveDatabase) {
		t.Fatalf("recovery touched the live veil.db a two-member journal never staged")
	}
	if _, err := os.Stat(databasePath + "-wal"); err != nil {
		t.Fatalf("recovery removed the live database's WAL sidecar: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, restoreTransactionJournalName)); !os.IsNotExist(err) {
		t.Fatal("restore journal survived completed recovery")
	}
	// Rolled back, not committed: no durable commit evidence may remain.
	committed, err := RestoreTransactionCommitted(statePath, keyPath, databasePath)
	if err != nil || committed {
		t.Fatalf("rolled-back restore reported committed=%v err=%v", committed, err)
	}
}

// TestRecoverCommittedTwoMemberJournalLeavesLiveDatabase covers the #1116
// companion bug: once a two-member journal decodes, the committed path must
// not run database-side recovery (sidecar cleanup, revision binding, fencing
// floor) against a live veil.db the restore never staged.
func TestRecoverCommittedTwoMemberJournalLeavesLiveDatabase(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	databasePath := filepath.Join(root, "veil.db")

	oldKey := bytes.Repeat([]byte{0x11}, 32)
	if err := os.WriteFile(statePath, []byte(`{"schemaVersion":4}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, oldKey, 0o600); err != nil {
		t.Fatal(err)
	}
	liveDatabase := writeLiveDatabase(t, databasePath)

	intendedState := []byte(`{"schemaVersion":4,"settings":{"mode":"server","domain":"restored.example"}}`)
	intendedKey := bytes.Repeat([]byte{0x22}, 32)
	// Crash after both members published but before journal finalization:
	// targets already hold the intended content.
	stageTwoMemberJournal(t, statePath, keyPath, intendedState, intendedKey, 2)

	if err := RecoverInterruptedRestore(statePath, keyPath, databasePath); err != nil {
		t.Fatalf("recover committed two-member restore: %v", err)
	}
	gotState, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(gotState, intendedState) {
		t.Fatal("committed restore did not keep the intended state.json")
	}
	gotKey, err := os.ReadFile(keyPath)
	if err != nil || !bytes.Equal(gotKey, intendedKey) {
		t.Fatal("committed restore did not keep the intended state.key")
	}
	gotDatabase, err := os.ReadFile(databasePath)
	if err != nil || !bytes.Equal(gotDatabase, liveDatabase) {
		t.Fatal("committed recovery modified a live veil.db the journal never staged")
	}
	if _, err := os.Stat(databasePath + "-wal"); err != nil {
		t.Fatalf("committed recovery deleted the live database's WAL sidecar: %v", err)
	}
	committed, err := RestoreTransactionCommitted(statePath, keyPath, databasePath)
	if err != nil || !committed {
		t.Fatalf("committed restore not reported committed=%v err=%v", committed, err)
	}
}

// TestPreflightChargesDecryptedInspectionWorkspace covers #1119: an encrypted
// archive's inspect workspace holds the decrypted tarball (≤maxBytes) AND the
// extracted members (≤maxBytes aggregate) at once, so the archive directory
// must be charged archiveSize + 2*maxBytes — not archiveSize + maxBytes.
func TestPreflightChargesDecryptedInspectionWorkspace(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "backup.tar.gz.enc")
	if err := os.WriteFile(archive, []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	const maxBytes = int64(10)
	const reserve = int64(64 * 1024 * 1024)
	archiveRequirement := int64(4) + 2*maxBytes + reserve

	old := backupStatfs
	t.Cleanup(func() { backupStatfs = old })
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(archiveRequirement - 1)
		return nil
	}
	if err := PreflightVerifySpace(archive, maxBytes); err == nil || !strings.Contains(err.Error(), "insufficient free space") {
		t.Fatalf("verify preflight accepted space short of the decrypted-workspace need: %v", err)
	}
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(archiveRequirement)
		return nil
	}
	if err := PreflightVerifySpace(archive, maxBytes); err != nil {
		t.Fatalf("verify preflight rejected exactly-sufficient space: %v", err)
	}

	// Same accounting on the restore preflight: state dir on the same
	// filesystem adds its own maxBytes + member sizes.
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	databasePath := filepath.Join(root, "veil.db")
	for path, body := range map[string][]byte{statePath: []byte("123"), keyPath: []byte("12"), databasePath: []byte("12345")} {
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	restoreRequirement := archiveRequirement + maxBytes + 3 + 2 + 5
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(restoreRequirement - 1)
		return nil
	}
	if err := PreflightRestoreSpace(archive, statePath, keyPath, databasePath, maxBytes); err == nil || !strings.Contains(err.Error(), "insufficient free space") {
		t.Fatalf("restore preflight accepted space short of the decrypted-workspace need: %v", err)
	}
	backupStatfs = func(_ string, stats *syscall.Statfs_t) error {
		stats.Bsize = 1
		stats.Bavail = uint64(restoreRequirement)
		return nil
	}
	if err := PreflightRestoreSpace(archive, statePath, keyPath, databasePath, maxBytes); err != nil {
		t.Fatalf("restore preflight rejected exactly-sufficient space: %v", err)
	}
}

// TestRestorePublishWaitsForCrossProcessSnapshotBarrier covers #1123: the
// capture→publish window (previous-digest capture through member replacement)
// must hold .veil-snapshot.lock — the same flock backup create takes — so a
// racing management-state commit from another process cannot slip between the
// safety digest capture and the safety rename.
func TestRestorePublishWaitsForCrossProcessSnapshotBarrier(t *testing.T) {
	fixture := prepareRestoreTripleFixture(t)
	lockPath := filepath.Join(filepath.Dir(fixture.statePath), ".veil-snapshot.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		t.Fatal(err)
	}
	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_EX); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := RestoreBackupFileWithOptions(fixture.archive, fixture.statePath, fixture.keyPath, "",
			RestoreOptions{DatabasePath: fixture.databasePath, AllowUnencrypted: true})
		done <- err
	}()

	// While a foreign process holds the barrier the restore must not reach
	// publish — no journal, no safety rename, no replacement.
	select {
	case err := <-done:
		t.Fatalf("restore completed while another holder owned the snapshot barrier: %v", err)
	case <-time.After(750 * time.Millisecond):
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(fixture.statePath), restoreTransactionJournalName)); !os.IsNotExist(err) {
		t.Fatal("restore staged a journal while the snapshot barrier was held")
	}

	if err := unix.Flock(int(lockFile.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if err := lockFile.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("restore after barrier release: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("restore did not finish after the snapshot barrier was released")
	}
	state, err := os.ReadFile(fixture.statePath)
	if err != nil || !bytes.Equal(state, fixture.intendedState) {
		t.Fatal("restore did not publish the intended state.json")
	}
}

// TestRestoreBackupWithOptionsJournalsCommit covers #1124: the byte-slice
// restore API must run the same journaled, barrier-held file path as
// RestoreBackupFileWithOptions — durable commit evidence (receipt), safeties
// and the snapshot lock all appear, so a crash is recoverable instead of
// leaving a torn triple.
func TestRestoreBackupWithOptionsJournalsCommit(t *testing.T) {
	fixture := prepareRestoreTripleFixture(t)
	data, err := os.ReadFile(fixture.archive)
	if err != nil {
		t.Fatal(err)
	}
	result, err := RestoreBackupWithOptions(data, fixture.statePath, fixture.keyPath, "",
		RestoreOptions{DatabasePath: fixture.databasePath, AllowUnencrypted: true})
	if err != nil {
		t.Fatalf("byte-slice restore: %v", err)
	}
	if !result.Verified || result.SafetyStatePath == "" || result.SafetyKeyPath == "" || result.SafetyDatabasePath == "" {
		t.Fatalf("journaled restore did not report safeties: %+v", result)
	}
	for _, pair := range []struct {
		path string
		want []byte
	}{
		{fixture.statePath, fixture.intendedState},
		{fixture.keyPath, fixture.intendedKey},
	} {
		got, err := os.ReadFile(pair.path)
		if err != nil || !bytes.Equal(got, pair.want) {
			t.Fatalf("byte-slice restore did not publish intended %s: %v", pair.path, err)
		}
	}
	// The published database is the archive member re-marked runtime-unknown
	// (applied_revision rewound by prepareRestoredDatabaseRuntimeUnknown), so
	// compare semantics rather than bytes.
	oldDatabase, err := os.ReadFile(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(oldDatabase, fixture.oldDatabase) {
		t.Fatal("byte-slice restore left the pre-restore veil.db in place")
	}
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatalf("restored database does not open: %v", err)
	}
	var runtimeStatus string
	if err := db.QueryRow(`SELECT status FROM runtime_verification WHERE id=1`).Scan(&runtimeStatus); err != nil || runtimeStatus != "unknown" {
		t.Fatalf("restored database runtime status=%q err=%v, want unknown", runtimeStatus, err)
	}
	var marker int
	if err := db.QueryRow(`SELECT COUNT(*) FROM migration_markers WHERE key='old-restore-triple'`).Scan(&marker); err != nil || marker != 0 {
		t.Fatalf("restored database still carries the pre-restore marker: count=%d err=%v", marker, err)
	}
	_ = db.Close()
	committed, err := RestoreTransactionCommitted(fixture.statePath, fixture.keyPath, fixture.databasePath)
	if err != nil || !committed {
		t.Fatalf("byte-slice restore left no durable commit evidence: committed=%v err=%v", committed, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(fixture.statePath), ".veil-snapshot.lock")); err != nil {
		t.Fatalf("byte-slice restore bypassed the snapshot barrier: %v", err)
	}
}

// TestRestoreBackupWithOptionsBoundsCallerInput covers the #1124 input bound:
// the caller-supplied byte slice is policy-bounded before it is staged.
func TestRestoreBackupWithOptionsBoundsCallerInput(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	old := []byte(`{"schemaVersion":4}`)
	if err := os.WriteFile(statePath, old, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RestoreBackupWithOptions(make([]byte, 128), statePath, keyPath, "", RestoreOptions{MaxBytes: 64}); err == nil ||
		!strings.Contains(err.Error(), "size policy") {
		t.Fatalf("oversized byte-slice restore error=%v", err)
	}
	got, err := os.ReadFile(statePath)
	if err != nil || !bytes.Equal(got, old) {
		t.Fatal("oversized byte-slice restore touched the target")
	}
}

// TestReadArchiveTarballAggregateSizeBound covers the #1124 aggregate bound:
// per-member limits alone let a multi-member tarball expand far past the
// configured policy in memory.
func TestReadArchiveTarballAggregateSizeBound(t *testing.T) {
	member := bytes.Repeat([]byte{0x42}, 90)
	tarball := buildTarball(t, []tarEntry{
		{name: "state.json", body: member},
		{name: "state.key", body: member},
		{name: "veil.db", body: member},
	})
	if _, err := readArchiveTarballWithMax(tarball, 200); err == nil ||
		!strings.Contains(err.Error(), "size policy") {
		t.Fatalf("aggregate 270B over a 200B policy was accepted: %v", err)
	}
}

// TestInterruptedRestoreResidueSweptOnlyWithoutJournal covers #1125: crash
// leftovers (staged member temps, atomicfile temps, db snapshots, inspect
// workspaces) are swept when no restore journal can still reference them —
// and never while a journal survives.
func TestInterruptedRestoreResidueSweptOnlyWithoutJournal(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	databasePath := filepath.Join(root, "veil.db")
	for path, body := range map[string][]byte{
		statePath:    []byte(`{"schemaVersion":4}`),
		keyPath:      bytes.Repeat([]byte{0x11}, 32),
		databasePath: {},
	} {
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	old := []string{".veil-restore-aaa", ".tmp-bbb", ".veil-backup-db-ccc.sqlite"}
	for _, name := range old {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("residue"), 0o600); err != nil {
			t.Fatal(err)
		}
		stale := time.Now().Add(-2 * restoreResidueMinAge)
		if err := os.Chtimes(path, stale, stale); err != nil {
			t.Fatal(err)
		}
	}
	staleDir := filepath.Join(root, ".veil-backup-inspect-stale")
	if err := os.MkdirAll(staleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	staleTime := time.Now().Add(-2 * restoreResidueMinAge)
	if err := os.Chtimes(staleDir, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}
	// Fresh entries may belong to a live operation and must survive the sweep.
	fresh := filepath.Join(root, ".veil-restore-live")
	if err := os.WriteFile(fresh, []byte("in-flight"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The commit receipt is evidence, not residue.
	receipt := filepath.Join(root, restoreCommitReceiptName)
	if err := os.WriteFile(receipt, []byte(`{"version":1,"committed":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := RecoverInterruptedRestore(statePath, keyPath, databasePath); err != nil {
		t.Fatalf("recovery sweep: %v", err)
	}
	for _, name := range old {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("stale restore residue %s survived the sweep", name)
		}
	}
	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Fatal("stale backup inspect workspace survived the sweep")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("sweep removed a fresh temp that may belong to a live operation")
	}
	if _, err := os.Stat(receipt); err != nil {
		t.Fatal("sweep removed the restore commit receipt")
	}

	// While a journal exists the residue stays — it may still be referenced
	// for rollback.
	journalPath := filepath.Join(root, restoreTransactionJournalName)
	if err := os.WriteFile(journalPath, []byte(`{"version":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	remaining := filepath.Join(root, ".tmp-still-referenced")
	if err := os.WriteFile(remaining, []byte("residue"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(remaining, staleTime, staleTime); err != nil {
		t.Fatal(err)
	}
	_ = RecoverInterruptedRestore(statePath, keyPath, databasePath) // decode error is fine
	if _, err := os.Stat(remaining); err != nil {
		t.Fatal("sweep ran while a restore journal still exists")
	}
}

// TestInspectSweepsStaleBackupWorkspaces covers the second #1125 channel:
// .veil-backup-* workspaces under the archive dir (decrypted tarballs holding
// raw key bytes) are swept on the next inspect/verify/restore — only once they
// are old enough that no live operation can still own them.
func TestInspectSweepsStaleBackupWorkspaces(t *testing.T) {
	root := t.TempDir()
	sourceState, sourceKey := writeValidBackupSource(t)
	archive := filepath.Join(root, "backup.tar.gz")
	if err := CreateBackupFileWithOptions(archive, sourceState, sourceKey, "", ArchiveOptions{
		DatabasePath: filepath.Join(filepath.Dir(sourceState), "veil.db"),
	}); err != nil {
		t.Fatal(err)
	}
	staleWorkspace := filepath.Join(root, ".veil-backup-inspect-stale")
	if err := os.MkdirAll(staleWorkspace, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staleWorkspace, "member-state-key"), []byte("raw-key-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-2 * restoreResidueMinAge)
	for _, path := range []string{staleWorkspace, filepath.Join(staleWorkspace, "member-state-key")} {
		if err := os.Chtimes(path, stale, stale); err != nil {
			t.Fatal(err)
		}
	}
	fresh := filepath.Join(root, ".veil-backup-publish-live")
	if err := os.WriteFile(fresh, []byte("in-flight"), 0o600); err != nil {
		t.Fatal(err)
	}

	verified, err := inspectBackupFileWithOptions(archive, "", DefaultMaxBackupBytes, CryptoOptions{}, true)
	if err != nil {
		t.Fatalf("inspect with stale workspace present: %v", err)
	}
	verified.cleanup()
	if _, err := os.Stat(staleWorkspace); !os.IsNotExist(err) {
		t.Fatal("stale inspect workspace with raw key bytes was never swept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("sweep removed a fresh backup temp")
	}
}

// TestStageRestoreFileRejectsSymlinkedTarget covers the #1125 edge case: a
// symlinked live state file must be rejected, not renamed into a safety slot
// that safety pruning then rejects forever as non-regular.
func TestStageRestoreFileRejectsSymlinkedTarget(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real-state.json")
	if err := os.WriteFile(real, []byte(`{"schemaVersion":4}`), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "state.json")
	if err := os.Symlink(real, target); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	safety := target + ".pre-restore-test"
	if _, err := stageRestoreFile(target, []byte(`{"schemaVersion":4,"settings":{}}`), safety); err == nil ||
		!strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlinked restore target error=%v", err)
	}
	// The symlink must stay in place — renaming it into the safety slot would
	// poison PruneRestoreSafetyFiles for every subsequent restore.
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("restore moved or replaced the symlinked live target")
	}
	if _, err := os.Lstat(safety); !os.IsNotExist(err) {
		t.Fatal("rejected restore still created a safety entry")
	}
	if got, err := os.ReadFile(real); err != nil || !bytes.Equal(got, []byte(`{"schemaVersion":4}`)) {
		t.Fatal("restore touched the file behind the symlink")
	}
}

// TestFileRestorePrunesRetainedSafeties covers the first #1125 channel: the
// file-based restore path used by `veil restore` and the helper now prunes
// .pre-restore-* safeties itself — they no longer accumulate forever when the
// privileged-helper prune never runs.
func TestFileRestorePrunesRetainedSafeties(t *testing.T) {
	fixture := prepareRestoreTripleFixture(t)
	// Four pre-existing safeties, oldest last — keep=2 must evict names[2:].
	var safetyNames []string
	for i := 0; i < 4; i++ {
		name := fixture.statePath + ".pre-restore-2020010" + string(rune('1'+i)) + "T000000.000000000Z"
		if err := os.WriteFile(name, fixture.oldState, 0o600); err != nil {
			t.Fatal(err)
		}
		aged := time.Now().Add(-time.Duration(2+i) * time.Hour)
		if err := os.Chtimes(name, aged, aged); err != nil {
			t.Fatal(err)
		}
		safetyNames = append(safetyNames, name)
	}
	if _, err := RestoreBackupFileWithOptions(fixture.archive, fixture.statePath, fixture.keyPath, "",
		RestoreOptions{DatabasePath: fixture.databasePath, AllowUnencrypted: true}); err != nil {
		t.Fatalf("restore with retained safeties: %v", err)
	}
	matches, err := filepath.Glob(fixture.statePath + ".pre-restore-*")
	if err != nil {
		t.Fatal(err)
	}
	// keep=2 prunes the two oldest pre-existing safeties; this restore's own
	// safety is added afterwards.
	if len(matches) != 3 {
		t.Fatalf("expected 2 retained + 1 new safety, got %d: %v", len(matches), matches)
	}
	for _, pruned := range safetyNames[2:] {
		if _, err := os.Lstat(pruned); !os.IsNotExist(err) {
			t.Fatalf("oldest safety %s was not pruned", filepath.Base(pruned))
		}
	}
	for _, kept := range safetyNames[:2] {
		if _, err := os.Lstat(kept); err != nil {
			t.Fatalf("recent safety %s was wrongly pruned: %v", filepath.Base(kept), err)
		}
	}
}
