package apply

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrApplyLeaseLost = errors.New("apply: durable lease lost")

type LeaseStore struct{ db *sql.DB }

func NewLeaseStore(db *sql.DB) *LeaseStore { return &LeaseStore{db: db} }

func (s *LeaseStore) Acquire(owner, operation string, now time.Time, ttl time.Duration) (bool, error) {
	if s == nil || s.db == nil {
		return false, errors.New("apply lease store is not configured")
	}
	nowUnix := now.UTC().Unix()
	expires := now.Add(ttl).UTC().Unix()
	result, err := s.db.Exec(`UPDATE apply_lease
SET owner_process=?, lease_expires_at=?, heartbeat_at=?, current_operation=?
WHERE id=1 AND (owner_process='' OR lease_expires_at<=? OR owner_process=?)`,
		owner, expires, nowUnix, operation, nowUnix, owner)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (s *LeaseStore) Heartbeat(owner string, now time.Time, ttl time.Duration) error {
	nowUnix := now.UTC().Unix()
	result, err := s.db.Exec(`UPDATE apply_lease
SET lease_expires_at=?, heartbeat_at=?
WHERE id=1 AND owner_process=? AND lease_expires_at>?`, now.Add(ttl).UTC().Unix(), nowUnix, owner, nowUnix)
	if err != nil {
		return err
	}
	return requireOneLeaseRow(result, "heartbeat")
}

func (s *LeaseStore) Release(owner string) error {
	result, err := s.db.Exec(`UPDATE apply_lease
SET owner_process='', lease_expires_at=0, heartbeat_at=0, current_operation=''
WHERE id=1 AND owner_process=?`, owner)
	if err != nil {
		return err
	}
	return requireOneLeaseRow(result, "release")
}

func (s *LeaseStore) Valid(now time.Time) (bool, error) {
	var owner string
	var expires int64
	if err := s.db.QueryRow(`SELECT owner_process, lease_expires_at FROM apply_lease WHERE id=1`).Scan(&owner, &expires); err != nil {
		return false, err
	}
	return owner != "" && expires > now.UTC().Unix(), nil
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
