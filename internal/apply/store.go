package apply

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// RevisionStore persists the desired/applied revision pair in the single-row
// revisions table. Desired is bumped by every committed configuration
// mutation; applied advances only after a job succeeds (render + apply +
// health check).
type RevisionStore struct{ db *sql.DB }

func NewRevisionStore(db *sql.DB) *RevisionStore { return &RevisionStore{db: db} }

// DBTX is the database/sql surface shared by *sql.DB and *sql.Tx so revision
// and snapshot writes can join a caller-managed transaction.
type DBTX interface {
	Exec(query string, args ...any) (sql.Result, error)
	QueryRow(query string, args ...any) *sql.Row
}

func ensureRowQ(q DBTX) error {
	_, err := q.Exec(`INSERT OR IGNORE INTO revisions(id, desired_revision, applied_revision) VALUES(1,0,0)`)
	return err
}

func (s *RevisionStore) ensureRow() error {
	return ensureRowQ(s.db)
}

// Get returns the current revision pair.
func (s *RevisionStore) Get() (Revisions, error) {
	return getRevisionsQ(s.db)
}

func getRevisionsQ(q DBTX) (Revisions, error) {
	if err := ensureRowQ(q); err != nil {
		return Revisions{}, fmt.Errorf("apply: init revisions: %w", err)
	}
	var r Revisions
	err := q.QueryRow(`SELECT desired_revision, applied_revision FROM revisions WHERE id=1`).
		Scan(&r.Desired, &r.Applied)
	if err != nil {
		return Revisions{}, fmt.Errorf("apply: read revisions: %w", err)
	}
	return r, nil
}

// BumpDesired increments desired_revision and returns the new value. Applied is
// untouched so the system reports pending/applying until a job succeeds.
func (s *RevisionStore) BumpDesired() (uint64, error) {
	return BumpDesiredTx(s.db)
}

