package apply

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrApplyLeaseLost = errors.New("apply: durable lease lost")

type Lease struct {
	Owner       string
	Operation   string
	Generation  uint64
	ExpiresAt   int64
	HeartbeatAt int64
}

type LeaseStore struct{ db *sql.DB }

func NewLeaseStore(db *sql.DB) *LeaseStore { return &LeaseStore{db: db} }

// Acquire atomically claims the singleton lease and increments its fencing
// generation. Generations are retained when the lease is released, so every
// successful acquisition is strictly newer than every previous owner.
func (s *LeaseStore) Acquire(owner, operation string, now time.Time, ttl time.Duration) (Lease, bool, error) {
	if s == nil || s.db == nil {
		return Lease{}, false, errors.New("apply lease store is not configured")
	}
	if owner == "" || ttl <= 0 {
		return Lease{}, false, errors.New("apply lease owner and positive ttl are required")
	}
	nowUnix := now.UTC().Unix()
	expires := now.Add(ttl).UTC().Unix()
	var lease Lease
	err := s.db.QueryRow(`UPDATE apply_lease
SET owner_process=?, lease_expires_at=?, heartbeat_at=?, current_operation=?, generation=generation+1
WHERE id=1 AND (owner_process='' OR lease_expires_at<=?)
RETURNING owner_process, current_operation, generation, lease_expires_at, heartbeat_at`,
		owner, expires, nowUnix, operation, nowUnix,
	).Scan(&lease.Owner, &lease.Operation, &lease.Generation, &lease.ExpiresAt, &lease.HeartbeatAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Lease{}, false, nil
	}
	if err != nil {
		return Lease{}, false, err
	}
	return lease, true, nil
}

func (s *LeaseStore) Heartbeat(owner string, generation uint64, now time.Time, ttl time.Duration) error {
	if s == nil || s.db == nil {
		return errors.New("apply lease store is not configured")
	}
	nowUnix := now.UTC().Unix()
	result, err := s.db.Exec(`UPDATE apply_lease
SET lease_expires_at=?, heartbeat_at=?
WHERE id=1 AND owner_process=? AND generation=? AND lease_expires_at>?`,
		now.Add(ttl).UTC().Unix(), nowUnix, owner, generation, nowUnix)
	if err != nil {
		return err
	}
	return requireOneLeaseRow(result, "heartbeat")
}

func (s *LeaseStore) Release(owner string, generation uint64) error {
	if s == nil || s.db == nil {
		return errors.New("apply lease store is not configured")
	}
	result, err := s.db.Exec(`UPDATE apply_lease
SET owner_process='', lease_expires_at=0, heartbeat_at=0, current_operation=''
WHERE id=1 AND owner_process=? AND generation=?`, owner, generation)
	if err != nil {
		return err
	}
	return requireOneLeaseRow(result, "release")
}

func (s *LeaseStore) Expire(owner string, generation uint64) error {
	if s == nil || s.db == nil {
		return errors.New("apply lease store is not configured")
	}
	result, err := s.db.Exec(`UPDATE apply_lease
SET owner_process='', lease_expires_at=0, heartbeat_at=0, current_operation=''
WHERE id=1 AND owner_process=? AND generation=?`, owner, generation)
	if err != nil {
		return err
	}
	return requireOneLeaseRow(result, "expire")
}

func (s *LeaseStore) Current() (Lease, error) {
	if s == nil || s.db == nil {
		return Lease{}, errors.New("apply lease store is not configured")
	}
	var lease Lease
	err := s.db.QueryRow(`SELECT owner_process, current_operation, generation, lease_expires_at, heartbeat_at
FROM apply_lease WHERE id=1`).Scan(&lease.Owner, &lease.Operation, &lease.Generation, &lease.ExpiresAt, &lease.HeartbeatAt)
	return lease, err
}

func (s *LeaseStore) Valid(now time.Time) (bool, error) {
	lease, err := s.Current()
	if err != nil {
		return false, err
	}
	return lease.Owner != "" && lease.ExpiresAt > now.UTC().Unix(), nil
}

func assertLeaseCurrentTx(tx *sql.Tx, owner string, generation uint64, now time.Time) error {
	var currentOwner string
	var currentGeneration uint64
	var expires int64
	if err := tx.QueryRow(`SELECT owner_process, generation, lease_expires_at FROM apply_lease WHERE id=1`).Scan(
		&currentOwner, &currentGeneration, &expires,
	); err != nil {
		return err
	}
	if currentOwner != owner || currentGeneration != generation || expires <= now.UTC().Unix() {
		return ErrApplyLeaseLost
	}
	return nil
}

func requireOneLeaseRow(result sql.Result, operation string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("%w during %s", ErrApplyLeaseLost, operation)
	}
	return nil
}
