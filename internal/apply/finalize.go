package apply

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type runtimePublication struct {
	JobID                      string
	Revision                   uint64
	Generation                 uint64
	SnapshotSHA256             string
	Operations                 []OperationResult
	PublishedAt                int64
	OwnerProcess               string
	OperationID                string
	LeaseExpiresAt             int64
	Phase                      string
	ExpectedLiveManifestSHA256 string
	PreviousLiveManifestSHA256 string
	Artifacts                  []string
	LiveRoot                   string
	ServicePhase               string
	FirewallPhase              string
	Confirmations              []EnforcementConfirmation
}

func recordRuntimePublicationIntent(db *sql.DB, job Job, lease Lease, createdAt int64) error {
	digest, err := revisionSnapshotDigest(db, job.DesiredRevision)
	if err != nil {
		return fmt.Errorf("apply: digest intended revision snapshot: %w", err)
	}
	_, err = db.Exec(`INSERT INTO runtime_publications
  (job_id,revision,generation,snapshot_sha256,operations_json,published_at,
   owner_process,operation_id,lease_expires_at,phase,artifacts_json,service_phase,firewall_phase,updated_at)
  VALUES(?,?,?,?,?,0,?,?,?,'intent','[]','pending','pending',?)`,
		job.ID, job.DesiredRevision, lease.Generation, digest, "[]",
		lease.Owner, lease.Operation, lease.ExpiresAt, createdAt)
	if err != nil {
		return fmt.Errorf("apply: persist runtime publication intent: %w", err)
	}
	return nil
}

func markRuntimePublicationPublishing(db *sql.DB, jobID string, generation uint64, details PublicationDetails, updatedAt int64) error {
	artifacts, err := json.Marshal(details.Artifacts)
	if err != nil {
		return err
	}
	result, err := db.Exec(`UPDATE runtime_publications SET
 phase='publishing',
 expected_live_manifest_sha256=CASE WHEN ?<>'' THEN ? ELSE expected_live_manifest_sha256 END,
 previous_live_manifest_sha256=CASE WHEN ?<>'' THEN ? ELSE previous_live_manifest_sha256 END,
 artifacts_json=CASE WHEN ?<>'[]' THEN ? ELSE artifacts_json END,
 service_phase=CASE WHEN ?<>'' THEN ? ELSE service_phase END,
 firewall_phase=CASE WHEN ?<>'' THEN ? ELSE firewall_phase END,
 live_root=CASE WHEN ?<>'' THEN ? ELSE live_root END,
 updated_at=?
WHERE job_id=? AND generation=? AND phase IN ('intent','publishing')`,
		details.ExpectedLiveManifestSHA256, details.ExpectedLiveManifestSHA256,
		details.PreviousLiveManifestSHA256, details.PreviousLiveManifestSHA256,
		string(artifacts), string(artifacts), details.ServicePhase, details.ServicePhase,
		details.FirewallPhase, details.FirewallPhase, details.LiveRoot, details.LiveRoot,
		updatedAt, jobID, generation)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return errors.New("apply: publication intent is missing before runtime mutation")
	}
	return nil
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

