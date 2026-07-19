package apply

import (
	"database/sql"
	"fmt"
)

// SnapshotStore persists an immutable, content-addressed-by-revision snapshot
// of the desired configuration for each desired revision. Once a revision is
// recorded its snapshot never changes, so an apply job for revision N always
// renders exactly the configuration that was committed as revision N — never
// a newer mutable state.
type SnapshotStore struct{ db *sql.DB }

func NewSnapshotStore(db *sql.DB) *SnapshotStore { return &SnapshotStore{db: db} }

// Save records the snapshot payload for a revision. It is idempotent and
// never overwrites an existing snapshot: re-saving the same revision is a
// no-op (the first write wins), preserving immutability.
func (s *SnapshotStore) Save(revision uint64, payload []byte) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO revision_snapshots(revision, payload) VALUES(?, ?)`,
		revision, string(payload),
	)
	if err != nil {
		return fmt.Errorf("apply: save snapshot rev %d: %w", revision, err)
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