// BumpDesiredTx is BumpDesired inside a caller-managed transaction so the
// revision bump commits atomically with the mutation that caused it.
func BumpDesiredTx(q DBTX) (uint64, error) {
	if err := ensureRowQ(q); err != nil {
		return 0, err
	}
	res, err := q.Exec(`UPDATE revisions SET desired_revision = desired_revision + 1 WHERE id=1`)
	if err != nil {
		return 0, fmt.Errorf("apply: bump desired: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, fmt.Errorf("apply: bump desired affected no rows")
	}
	r, err := getRevisionsQ(q)
	if err != nil {
		return 0, err
	}
	return r.Desired, nil
}

// MarkApplied sets applied_revision to rev, which must not exceed desired.
func (s *RevisionStore) MarkApplied(rev uint64) error {
	if err := s.ensureRow(); err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE revisions SET applied_revision=?
	  WHERE id=1 AND ? <= desired_revision AND ? >= applied_revision`, rev, rev, rev)
	if err != nil {
		return fmt.Errorf("apply: mark applied: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("apply: cannot move applied revision to %d outside [applied, desired]", rev)
	}
	return nil
}

// JobStore persists apply jobs durably so history survives a panel restart.
type JobStore struct{ db *sql.DB }

func NewJobStore(db *sql.DB) *JobStore { return &JobStore{db: db} }

func (s *JobStore) Create(j Job) error {
	ops, err := json.Marshal(j.Operations)
	if err != nil {
		return err
	}
	if j.Operations == nil {
		ops = []byte("[]")
	}
	result, err := s.db.Exec(`INSERT INTO apply_jobs
  (id, desired_revision, base_revision, status, trigger, actor_id, created_at, operations)
  VALUES(?,?,?,?,?,?,?,?)`,
		j.ID, j.DesiredRevision, j.BaseRevision, j.Status, j.Trigger, j.ActorID, j.CreatedAt, string(ops))
	if err != nil {
		return fmt.Errorf("apply: create job: %w", err)
	}
	return requireOneJobRow(result, "create", j.ID)
}

func (s *JobStore) Get(id string) (Job, error) {
	row := s.db.QueryRow(`SELECT id, desired_revision, base_revision, status, trigger, actor_id,
	  created_at, started_at, finished_at, error_code, error_message, operations
	  FROM apply_jobs WHERE id=?`, id)
	return scanJob(row)
}

func (s *JobStore) List(limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT id, desired_revision, base_revision, status, trigger, actor_id,
	  created_at, started_at, finished_at, error_code, error_message, operations
	  FROM apply_jobs ORDER BY created_at DESC, rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("apply: list jobs: %w", err)
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// MarkStatus transitions a job to a non-terminal status, setting started_at on
// first transition out of pending.
func (s *JobStore) MarkStatus(id, status, code, message string) error {
	result, err := s.db.Exec(`UPDATE apply_jobs SET status=?,
  started_at = COALESCE(started_at, ?),
  error_code = ?, error_message = ? WHERE id=?`,
		status, nowUnix(), code, message, id)
	if err != nil {
		return fmt.Errorf("apply: mark job status: %w", err)
	}
	return requireOneJobRow(result, "mark status", id)
}

// Finish marks a job terminal and records finished_at.
func (s *JobStore) Finish(id, status, code, message string) error {
	result, err := s.db.Exec(`UPDATE apply_jobs SET status=?,
  started_at = COALESCE(started_at, ?), finished_at = ?,
  error_code = ?, error_message = ? WHERE id=?`,
		status, nowUnix(), nowUnix(), code, message, id)
	if err != nil {
		return fmt.Errorf("apply: finish job: %w", err)
	}
	return requireOneJobRow(result, "finish", id)
}

// SetOperations replaces the operation results recorded for a job.
func (s *JobStore) SetOperations(id string, ops []OperationResult) error {
	body, err := json.Marshal(ops)
	if err != nil {
		return err
	}
	if ops == nil {
		body = []byte("[]")
	}
	result, err := s.db.Exec(`UPDATE apply_jobs SET operations=? WHERE id=?`, string(body), id)
	if err != nil {
		return fmt.Errorf("apply: set job operations: %w", err)
	}
	return requireOneJobRow(result, "set operations", id)
}

func (s *JobStore) MarkApplyingInterrupted(message string) error {
	now := nowUnix()
	_, err := s.db.Exec(`UPDATE apply_jobs SET status=?, finished_at=?,
  error_code='INTERRUPTED', error_message=? WHERE status=?`,
		StatusFailed, now, message, StatusApplying)
	if err != nil {
		return fmt.Errorf("apply: mark stale jobs interrupted: %w", err)
	}
	return nil
}

func requireOneJobRow(result sql.Result, operation, id string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("apply: %s job %s affected %d rows", operation, id, rows)
	}
	return nil
}

// LatestForRevision returns the most recent job for a desired revision.
func (s *JobStore) LatestForRevision(rev uint64) (Job, bool, error) {
	row := s.db.QueryRow(`SELECT id, desired_revision, base_revision, status, trigger, actor_id,
	  created_at, started_at, finished_at, error_code, error_message, operations
	  FROM apply_jobs WHERE desired_revision=? ORDER BY created_at DESC, rowid DESC LIMIT 1`, rev)
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	return j, true, nil
}

type jobScanner interface{ Scan(dest ...any) error }

func scanJob(row jobScanner) (Job, error) {
	var j Job
	var started, finished sql.NullInt64
	var ops string
	err := row.Scan(&j.ID, &j.DesiredRevision, &j.BaseRevision, &j.Status, &j.Trigger,
		&j.ActorID, &j.CreatedAt, &started, &finished, &j.ErrorCode, &j.ErrorMessage, &ops)
	if err != nil {
		return Job{}, err
	}
	if started.Valid {
		j.StartedAt = &started.Int64
	}
	if finished.Valid {
		j.FinishedAt = &finished.Int64
	}
	if ops != "" {
		_ = json.Unmarshal([]byte(ops), &j.Operations)
	}
	return j, nil
}