func liveArtifactManifestDigest(root string, artifacts []string) (string, error) {
	if root == "" || !filepath.IsAbs(root) {
		return "", errors.New("apply: publication live root is unavailable")
	}
	ids := append([]string(nil), artifacts...)
	sort.Strings(ids)
	hash := sha256.New()
	for _, id := range ids {
		if id == "" || filepath.IsAbs(id) || id == ".." || strings.HasPrefix(id, "../") {
			return "", fmt.Errorf("apply: invalid publication artifact %q", id)
		}
		path := filepath.Join(root, filepath.FromSlash(id))
		info, err := os.Lstat(path)
		digest := "<absent>"
		if err == nil {
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("apply: publication artifact is not regular: %s", path)
			}
			body, readErr := os.ReadFile(path)
			if readErr != nil {
				return "", readErr
			}
			sum := sha256.Sum256(body)
			digest = hex.EncodeToString(sum[:])
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		fmt.Fprintf(hash, "%s\x00%s\n", id, digest)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func recordRuntimePublication(db *sql.DB, job Job, generation uint64, operations []OperationResult, confirmations []EnforcementConfirmation, publishedAt int64) error {
	body, err := json.Marshal(operations)
	if err != nil {
		return err
	}
	confirmationBody, err := json.Marshal(confirmations)
	if err != nil {
		return err
	}
	result, err := db.Exec(`UPDATE runtime_publications SET phase='published',operations_json=?,confirmations_json=?,published_at=?,updated_at=?
	WHERE job_id=? AND revision=? AND generation=? AND phase IN ('intent','publishing')`,
		string(body), string(confirmationBody), publishedAt, publishedAt, job.ID, job.DesiredRevision, generation)
	if err != nil {
		return fmt.Errorf("apply: persist runtime publication receipt: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return errors.New("apply: publication intent is missing or fenced")
	}
	return nil
}

func markRuntimePublicationRolledBack(db *sql.DB, jobID string, generation uint64, now int64) error {
	result, err := db.Exec(`UPDATE runtime_publications SET phase='rolled_back',updated_at=?
WHERE job_id=? AND generation=? AND phase IN ('intent','publishing')`, now, jobID, generation)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return errors.New("apply: publication intent is missing while recording rollback")
	}
	return nil
}

func finalizeFencedJob(db *sql.DB, owner string, generation uint64, now time.Time, job Job, status, code, message string, operations []OperationResult, confirmations []EnforcementConfirmation, markApplied, recovery bool) error {
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
		for _, confirmation := range confirmations {
			if confirmation.ClientID == "" {
				return errors.New("apply: enforcement confirmation has no client")
			}
			table := ""
			switch confirmation.Kind {
			case "expiration":
				table = "expiration_enforcement"
			case "quota":
				table = "quota_enforcement"
			default:
				return fmt.Errorf("apply: unsupported enforcement confirmation %q", confirmation.Kind)
			}
			query := fmt.Sprintf(`UPDATE %s SET state='enforced',applied_revision=?,next_retry_at=0,last_error='',updated_at=?
WHERE client_id=? AND desired_revision=?`, table)
			result, err := tx.Exec(query, job.DesiredRevision, now.UTC().Unix(), confirmation.ClientID, job.DesiredRevision)
			if err != nil {
				return fmt.Errorf("apply: confirm %s enforcement: %w", confirmation.Kind, err)
			}
			if rows, _ := result.RowsAffected(); rows != 1 {
				return fmt.Errorf("apply: %s enforcement confirmation is stale", confirmation.Kind)
			}
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
	} else if status == StatusFailed {
		if _, err := tx.Exec(`DELETE FROM runtime_publications WHERE job_id=? AND phase='rolled_back'`, job.ID); err != nil {
			return fmt.Errorf("apply: consume rolled-back publication intent: %w", err)
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
	result, err := tx.Exec(`UPDATE apply_jobs SET status=?, started_at=COALESCE(started_at,?), finished_at=NULL,
  error_code='FINALIZATION_PENDING', error_message=?
  WHERE id=? AND owner_process=? AND lease_generation=?`,
		StatusApplying, finished, cause.Error(), jobID, owner, generation)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrApplyLeaseLost
	}
	result, err = tx.Exec(`UPDATE runtime_publications SET phase='finalization_pending',updated_at=?
WHERE job_id=? AND owner_process=? AND generation=?`, finished, jobID, owner, generation)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return errors.New("apply: publication receipt missing during pending finalization")
	}
	// The live runtime has changed but its receipt is unresolved. Retain the
	// durable lease so no newer generation can publish until recovery finalizes
	// this exact job.
	return tx.Commit()
}

func listRuntimePublications(db *sql.DB) ([]runtimePublication, error) {
	rows, err := db.Query(`SELECT job_id, revision, generation, snapshot_sha256, operations_json, published_at,
 owner_process,operation_id,lease_expires_at,phase,expected_live_manifest_sha256,
 previous_live_manifest_sha256,artifacts_json,live_root,confirmations_json,service_phase,firewall_phase
FROM runtime_publications ORDER BY revision, published_at, job_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var receipts []runtimePublication
	for rows.Next() {
		var receipt runtimePublication
		var operations, artifacts, confirmations string
		if err := rows.Scan(&receipt.JobID, &receipt.Revision, &receipt.Generation, &receipt.SnapshotSHA256, &operations, &receipt.PublishedAt,
			&receipt.OwnerProcess, &receipt.OperationID, &receipt.LeaseExpiresAt, &receipt.Phase,
			&receipt.ExpectedLiveManifestSHA256, &receipt.PreviousLiveManifestSHA256, &artifacts, &receipt.LiveRoot, &confirmations,
			&receipt.ServicePhase, &receipt.FirewallPhase); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(operations), &receipt.Operations); err != nil {
			return nil, fmt.Errorf("apply: decode publication operations: %w", err)
		}
		if err := json.Unmarshal([]byte(artifacts), &receipt.Artifacts); err != nil {
			return nil, fmt.Errorf("apply: decode publication artifacts: %w", err)
		}
		if err := json.Unmarshal([]byte(confirmations), &receipt.Confirmations); err != nil {
			return nil, fmt.Errorf("apply: decode publication confirmations: %w", err)
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
		if receipt.Phase == "publishing" {
			if receipt.ExpectedLiveManifestSHA256 == "" || receipt.PreviousLiveManifestSHA256 == "" {
				if receipt.ServicePhase == "restart-panel" || receipt.ServicePhase == "update-install" {
					if _, err := db.Exec(`UPDATE runtime_publications SET phase='published',published_at=?,updated_at=? WHERE job_id=? AND generation=?`,
						now().UTC().Unix(), now().UTC().Unix(), receipt.JobID, receipt.Generation); err != nil {
						return err
					}
					receipt.Phase = "published"
				} else {
					return fmt.Errorf("apply: publication %s lacks manifest recovery evidence", receipt.JobID)
				}
			} else {
				current, err := liveArtifactManifestDigest(receipt.LiveRoot, receipt.Artifacts)
				if err != nil {
					return fmt.Errorf("apply: verify publishing transaction %s: %w", receipt.JobID, err)
				}
				switch current {
				case receipt.ExpectedLiveManifestSHA256:
					if _, err := db.Exec(`UPDATE runtime_publications SET phase='published',published_at=?,updated_at=? WHERE job_id=? AND generation=?`,
						now().UTC().Unix(), now().UTC().Unix(), receipt.JobID, receipt.Generation); err != nil {
						return err
					}
					receipt.Phase = "published"
				case receipt.PreviousLiveManifestSHA256:
					if _, err := db.Exec(`UPDATE runtime_publications SET phase='rolled_back',updated_at=? WHERE job_id=? AND generation=?`,
						now().UTC().Unix(), receipt.JobID, receipt.Generation); err != nil {
						return err
					}
					receipt.Phase = "rolled_back"
				default:
					return fmt.Errorf("apply: publishing transaction %s has mixed live manifest", receipt.JobID)
				}
			}
		}

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
		if receipt.Phase == "intent" || receipt.Phase == "rolled_back" {
			if receipt.Phase == "intent" {
				if err := markRuntimePublicationRolledBack(db, receipt.JobID, receipt.Generation, now().UTC().Unix()); err != nil {
					_ = leases.Release(owner, lease.Generation)
					return err
				}
			}
			if err := finalizeFencedJob(db, owner, lease.Generation, now(), job, StatusFailed,
				"PUBLICATION_NOT_STARTED", "publication owner exited before runtime mutation", nil, nil, false, true); err != nil {
				_ = leases.Release(owner, lease.Generation)
				return err
			}
			continue
		}
		if err := finalizeFencedJob(db, owner, lease.Generation, now(), job, StatusSucceeded, "", "", receipt.Operations, receipt.Confirmations, true, true); err != nil {
			_ = leases.Release(owner, lease.Generation)
			return err
		}
	}
	return nil
}
