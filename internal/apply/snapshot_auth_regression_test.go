package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/secrets"
)

// #1280: revision_snapshots rows are authenticated under a key derived from
// state.key, so a veil.db writer cannot author what the /s/ feed, the apply
// render path or rollback reads.

func snapshotTestCipher(t *testing.T, fill byte) *secrets.Cipher {
	t.Helper()
	var key [secrets.KeySize]byte
	for i := range key {
		key[i] = fill
	}
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

func snapshotTestDigest(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func armedSnapshotStore(t *testing.T) *SnapshotStore {
	t.Helper()
	store := NewSnapshotStore(openTestDB(t))
	if err := store.SetCipher(snapshotTestCipher(t, 7)); err != nil {
		t.Fatal(err)
	}
	return store
}

func requireUnauthenticated(t *testing.T, err error, what string) {
	t.Helper()
	if !errors.Is(err, ErrSnapshotUnauthenticated) {
		t.Fatalf("%s: err=%v, want ErrSnapshotUnauthenticated", what, err)
	}
}

func TestSnapshotLoadRejectsPayloadRewrittenByDatabaseWriter(t *testing.T) {
	store := armedSnapshotStore(t)
	honest := `{"inbounds":[{"name":"hy2","domain":"vpn.example.com"}]}`
	if err := store.SaveBound(5, []byte(honest), snapshotTestDigest("state-5")); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Load(5); err != nil || string(got) != honest {
		t.Fatalf("honest sealed row: payload=%q err=%v", got, err)
	}
	if _, err := store.db.Exec(`UPDATE revision_snapshots SET payload=? WHERE revision=5`,
		`{"inbounds":[{"name":"hy2","domain":"attacker.example.net"}]}`); err != nil {
		t.Fatal(err)
	}
	_, err := store.Load(5)
	requireUnauthenticated(t, err, "rewritten payload")
}

func TestSnapshotLoadRejectsPlantedUnsealedRow(t *testing.T) {
	store := armedSnapshotStore(t)
	if _, err := store.db.Exec(`INSERT INTO revision_snapshots(revision,payload,state_sha256) VALUES(9,'{"settings":{}}',?)`,
		snapshotTestDigest("planted")); err != nil {
		t.Fatal(err)
	}
	_, err := store.Load(9)
	requireUnauthenticated(t, err, "planted row")
	_, err = store.EffectiveAt(9)
	requireUnauthenticated(t, err, "planted row effective time")
}

func TestSnapshotMACBindsRevisionDigestAndCreationTime(t *testing.T) {
	store := armedSnapshotStore(t)
	payload := `{"v":1}`
	if err := store.SaveBound(1, []byte(payload), snapshotTestDigest("one")); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveBound(2, []byte(`{"v":2}`), snapshotTestDigest("two")); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		sql  string
		rev  uint64
	}{
		{"copied row moved to another revision", `UPDATE revision_snapshots SET payload=(SELECT payload FROM revision_snapshots WHERE revision=1), state_sha256=(SELECT state_sha256 FROM revision_snapshots WHERE revision=1), created_at=(SELECT created_at FROM revision_snapshots WHERE revision=1), payload_mac=(SELECT payload_mac FROM revision_snapshots WHERE revision=1) WHERE revision=2`, 2},
		{"re-pointed state digest", `UPDATE revision_snapshots SET state_sha256='` + snapshotTestDigest("forged") + `' WHERE revision=1`, 1},
		{"shifted creation time", `UPDATE revision_snapshots SET created_at=created_at-86400 WHERE revision=1`, 1},
		{"blanked mac", `UPDATE revision_snapshots SET payload_mac='' WHERE revision=1`, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := store.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback() //nolint:errcheck
			if _, err := tx.Exec(tc.sql); err != nil {
				t.Fatal(err)
			}
			var p, d, m string
			var c int64
			if err := tx.QueryRow(`SELECT payload,state_sha256,created_at,payload_mac FROM revision_snapshots WHERE revision=?`, tc.rev).Scan(&p, &d, &c, &m); err != nil {
				t.Fatal(err)
			}
			if store.Seal().Verify(tc.rev, c, d, []byte(p), m) {
				t.Fatal("tampered row still verifies")
			}
		})
	}
}

