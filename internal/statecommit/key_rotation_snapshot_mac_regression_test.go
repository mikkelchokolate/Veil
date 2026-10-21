package statecommit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

// #1280: key rotation re-seals every retained snapshot under the new key. It
// must not launder a row a veil.db writer edited into a valid new seal, and
// it must not promote an unsealed (pre-#1280 or planted) row to sealed.

func seedRotationSnapshot(t *testing.T, fixture keyRotationFixture, payload string) uint64 {
	t.Helper()
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	digest := sha256.Sum256(fixture.oldState)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	revision, err := apply.BumpDesiredTx(tx)
	if err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := apply.SaveSnapshotTxBound(tx, snapshotSealFor(t, cipherFromBytes(t, fixture.oldKey)), revision, []byte(payload), hex.EncodeToString(digest[:])); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return revision
}

func execOnFixtureDB(t *testing.T, fixture keyRotationFixture, query string, args ...any) {
	t.Helper()
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestRotateKeyRefusesToResealEditedSnapshot(t *testing.T) {
	fixture := newKeyRotationFixture(t)
	revision := seedRotationSnapshot(t, fixture, `{"settings":{"mode":"dev","domain":"before.example.com"}}`)
	execOnFixtureDB(t, fixture, `UPDATE revision_snapshots SET payload=? WHERE revision=?`,
		`{"settings":{"mode":"dev","domain":"attacker.example.net"}}`, revision)

	_, err := RotateKey(RotateKeyOptions{
		StatePath: fixture.statePath, KeyPath: fixture.keyPath, DatabasePath: fixture.databasePath,
	})
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("RotateKey laundered an edited snapshot: err=%v", err)
	}
	key, err := os.ReadFile(fixture.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(key, fixture.oldKey) {
		t.Fatal("refused rotation still replaced the master key")
	}
}

func TestRotateKeyKeepsUnsealedSnapshotUnsealed(t *testing.T) {
	fixture := newKeyRotationFixture(t)
	revision := seedRotationSnapshot(t, fixture, `{"settings":{"mode":"dev","domain":"before.example.com"}}`)
	execOnFixtureDB(t, fixture, `UPDATE revision_snapshots SET payload_mac='' WHERE revision=?`, revision)

	if _, err := RotateKey(RotateKeyOptions{
		StatePath: fixture.statePath, KeyPath: fixture.keyPath, DatabasePath: fixture.databasePath,
	}); err != nil {
		t.Fatal(err)
	}
	newKey, err := os.ReadFile(fixture.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	db, err := storage.OpenExisting(fixture.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := apply.NewSnapshotStore(db)
	if err := store.SetCipher(cipherFromBytes(t, newKey)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(revision); err == nil {
		t.Fatal("rotation promoted an unsealed snapshot to a valid seal")
	}
	revisions, err := apply.NewRevisionStore(db).Get()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(revisions.Desired); err != nil {
		t.Fatalf("rotation revision is not sealed under the new key: %v", err)
	}
}
