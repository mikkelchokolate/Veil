package apply

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
)

// SnapshotStore persists an immutable, content-addressed-by-revision snapshot
// of the desired configuration for each desired revision. Once a revision is
// recorded its snapshot never changes, so an apply job for revision N always
// renders exactly the configuration that was committed as revision N — never
// a newer mutable state.
type SnapshotStore struct{ db *sql.DB }

func NewSnapshotStore(db *sql.DB) *SnapshotStore { return &SnapshotStore{db: db} }

// Save records the snapshot payload for a revision. It is retained for
// unpersisted/legacy callers that have no state-file digest.
func (s *SnapshotStore) Save(revision uint64, payload []byte) error {
	return SaveSnapshotTx(s.db, revision, payload)
}

// SaveBound records a snapshot together with the SHA-256 digest of the exact
// state.json bytes that belong to the same desired revision.
func (s *SnapshotStore) SaveBound(revision uint64, payload []byte, stateSHA256 string) error {
	return SaveSnapshotTxBound(s.db, revision, payload, stateSHA256)
}

// SaveSnapshotTx is Save inside a caller-managed transaction.
func SaveSnapshotTx(q DBTX, revision uint64, payload []byte) error {
	return saveSnapshotTx(q, revision, payload, "", false)
}

// SaveSnapshotTxBound commits an immutable snapshot and its state-file digest
// inside the caller's revision transaction.
func SaveSnapshotTxBound(q DBTX, revision uint64, payload []byte, stateSHA256 string) error {
	return saveSnapshotTx(q, revision, payload, stateSHA256, true)
}

func saveSnapshotTx(q DBTX, revision uint64, payload []byte, stateSHA256 string, requireDigest bool) error {
	if requireDigest && !validStateSHA256(stateSHA256) {
		return fmt.Errorf("apply: state digest for revision %d is invalid", revision)
	}
	if _, err := q.Exec(
		`INSERT OR IGNORE INTO revision_snapshots(revision, payload, state_sha256) VALUES(?, ?, ?)`,
		revision, string(payload), stateSHA256,
	); err != nil {
		return fmt.Errorf("apply: save snapshot rev %d: %w", revision, err)
	}
	if !requireDigest {
		return nil
	}
	// INSERT OR IGNORE preserves first-write immutability, but a bound writer
	// must not hide a conflicting stale row at the same revision.
	var storedPayload, storedDigest string
	if err := q.QueryRow(
		`SELECT payload, state_sha256 FROM revision_snapshots WHERE revision = ?`, revision,
	).Scan(&storedPayload, &storedDigest); err != nil {
		return fmt.Errorf("apply: verify snapshot rev %d: %w", revision, err)
	}
	if storedPayload != string(payload) || storedDigest != stateSHA256 {
		return fmt.Errorf("apply: immutable snapshot collision for revision %d", revision)
	}
	return nil
}

// Load returns the snapshot payload for a revision.
func (s *SnapshotStore) Load(revision uint64) ([]byte, error) {
	var payload string
	err := s.db.QueryRow(
		`SELECT payload FROM revision_snapshots WHERE revision = ?`, revision,
	).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("apply: no snapshot for revision %d", revision)
	}
	if err != nil {
		return nil, fmt.Errorf("apply: load snapshot rev %d: %w", revision, err)
	}
	return []byte(payload), nil
}

// Has reports whether a snapshot exists for a revision.
func (s *SnapshotStore) Has(revision uint64) bool {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(1) FROM revision_snapshots WHERE revision = ?`, revision).Scan(&n)
	return n > 0
}

// StateDigest returns the exact state.json digest associated with revision.
func (s *SnapshotStore) StateDigest(revision uint64) (string, error) {
	var digest string
	if err := s.db.QueryRow(
		`SELECT state_sha256 FROM revision_snapshots WHERE revision = ?`, revision,
	).Scan(&digest); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("apply: no snapshot for revision %d", revision)
		}
		return "", fmt.Errorf("apply: load state digest rev %d: %w", revision, err)
	}
	return digest, nil
}

// BindStateDigest fills the one-time association for a proven-consistent
// legacy snapshot. It never replaces an existing non-empty digest.
func (s *SnapshotStore) BindStateDigest(revision uint64, stateSHA256 string) error {
	if !validStateSHA256(stateSHA256) {
		return fmt.Errorf("apply: state digest for revision %d is invalid", revision)
	}
	if _, err := s.db.Exec(
		`UPDATE revision_snapshots SET state_sha256 = ? WHERE revision = ? AND state_sha256 = ''`,
		stateSHA256, revision,
	); err != nil {
		return fmt.Errorf("apply: bind state digest rev %d: %w", revision, err)
	}
	stored, err := s.StateDigest(revision)
	if err != nil {
		return err
	}
	if stored != stateSHA256 {
		return fmt.Errorf("apply: state digest mismatch for revision %d", revision)
	}
	return nil
}

func validStateSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