func TestSnapshotMACUnderAnotherKeyIsRefused(t *testing.T) {
	db := openTestDB(t)
	foreign := NewSnapshotStore(db)
	if err := foreign.SetCipher(snapshotTestCipher(t, 9)); err != nil {
		t.Fatal(err)
	}
	if err := foreign.SaveBound(3, []byte(`{"v":3}`), snapshotTestDigest("three")); err != nil {
		t.Fatal(err)
	}
	store := NewSnapshotStore(db)
	if err := store.SetCipher(snapshotTestCipher(t, 7)); err != nil {
		t.Fatal(err)
	}
	_, err := store.Load(3)
	requireUnauthenticated(t, err, "foreign-key seal")
}

func TestSnapshotBoundSaveRefusesPlantedRowAtSameRevision(t *testing.T) {
	store := armedSnapshotStore(t)
	payload := `{"v":4}`
	digest := snapshotTestDigest("four")
	// A writer pre-plants the exact payload/digest a future commit will
	// write, but cannot produce its MAC; the commit must not adopt it.
	if _, err := store.db.Exec(`INSERT INTO revision_snapshots(revision,payload,state_sha256) VALUES(4,?,?)`, payload, digest); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveBound(4, []byte(payload), digest); err == nil {
		t.Fatal("bound save adopted an unsealed planted row")
	}
	// An honest retry of an already sealed revision stays idempotent.
	if err := store.SaveBound(6, []byte(payload), digest); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveBound(6, []byte(payload), digest); err != nil {
		t.Fatalf("idempotent sealed retry: %v", err)
	}
}

func TestSnapshotBoundSaveRequiresSeal(t *testing.T) {
	store := NewSnapshotStore(openTestDB(t))
	if err := store.SaveBound(1, []byte(`{}`), snapshotTestDigest("x")); err == nil {
		t.Fatal("unarmed store wrote a bound snapshot without a MAC")
	}
}

func TestSnapshotSealLegacyOnlySealsTheVerifiedUnsealedRow(t *testing.T) {
	store := armedSnapshotStore(t)
	digest := snapshotTestDigest("legacy")
	if _, err := store.db.Exec(`INSERT INTO revision_snapshots(revision,payload,state_sha256) VALUES(8,'{"v":8}',?)`, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(8); err == nil {
		t.Fatal("unsealed legacy row was readable before sealing")
	}
	if err := store.SealLegacy(8, []byte(`{"v":"other"}`), digest); err == nil {
		t.Fatal("SealLegacy sealed a payload other than the one verified")
	}
	if err := store.SealLegacy(8, []byte(`{"v":8}`), snapshotTestDigest("other")); err == nil {
		t.Fatal("SealLegacy sealed under a different state digest")
	}
	if err := store.SealLegacy(8, []byte(`{"v":8}`), digest); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Load(8); err != nil || string(got) != `{"v":8}` {
		t.Fatalf("sealed legacy row: %q %v", got, err)
	}
	if err := store.SealLegacy(8, []byte(`{"v":8}`), digest); err == nil {
		t.Fatal("SealLegacy re-sealed an already sealed row")
	}
	if _, err := store.LoadUnsealed(8); err == nil {
		t.Fatal("LoadUnsealed returned a sealed row without authentication")
	}
}

func TestSnapshotBindStateDigestNeverTouchesSealedRow(t *testing.T) {
	store := armedSnapshotStore(t)
	if err := store.Save(11, []byte(`{"v":11}`)); err != nil {
		t.Fatal(err)
	}
	if err := store.BindStateDigest(11, snapshotTestDigest("late")); err == nil {
		t.Fatal("BindStateDigest re-pointed a sealed row")
	}
	if _, err := store.Load(11); err != nil {
		t.Fatalf("sealed row must stay readable: %v", err)
	}
}
