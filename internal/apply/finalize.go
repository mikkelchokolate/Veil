package apply

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type runtimePublication struct {
	JobID          string
	Revision       uint64
	Generation     uint64
	SnapshotSHA256 string
	Operations     []OperationResult
	PublishedAt    int64
}

func revisionSnapshotDigest(db *sql.DB, revision uint64) (string, error) {
	var payload []byte
	err := db.QueryRow(`SELECT payload FROM revision_snapshots WHERE revision=?`, revision).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func recordRuntimePublication(db *sql.DB, job Job, generation uint64, operations []OperationResult, publishedAt int64) error {
	digest, err := revisionSnapshotDigest(db, job.DesiredRevision)
	if err != nil {
		return fmt.Errorf("apply: digest published revision snapshot: %w", err)
	}
	body, err := json.Marshal(operations)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO runtime_publications
  (job_id, revision, generation, snapshot_sha256, operations_json, published_at)
  VALUES(?,?,?,?,?,?)
  ON CONFLICT(job_id) DO UPDATE SET
    revision=excluded.revision,
    generation=excluded.generation,
    snapshot_sha256=excluded.snapshot_sha256,
    operations_json=excluded.operations_json,
    published_at=excluded.published_at`,
		job.ID, job.DesiredRevision, generation, digest, string(body), publishedAt)
	if err != nil {
		return fmt.Errorf("apply: persist runtime publication receipt: %w", err)
	}
	return nil
}

func finalizeFencedJob(db *sql.DB, owner string, generation uint64, now time.Time, job Job, status, code, message string, operations []OperationResult, markApplied, recovery bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := assertLeaseCurrentTx(tx, owner, generation, now); err != nil {
		return err
	}
	if markApplied {
		result, err := tx.Exec(`UPDATE revisions SET applied_revision=?
  WHERE id=1 AND desired_revision>=? AND applied_revision<=?`, job.DesiredRevision, job.DesiredRevision, job.DesiredRevision)
		if err != nil {
			return fmt.Errorf("apply: mark applied in finalization: %w", err)
		}
		if rows, _ := result.RowsAffected(); rows != 1 {
			return fmt.Errorf("apply: revision %d cannot be finalized", job.DesiredRevision)
		}
	}
	body, err := json.Marshal(operations)
	if err != nil {
		return err
	}
	finishedAt := now.UTC().Unix()
	query := `UPDATE apply_jobs SET status=?, started_at=COALESCE(started_at,?), finished_at=?,
  error_code=?, error_message=?, operations=? WHERE id=? AND owner_process=? AND lease_generation=?`
	args := []any{status, finishedAt, finishedAt, code, message, string(body), job.ID, owner, generation}
	if recovery {
		query = `UPDATE apply_jobs SET status=?, started_at=COALESCE(started_at,?), finished_at=?,
  error_code=?, error_message=?, operations=? WHERE id=?`
		args = []any{status, finishedAt, finishedAt, code, message, string(body), job.ID}
	}
	result, err := tx.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("apply: finalize job: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrApplyLeaseLost
	}
	if markApplied {
		if _, err := tx.Exec(`DELETE FROM runtime_publications WHERE job_id=?`, job.ID); err != nil {
			return fmt.Errorf("apply: consume publication receipt: %w", err)
		}
	}
	result, err = tx.Exec(`UPDATE apply_lease
SET owner_process='', lease_expires_at=0, heartbeat_at=0, current_operation=''
WHERE id=1 AND owner_process=? AND generation=?`, owner, generation)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrApplyLeaseLost
	}
	return tx.Commit()
}

func markFinalizationPending(db *sql.DB, owner string, generation uint64, now time.Time, jobID string, cause error) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := assertLeaseCurrentTx(tx, owner, generation, now); err != nil {
		return err
	}
	finished := now.UTC().Unix()
	result, err := tx.Exec(`UPDATE apply_jobs SET status=?, started_at=COALESCE(started_at,?), finished_at=?,
  error_code='FINALIZATION_PENDING', error_message=?
  WHERE id=? AND owner_process=? AND lease_generation=?`,
		StatusFailed, finished, finished, cause.Error(), jobID, owner, generation)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrApplyLeaseLost
	}
	result, err = tx.Exec(`UPDATE apply_lease
SET owner_process='', lease_expires_at=0, heartbeat_at=0, current_operation=''
WHERE id=1 AND owner_process=? AND generation=?`, owner, generation)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrApplyLeaseLost
	}
	return tx.Commit()
}

func listRuntimePublications(db *sql.DB) ([]runtimePublication, error) {
	rows, err := db.Query(`SELECT job_id, revision, generation, snapshot_sha256, operations_json, published_at
FROM runtime_publications ORDER BY revision, published_at, job_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var receipts []runtimePublication
	for rows.Next() {
		var receipt runtimePublication
		var operations string
		if err := rows.Scan(&receipt.JobID, &receipt.Revision, &receipt.Generation, &receipt.SnapshotSHA256, &operations, &receipt.PublishedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(operations), &receipt.Operations); err != nil {
			return nil, fmt.Errorf("apply: decode publication operations: %w", err)
		}
		receipts = append(receipts, receipt)
	}
	return receipts, rows.Err()
}

func recoverRuntimePublications(db *sql.DB, leases *LeaseStore, jobs *JobStore, owner string, now func() time.Time, ttl time.Duration) error {
	receipts, err := listRuntimePublications(db)
	if err != nil {
		return fmt.Errorf("apply: list runtime publication receipts: %w", err)
	}
	for _, receipt := range receipts {
		digest, err := revisionSnapshotDigest(db, receipt.Revision)
		if err != nil {
			return err
		}
		if digest != receipt.SnapshotSHA256 {
			return fmt.Errorf("apply: publication receipt %s snapshot digest mismatch", receipt.JobID)
		}
		lease, acquired, err := leases.Acquire(owner, "recover-publication:"+receipt.JobID, now(), ttl)
		if err != nil {
			return err
		}
		if !acquired {
			return ErrApplyBusy
		}
		job, err := jobs.Get(receipt.JobID)
		if err != nil {
			_ = leases.Release(owner, lease.Generation)
			return err
		}
		job.DesiredRevision = receipt.Revision
		if err := finalizeFencedJob(db, owner, lease.Generation, now(), job, StatusSucceeded, "", "", receipt.Operations, true, true); err != nil {
			_ = leases.Release(owner, lease.Generation)
			return err
		}
	}
	return nil
}
