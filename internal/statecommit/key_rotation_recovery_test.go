package statecommit

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

func TestRotateKeyRecoveryAfterNewKeyPublicationBeforeStatePublication(t *testing.T) {
	testInterruptedKeyRotationRecovery(t, rotationPhaseKeyPublished, false)
}

func TestRotateKeyRecoveryAfterBothFilesPublicationBeforeSQLiteCommit(t *testing.T) {
	testInterruptedKeyRotationRecovery(t, rotationPhaseStatePublished, false)
}

func TestRotateKeyRecoveryAfterSQLiteCommitBeforeJournalCleanup(t *testing.T) {
	testInterruptedKeyRotationRecovery(t, rotationPhaseSQLiteCommitted, true)
}

func TestRotateKeyStartupRecoveryCoversEveryDurablePhase(t *testing.T) {
	for _, test := range []struct {
		name      string
		phase     keyRotationPhase
		committed bool
	}{
		{name: "prepared", phase: rotationPhasePrepared},
		{name: "key-published", phase: rotationPhaseKeyPublished},
		{name: "state-published", phase: rotationPhaseStatePublished},
		{name: "sqlite-committed", phase: rotationPhaseSQLiteCommitted, committed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			testInterruptedKeyRotationRecovery(t, test.phase, test.committed)
		})
	}
}

