package api

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

// TestPinStateToRevisionSwapsAndRestores verifies (A3) that the executor pins
// the renderer to the immutable snapshot recorded for a revision and restores
// live state afterwards — so a job for an older revision never renders newer
// committed state.
func TestPinStateToRevisionSwapsAndRestores(t *testing.T) {
	s := &managementState{}

	// Live (current) state is the NEWER revision.
	s.settings = Settings{Domain: "newer.example"}

	// Record an immutable snapshot for revision 41 holding the OLDER config.
	db := openApplyTestDB(t)
	s.applySnapshots = apply.NewSnapshotStore(db)
	older := managementSnapshot{Settings: Settings{Domain: "older.example"}}
	payload, err := json.Marshal(older)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := s.applySnapshots.Save(41, payload); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	// Pin to revision 41: live state must now render the OLDER config.
	restore, err := s.pinStateToRevisionLocked(41)
	if err != nil {
		t.Fatalf("pin: %v", err)
	}
	if restore == nil {
		t.Fatal("expected a pin (snapshot exists), got nil")
	}
	if s.settings.Domain != "older.example" {
		t.Fatalf("pinned state not applied: domain=%q", s.settings.Domain)
	}

	// Restore: live state returns to the newer config.
	restore()
	if s.settings.Domain != "newer.example" {
		t.Fatalf("state not restored after pinned render: domain=%q", s.settings.Domain)
	}
}

// TestPinStateToRevisionMissingSnapshotFails ensures a revision with no
// recorded snapshot FAILS the apply (A3: forbidden fallback removed). The
// executor must never silently render current mutable state for a tracked
// revision — that would violate immutability.
func TestPinStateToRevisionMissingSnapshotFails(t *testing.T) {
	s := &managementState{}
	s.settings = Settings{Domain: "current.example"}
	db := openApplyTestDB(t)
	s.applySnapshots = apply.NewSnapshotStore(db)

	restore, err := s.pinStateToRevisionLocked(999)
	if err == nil {
		t.Fatal("expected error for missing snapshot, got nil")
	}
	if restore != nil {
		t.Fatal("expected nil restore for missing snapshot")
	}
	if s.settings.Domain != "current.example" {
		t.Fatalf("state mutated for missing snapshot: %q", s.settings.Domain)
	}
}

// TestSnapshotDoesNotContainPlaintextCredentials guards the A3 requirement
// that the immutable snapshot must not embed plaintext credentials. bumpDesired
// encrypts the snapshot via the secrets cipher before persisting it, so the
// stored payload must contain ciphertext, never the raw password.
func TestSnapshotDoesNotContainPlaintextCredentials(t *testing.T) {
	s := &managementState{}
	s.cipher = newTestCipher(t)
	s.settings = Settings{Domain: "x.example", PanelListen: "127.0.0.1:2096"}
	s.inbounds = []Inbound{{Name: "hy2", Protocol: "hysteria2", Password: "SECRET-PASSWORD", Port: 443}}

	db := openApplyTestDB(t)
	s.applySnapshots = apply.NewSnapshotStore(db)
	s.applyRevisions = apply.NewRevisionStore(db)
	s.applyJobs = apply.NewJobStore(db)
	s.applyRunner = apply.NewRunner(s.applyRevisions, s.applyJobs, s.executeApplyRevision)

	rev := s.bumpDesiredRevisionLocked()
	if rev == 0 {
		t.Fatal("expected a desired revision")
	}
	raw, err := s.applySnapshots.Load(rev)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if strings.Contains(string(raw), "SECRET-PASSWORD") {
		t.Fatalf("snapshot stored plaintext credential: %s", raw)
	}
	// And the pinned render must decrypt it back for the renderer.
	restore, err := s.pinStateToRevisionLocked(rev)
	if err != nil {
		t.Fatalf("pin: %v", err)
	}
	if restore == nil {
		t.Fatal("expected pin")
	}
	if s.inbounds[0].Password != "SECRET-PASSWORD" {
		t.Fatalf("pinned render did not decrypt credential: %q", s.inbounds[0].Password)
	}
	restore()
}

func openApplyTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(filepath.Join(t.TempDir(), "veil.db"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