func TestRotateKeyReencryptsNormalizedCredentialsInCommittedRevision(t *testing.T) {
	fixture := newKeyRotationFixture(t)
	oldCipher := cipherFromBytes(t, fixture.oldKey)
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	repository := client.NewRepository(db)
	clientRow, err := repository.Create(client.Client{Name: "alice", Enabled: true, QuotaResetPolicy: client.ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repository.CreateBinding(client.Binding{ClientID: clientRow.ID, InboundID: "hy2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := client.NewCredentialStore(db, oldCipher).Set(binding.ID, "password", "normalized-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := RotateKey(RotateKeyOptions{
		StatePath: fixture.statePath, KeyPath: fixture.keyPath, DatabasePath: fixture.databasePath,
	}); err != nil {
		t.Fatal(err)
	}
	newKey, err := os.ReadFile(fixture.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	newCipher := cipherFromBytes(t, newKey)
	db, err = storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	plain, err := client.NewCredentialStore(db, newCipher).Reveal(credential.ID)
	if err != nil || plain != "normalized-secret" {
		t.Fatalf("reveal with new key: plain=%q err=%v", plain, err)
	}
	if _, err := client.NewCredentialStore(db, oldCipher).Reveal(credential.ID); err == nil {
		t.Fatal("normalized credential remained decryptable with previous key")
	}
	payload, err := apply.NewSnapshotStore(db).Load(1)
	if err != nil {
		t.Fatal(err)
	}
	var immutable model.ManagementSnapshot
	if err := json.Unmarshal(payload, &immutable); err != nil {
		t.Fatal(err)
	}
	if len(immutable.Credentials) != 1 {
		t.Fatalf("committed credential snapshot=%+v", immutable.Credentials)
	}
	snapshotPlain, err := newCipher.Decrypt(string(immutable.Credentials[0].EncryptedValue))
	if err != nil || snapshotPlain != "normalized-secret" {
		t.Fatalf("decrypt committed credential: plain=%q err=%v", snapshotPlain, err)
	}
}

func TestRotateKeyCredentialReencryptFailureRollsBackFilesAndRevision(t *testing.T) {
	fixture := newKeyRotationFixture(t)
	oldCipher := cipherFromBytes(t, fixture.oldKey)
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	repository := client.NewRepository(db)
	clientRow, err := repository.Create(client.Client{Name: "alice", Enabled: true, QuotaResetPolicy: client.ResetNever})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := repository.CreateBinding(client.Binding{ClientID: clientRow.ID, InboundID: "hy2", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := client.NewCredentialStore(db, oldCipher).Set(binding.ID, "password", "will-corrupt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE client_credentials SET encrypted_value=? WHERE id=?`, []byte("ve1:not-valid-ciphertext"), credential.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := RotateKey(RotateKeyOptions{
		StatePath: fixture.statePath, KeyPath: fixture.keyPath, DatabasePath: fixture.databasePath,
	}); err == nil {
		t.Fatal("rotation accepted undecryptable normalized credential")
	}
	keyAfter, err := os.ReadFile(fixture.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	stateAfter, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(keyAfter, fixture.oldKey) || !bytes.Equal(stateAfter, fixture.oldState) {
		t.Fatal("credential re-encryption failure did not restore key/state bytes")
	}
	if _, err := os.Stat(KeyRotationJournalPath(fixture.statePath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("credential failure left journal: %v", err)
	}
	db, err = storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	revisions, err := apply.NewRevisionStore(db).Get()
	if err != nil || revisions.Desired != 0 {
		t.Fatalf("desired revision=%d err=%v", revisions.Desired, err)
	}
}

func TestRotateKeyCredentialRowsFollowJournalRecoveryDecision(t *testing.T) {
	for _, test := range []struct {
		name  string
		phase keyRotationPhase
	}{
		{name: "rollback", phase: rotationPhaseStatePublished},
		{name: "finalize", phase: rotationPhaseSQLiteCommitted},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newKeyRotationFixture(t)
			oldCipher := cipherFromBytes(t, fixture.oldKey)
			db, err := storage.OpenExisting(fixture.databasePath)
			if err != nil {
				t.Fatal(err)
			}
			repository := client.NewRepository(db)
			clientRow, err := repository.Create(client.Client{Name: "alice", Enabled: true, QuotaResetPolicy: client.ResetNever})
			if err != nil {
				t.Fatal(err)
			}
			binding, err := repository.CreateBinding(client.Binding{ClientID: clientRow.ID, InboundID: "hy2", Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			credential, err := client.NewCredentialStore(db, oldCipher).Set(binding.ID, "password", "recovery-secret")
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			_, err = RotateKey(RotateKeyOptions{
				StatePath: fixture.statePath, KeyPath: fixture.keyPath, DatabasePath: fixture.databasePath,
				interruptAfter: test.phase,
			})
			if !errors.Is(err, errKeyRotationInterrupted) {
				t.Fatalf("RotateKey error=%v", err)
			}
			if err := RecoverKeyRotation(RecoverKeyRotationOptions{StatePath: fixture.statePath, DatabasePath: fixture.databasePath}); err != nil {
				t.Fatal(err)
			}
			liveKey, err := os.ReadFile(fixture.keyPath)
			if err != nil {
				t.Fatal(err)
			}
			db, err = storage.OpenExisting(fixture.databasePath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			plain, err := client.NewCredentialStore(db, cipherFromBytes(t, liveKey)).Reveal(credential.ID)
			if err != nil || plain != "recovery-secret" {
				t.Fatalf("reveal recovered credential: plain=%q err=%v", plain, err)
			}
		})
	}
}

func TestRotateKeyJournalRecordsBothFilesRevisionsOwnershipAndSafetyPaths(t *testing.T) {
	fixture := newKeyRotationFixture(t)
	_, err := RotateKey(RotateKeyOptions{
		StatePath:      fixture.statePath,
		KeyPath:        fixture.keyPath,
		DatabasePath:   fixture.databasePath,
		Now:            fixedRotationTime,
		interruptAfter: rotationPhasePrepared,
	})
	if !errors.Is(err, errKeyRotationInterrupted) {
		t.Fatalf("RotateKey error=%v want simulated interruption", err)
	}
	body, err := os.ReadFile(KeyRotationJournalPath(fixture.statePath))
	if err != nil {
		t.Fatal(err)
	}
	var journal map[string]any
	if err := json.Unmarshal(body, &journal); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"phase", "previousRevision", "intendedRevision",
		"previousKeySha256", "intendedKeySha256", "previousStateSha256", "intendedStateSha256",
		"previousKeyPath", "intendedKeyPath", "previousStatePath", "intendedStatePath",
		"previousKeyMode", "previousKeyUid", "previousKeyGid",
		"previousStateMode", "previousStateUid", "previousStateGid",
	} {
		if _, ok := journal[key]; !ok {
			t.Fatalf("rotation journal missing %q: %s", key, body)
		}
	}
	record, ok, err := loadKeyRotationJournal(fixture.statePath)
	if err != nil || !ok {
		t.Fatalf("load typed journal: ok=%v err=%v", ok, err)
	}
	previousKey, err := os.ReadFile(record.PreviousKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	previousState, err := os.ReadFile(record.PreviousStatePath)
	if err != nil {
		t.Fatal(err)
	}
	intendedKey, err := os.ReadFile(record.IntendedKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	intendedState, err := os.ReadFile(record.IntendedStatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(previousKey, fixture.oldKey) || !bytes.Equal(previousState, fixture.oldState) {
		t.Fatal("journal safety files do not contain exact previous key/state bytes")
	}
	if digestBytes(intendedKey) != record.IntendedKeySHA256 || digestBytes(intendedState) != record.IntendedStateSHA256 {
		t.Fatal("journal intended safety files do not match recorded digests")
	}
	keyInfo, err := os.Stat(fixture.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	stateInfo, err := os.Stat(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	keyMeta, stateMeta := fileMetadata(keyInfo), fileMetadata(stateInfo)
	if record.PreviousKeyMode != uint32(keyMeta.mode.Perm()) || record.PreviousKeyUID != keyMeta.uid || record.PreviousKeyGID != keyMeta.gid {
		t.Fatalf("previous key ownership not recorded: journal=%+v actual=%+v", record, keyMeta)
	}
	if record.PreviousStateMode != uint32(stateMeta.mode.Perm()) || record.PreviousStateUID != stateMeta.uid || record.PreviousStateGID != stateMeta.gid {
		t.Fatalf("previous state ownership not recorded: journal=%+v actual=%+v", record, stateMeta)
	}
}

func TestRotateKeyRecoveryRestoresSeparateSourceAndExistingTargetKeys(t *testing.T) {
	fixture := newKeyRotationFixture(t)
	targetPath := filepath.Join(filepath.Dir(fixture.keyPath), "next.key")
	previousTarget := bytes.Repeat([]byte{0x7c}, secrets.KeySize)
	if err := os.WriteFile(targetPath, previousTarget, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RotateKey(RotateKeyOptions{
		StatePath: fixture.statePath, KeyPath: fixture.keyPath, TargetKeyPath: targetPath,
		DatabasePath: fixture.databasePath, Now: fixedRotationTime,
		interruptAfter: rotationPhaseStatePublished,
	})
	if !errors.Is(err, errKeyRotationInterrupted) {
		t.Fatalf("RotateKey error=%v want simulated interruption", err)
	}
	if err := RecoverKeyRotation(RecoverKeyRotationOptions{
		StatePath: fixture.statePath, DatabasePath: fixture.databasePath,
	}); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(fixture.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	state, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source, fixture.oldKey) || !bytes.Equal(target, previousTarget) || !bytes.Equal(state, fixture.oldState) {
		t.Fatal("separate-path rollback did not restore source key, target key, and state exactly")
	}
}

func TestRotateKeyRejectsMarkerlessStateRevisionMismatch(t *testing.T) {
	fixture := newKeyRotationFixture(t)
	oldCipher := cipherFromBytes(t, fixture.oldKey)
	snapshot, ok, err := managementstate.NewStore(fixture.statePath, oldCipher).Load()
	if err != nil || !ok {
		t.Fatalf("load fixture: ok=%v err=%v", ok, err)
	}
	if _, err := Save(snapshot, Options{
		StatePath: fixture.statePath, DatabasePath: fixture.databasePath, Cipher: oldCipher,
	}); err != nil {
		t.Fatal(err)
	}
	// Replace the bound revision-1 bytes with another valid old-key state and
	// verify rotation refuses to turn that mismatch into revision 2.
	mismatched, err := managementstate.NewStore(fixture.statePath, oldCipher).Marshal(model.ManagementSnapshot{
		Settings: model.Settings{Mode: "dev", Domain: "mismatch.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.statePath, mismatched, 0o600); err != nil {
		t.Fatal(err)
	}
	keyBefore := append([]byte(nil), fixture.oldKey...)
	if _, err := RotateKey(RotateKeyOptions{
		StatePath: fixture.statePath, KeyPath: fixture.keyPath, DatabasePath: fixture.databasePath,
	}); err == nil {
		t.Fatal("rotation accepted markerless state/revision mismatch")
	}
	keyAfter, err := os.ReadFile(fixture.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	stateAfter, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(keyAfter, keyBefore) || !bytes.Equal(stateAfter, mismatched) {
		t.Fatal("rejected rotation changed live key/state bytes")
	}
	if _, err := os.Stat(KeyRotationJournalPath(fixture.statePath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected rotation created journal: %v", err)
	}
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	revisions, err := apply.NewRevisionStore(db).Get()
	if err != nil || revisions.Desired != 1 {
		t.Fatalf("desired revision=%d err=%v", revisions.Desired, err)
	}
}

func TestRotateKeyRecoveryFailsClosedOnUnknownDigestCombination(t *testing.T) {
	fixture := newKeyRotationFixture(t)
	_, err := RotateKey(RotateKeyOptions{
		StatePath:      fixture.statePath,
		KeyPath:        fixture.keyPath,
		DatabasePath:   fixture.databasePath,
		Now:            fixedRotationTime,
		interruptAfter: rotationPhaseStatePublished,
	})
	if !errors.Is(err, errKeyRotationInterrupted) {
		t.Fatalf("RotateKey error=%v want simulated interruption", err)
	}
	if err := os.WriteFile(fixture.statePath, []byte("unknown-state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RecoverKeyRotation(RecoverKeyRotationOptions{
		StatePath: fixture.statePath, DatabasePath: fixture.databasePath,
	}); err == nil {
		t.Fatal("recovery accepted a state digest that matched neither side of the journal")
	}
	if _, err := os.Stat(KeyRotationJournalPath(fixture.statePath)); err != nil {
		t.Fatalf("fail-closed recovery removed decision record: %v", err)
	}
}

func testInterruptedKeyRotationRecovery(t *testing.T, phase keyRotationPhase, committed bool) {
	t.Helper()
	fixture := newKeyRotationFixture(t)
	_, err := RotateKey(RotateKeyOptions{
		StatePath:      fixture.statePath,
		KeyPath:        fixture.keyPath,
		DatabasePath:   fixture.databasePath,
		Now:            fixedRotationTime,
		interruptAfter: phase,
	})
	if !errors.Is(err, errKeyRotationInterrupted) {
		t.Fatalf("RotateKey phase=%s error=%v want simulated interruption", phase, err)
	}
	if _, err := os.Stat(KeyRotationJournalPath(fixture.statePath)); err != nil {
		t.Fatalf("phase %s did not leave durable journal: %v", phase, err)
	}
	if err := RecoverKeyRotation(RecoverKeyRotationOptions{
		StatePath: fixture.statePath, DatabasePath: fixture.databasePath,
	}); err != nil {
		t.Fatalf("recover phase %s: %v", phase, err)
	}
	stateBody, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	keyBody, err := os.ReadFile(fixture.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if committed {
		if bytes.Equal(keyBody, fixture.oldKey) {
			t.Fatal("committed recovery restored the previous key")
		}
		cipher := cipherFromBytes(t, keyBody)
		snapshot, ok, err := managementstate.NewStore(fixture.statePath, cipher).Load()
		if err != nil || !ok {
			t.Fatalf("load finalized rotated state: ok=%v err=%v", ok, err)
		}
		if snapshot.Settings.Domain != "before.example.com" {
			t.Fatalf("finalized state domain=%q", snapshot.Settings.Domain)
		}
	} else {
		if !bytes.Equal(keyBody, fixture.oldKey) || !bytes.Equal(stateBody, fixture.oldState) {
			t.Fatal("rollback did not restore previous key and state byte-for-byte")
		}
	}
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	revisions, err := apply.NewRevisionStore(db).Get()
	if err != nil {
		t.Fatal(err)
	}
	wantRevision := uint64(0)
	if committed {
		wantRevision = 1
	}
	if revisions.Desired != wantRevision {
		t.Fatalf("desired revision=%d want=%d", revisions.Desired, wantRevision)
	}
	if committed {
		digest, err := apply.NewSnapshotStore(db).StateDigest(1)
		if err != nil {
			t.Fatal(err)
		}
		if digest != managementstate.EncodedStateSHA256(stateBody) {
			t.Fatalf("snapshot digest=%q state=%q", digest, managementstate.EncodedStateSHA256(stateBody))
		}
	}
	if _, err := os.Stat(KeyRotationJournalPath(fixture.statePath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery left journal: %v", err)
	}
}

type keyRotationFixture struct {
	statePath    string
	keyPath      string
	databasePath string
	oldKey       []byte
	oldState     []byte
}

func newKeyRotationFixture(t *testing.T) keyRotationFixture {
	t.Helper()
	root := t.TempDir()
	fixture := keyRotationFixture{
		statePath: filepath.Join(root, "state.json"), keyPath: filepath.Join(root, "state.key"),
		databasePath: filepath.Join(root, "veil.db"),
	}
	fixture.oldKey = make([]byte, secrets.KeySize)
	if _, err := rand.Read(fixture.oldKey); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.keyPath, fixture.oldKey, 0o640); err != nil {
		t.Fatal(err)
	}
	oldCipher := cipherFromBytes(t, fixture.oldKey)
	var err error
	fixture.oldState, err = managementstate.NewStore(fixture.statePath, oldCipher).Marshal(model.ManagementSnapshot{
		Settings: model.Settings{Mode: "dev", Domain: "before.example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.statePath, fixture.oldState, 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func cipherFromBytes(t *testing.T, body []byte) *secrets.Cipher {
	t.Helper()
	if len(body) != secrets.KeySize {
		t.Fatalf("key length=%d", len(body))
	}
	var key [secrets.KeySize]byte
	copy(key[:], body)
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

func fixedRotationTime() time.Time {
	return time.Date(2026, 7, 26, 12, 0, 0, 123, time.UTC)
}
