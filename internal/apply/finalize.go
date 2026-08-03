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
	"syscall"
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
	PhaseEvidence              PublicationDetails
}

func recordRuntimePublicationIntent(db *sql.DB, job Job, lease Lease, createdAt int64) error {
	digest, err := revisionSnapshotDigest(db, job.DesiredRevision)
	if err != nil {
		return fmt.Errorf("apply: digest intended revision snapshot: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO runtime_publications
  (job_id,revision,base_revision,generation,snapshot_sha256,operations_json,published_at,
   owner_process,operation_id,lease_expires_at,phase,artifacts_json,service_phase,firewall_phase,updated_at)
  VALUES(?,?,?,?,?,?,0,?,?,?,'intent','[]','pending','pending',?)`,
		job.ID, job.DesiredRevision, job.BaseRevision, lease.Generation, digest, "[]",
		lease.Owner, lease.Operation, lease.ExpiresAt, createdAt)
	if err != nil {
		return fmt.Errorf("apply: persist runtime publication intent: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO runtime_publication_phases(job_id,phase,generation,evidence_json,committed_at) VALUES(?,?,?,?,?)`,
		job.ID, PublicationPhaseIntent, lease.Generation, `{}`, createdAt); err != nil {
		return fmt.Errorf("apply: persist publication intent phase evidence: %w", err)
	}
	return tx.Commit()
}

func markRuntimePublicationPublishing(db *sql.DB, jobID string, generation uint64, details PublicationDetails, updatedAt int64) error {
	return advanceRuntimePublicationPhase(db, jobID, generation, PublicationPhaseArtifactsPrepared, details, updatedAt)
}

func advanceRuntimePublicationPhase(db *sql.DB, jobID string, generation uint64, phase string, details PublicationDetails, updatedAt int64) error {
	allowedPrevious := ""
	switch phase {
	case PublicationPhaseArtifactsPrepared:
		allowedPrevious = "'intent','artifacts_prepared'"
	case PublicationPhaseArtifactsCommitted:
		allowedPrevious = "'artifacts_prepared','artifacts_committed'"
	case PublicationPhaseServicesPlanned:
		allowedPrevious = "'intent','artifacts_committed','services_planned'"
	case PublicationPhaseServicesConverged:
		allowedPrevious = "'services_planned','services_converged'"
	case PublicationPhaseHealthVerified:
		allowedPrevious = "'services_converged','health_verified'"
	case PublicationPhaseFirewallCommitted:
		allowedPrevious = "'health_verified','firewall_committed'"
	case PublicationPhaseSideEffectPlanned:
		allowedPrevious = "'intent','health_verified','firewall_committed','side_effect_planned'"
	case PublicationPhaseSideEffectCommitted:
		allowedPrevious = "'side_effect_planned','side_effect_committed'"
	case PublicationPhaseSideEffectVerified:
		allowedPrevious = "'side_effect_committed','side_effect_verified'"
	case PublicationPhaseRecoveryTransferred:
		allowedPrevious = "'artifacts_prepared','artifacts_committed','services_planned','services_converged','health_verified','firewall_committed','side_effect_planned','side_effect_committed','side_effect_verified'"
	default:
		return fmt.Errorf("apply: unsupported publication phase %q", phase)
	}
	artifacts, err := json.Marshal(details.Artifacts)
	if err != nil {
		return err
	}
	servicePlan, err := json.Marshal(details.ServiceActionPlan)
	if err != nil {
		return err
	}
	previousServiceStates, err := json.Marshal(details.PreviousServiceStates)
	if err != nil {
		return err
	}
	healthEvidence, err := json.Marshal(details.HealthEvidence)
	if err != nil {
		return err
	}
	evidence, err := json.Marshal(details)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := fmt.Sprintf(`UPDATE runtime_publications SET
 phase=?,
 expected_live_manifest_sha256=CASE WHEN ?<>'' THEN ? ELSE expected_live_manifest_sha256 END,
 previous_live_manifest_sha256=CASE WHEN ?<>'' THEN ? ELSE previous_live_manifest_sha256 END,
 artifacts_json=CASE WHEN ?<>'[]' AND ?<>'null' THEN ? ELSE artifacts_json END,
 service_phase=CASE WHEN ?<>'' THEN ? ELSE service_phase END,
 firewall_phase=CASE WHEN ?<>'' THEN ? ELSE firewall_phase END,
 live_root=CASE WHEN ?<>'' THEN ? ELSE live_root END,
 service_plan_json=CASE WHEN ?<>'[]' AND ?<>'null' THEN ? ELSE service_plan_json END,
 previous_service_states_json=CASE WHEN ?<>'{}' AND ?<>'null' THEN ? ELSE previous_service_states_json END,
 expected_service_generation=CASE WHEN ?<>'' THEN ? ELSE expected_service_generation END,
 expected_config_digest=CASE WHEN ?<>'' THEN ? ELSE expected_config_digest END,
 firewall_transaction_id=CASE WHEN ?<>'' THEN ? ELSE firewall_transaction_id END,
 previous_firewall_digest=CASE WHEN ?<>'' THEN ? ELSE previous_firewall_digest END,
 intended_firewall_digest=CASE WHEN ?<>'' THEN ? ELSE intended_firewall_digest END,
 health_evidence_json=CASE WHEN ?<>'[]' AND ?<>'null' THEN ? ELSE health_evidence_json END,
 updated_at=?
WHERE job_id=? AND generation=? AND phase IN (%s)
 AND EXISTS (SELECT 1 FROM apply_lease l WHERE l.id=1
   AND l.owner_process=runtime_publications.owner_process
   AND l.generation=runtime_publications.generation
   AND l.current_operation=runtime_publications.operation_id
   AND l.lease_expires_at>=?)`, allowedPrevious)
	result, err := tx.Exec(query,
		phase,
		details.ExpectedLiveManifestSHA256, details.ExpectedLiveManifestSHA256,
		details.PreviousLiveManifestSHA256, details.PreviousLiveManifestSHA256,
		string(artifacts), string(artifacts), string(artifacts),
		details.ServicePhase, details.ServicePhase,
		details.FirewallPhase, details.FirewallPhase,
		details.LiveRoot, details.LiveRoot,
		string(servicePlan), string(servicePlan), string(servicePlan),
		string(previousServiceStates), string(previousServiceStates), string(previousServiceStates),
		details.ExpectedServiceGeneration, details.ExpectedServiceGeneration,
		details.ExpectedConfigDigest, details.ExpectedConfigDigest,
		details.FirewallTransactionID, details.FirewallTransactionID,
		details.PreviousFirewallDigest, details.PreviousFirewallDigest,
		details.IntendedFirewallDigest, details.IntendedFirewallDigest,
		string(healthEvidence), string(healthEvidence), string(healthEvidence),
		updatedAt, jobID, generation, updatedAt)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return fmt.Errorf("apply: publication phase %s is missing, out of order, or fenced", phase)
	}
	if _, err := tx.Exec(`INSERT INTO runtime_publication_phases(job_id,phase,generation,evidence_json,committed_at)
VALUES(?,?,?,?,?)
ON CONFLICT(job_id,phase) DO UPDATE SET generation=excluded.generation,evidence_json=excluded.evidence_json,committed_at=excluded.committed_at`,
		jobID, phase, generation, string(evidence), updatedAt); err != nil {
		return fmt.Errorf("apply: persist %s phase evidence: %w", phase, err)
	}
	return tx.Commit()
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

func recordRuntimePublication(db *sql.DB, job Job, generation uint64, disposition ApplyDisposition, operations []OperationResult, confirmations []EnforcementConfirmation, allowSyntheticPhases bool, publishedAt int64) error {
	body, err := json.Marshal(operations)
	if err != nil {
		return err
	}
	confirmationBody, err := json.Marshal(confirmations)
	if err != nil {
		return err
	}
	phase := PublicationPhasePublished
	allowedPrevious := "'health_verified','firewall_committed','side_effect_verified'"
	if disposition == ApplyDispositionArtifactsCommitted {
		phase = PublicationPhaseArtifactsCommitted
		allowedPrevious = "'intent','artifacts_prepared','artifacts_committed'"
	}
	if allowSyntheticPhases {
		allowedPrevious += ",'intent','artifacts_prepared','services_planned','services_converged'"
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := fmt.Sprintf(`UPDATE runtime_publications SET phase=?,disposition=?,operations_json=?,confirmations_json=?,published_at=?,updated_at=?
WHERE job_id=? AND revision=? AND generation=? AND phase IN (%s)
 AND EXISTS (SELECT 1 FROM apply_lease l WHERE l.id=1
   AND l.owner_process=runtime_publications.owner_process
   AND l.generation=runtime_publications.generation
   AND l.current_operation=runtime_publications.operation_id
   AND l.lease_expires_at>=?)`, allowedPrevious)
	result, err := tx.Exec(query,
		phase, string(disposition), string(body), string(confirmationBody), publishedAt, publishedAt, job.ID, job.DesiredRevision, generation, publishedAt)
	if err != nil {
		return fmt.Errorf("apply: persist runtime publication receipt: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		var currentPhase, receiptOwner, leaseOwner, receiptOperation, leaseOperation string
		var receiptGeneration, leaseGeneration uint64
		var leaseExpires int64
		_ = tx.QueryRow(`SELECT phase,owner_process,generation,operation_id FROM runtime_publications WHERE job_id=?`, job.ID).Scan(&currentPhase, &receiptOwner, &receiptGeneration, &receiptOperation)
		_ = tx.QueryRow(`SELECT owner_process,generation,current_operation,lease_expires_at FROM apply_lease WHERE id=1`).Scan(&leaseOwner, &leaseGeneration, &leaseOperation, &leaseExpires)
		return fmt.Errorf("apply: publication intent is missing or fenced (phase=%s receipt_owner_match=%t receipt_generation=%d lease_owner_match=%t lease_generation=%d lease_live=%t operation_match=%t)", currentPhase, receiptOwner == leaseOwner, receiptGeneration, leaseOwner == job.OwnerProcess, leaseGeneration, leaseExpires >= publishedAt, receiptOperation == leaseOperation)
	}
	evidence, err := json.Marshal(map[string]any{"disposition": disposition, "operations": operations, "confirmations": confirmations})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO runtime_publication_phases(job_id,phase,generation,evidence_json,committed_at)
VALUES(?,?,?,?,?)
ON CONFLICT(job_id,phase) DO UPDATE SET generation=excluded.generation,evidence_json=excluded.evidence_json,committed_at=excluded.committed_at`,
		job.ID, phase, generation, string(evidence), publishedAt); err != nil {
		return fmt.Errorf("apply: persist publication receipt phase evidence: %w", err)
	}
	return tx.Commit()
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
		if _, err := tx.Exec(`INSERT INTO runtime_verification(id,historical_applied_revision,verified_revision,status,updated_at)
VALUES(1,?,?, 'verified',?)
ON CONFLICT(id) DO UPDATE SET verified_revision=excluded.verified_revision,status='verified',updated_at=excluded.updated_at`,
			job.DesiredRevision, job.DesiredRevision, now.UTC().Unix()); err != nil {
			return fmt.Errorf("apply: persist runtime verification: %w", err)
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
			if confirmation.TargetGeneration <= 0 || len(confirmation.TargetPayloadHash) != 64 {
				return fmt.Errorf("apply: %s confirmation has no exact target", confirmation.Kind)
			}
			query := fmt.Sprintf(`UPDATE %s SET state='enforced',applied_revision=?,next_retry_at=0,last_error='',updated_at=?
WHERE client_id=? AND target_generation=? AND target_payload_hash=? AND desired_revision=? AND state<>'superseded'`, table)
			args := []any{job.DesiredRevision, now.UTC().Unix(), confirmation.ClientID, confirmation.TargetGeneration, confirmation.TargetPayloadHash, job.DesiredRevision}
			result, err := tx.Exec(query, args...)
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
	terminalPublicationPhase := ""
	switch {
	case markApplied:
		terminalPublicationPhase = PublicationPhaseFinalized
	case status == StatusFailed && code == "PUBLICATION_RECOVERY_TRANSFERRED":
		terminalPublicationPhase = PublicationPhaseRecoveryTransferred
	case status == StatusFailed && recovery:
		terminalPublicationPhase = PublicationPhaseRolledBack
	case status == StatusFailed:
		terminalPublicationPhase = "rolled_back"
	case status == StatusStaged:
		terminalPublicationPhase = string(ApplyDispositionStaged)
	case status == StatusSucceeded:
		terminalPublicationPhase = "artifacts_finalized"
	}
	if terminalPublicationPhase != "" {
		if err := archiveRuntimePublicationTx(tx, job.ID, terminalPublicationPhase, finishedAt); err != nil {
			return err
		}
	}
	if markApplied {
		if _, err := tx.Exec(`DELETE FROM runtime_publications WHERE job_id=?`, job.ID); err != nil {
			return fmt.Errorf("apply: consume publication receipt: %w", err)
		}
	} else if status == StatusFailed {
		if _, err := tx.Exec(`DELETE FROM runtime_publications WHERE job_id=? AND phase IN ('rolled_back','recovery_transferred')`, job.ID); err != nil {
			return fmt.Errorf("apply: consume rolled-back publication intent: %w", err)
		}
	} else if status == StatusStaged {
		if _, err := tx.Exec(`DELETE FROM runtime_publications WHERE job_id=? AND phase='intent'`, job.ID); err != nil {
			return fmt.Errorf("apply: consume staged-only publication intent: %w", err)
		}
	} else if status == StatusSucceeded && !markApplied {
		if _, err := tx.Exec(`DELETE FROM runtime_publications WHERE job_id=? AND phase='artifacts_committed'`, job.ID); err != nil {
			return fmt.Errorf("apply: consume artifact-only publication receipt: %w", err)
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

func archiveRuntimePublicationTx(tx *sql.Tx, jobID, finalPhase string, finalizedAt int64) error {
	_, err := tx.Exec(`INSERT OR REPLACE INTO runtime_publication_history
  (job_id,revision,base_revision,final_phase,receipt_json,phases_json,finalized_at)
SELECT p.job_id,p.revision,p.base_revision,?,
 json_object(
   'revision',p.revision,'baseRevision',p.base_revision,'generation',p.generation,
   'snapshotSHA256',p.snapshot_sha256,'ownerProcess',p.owner_process,
   'operationId',p.operation_id,'phase',p.phase,
   'expectedLiveManifestSHA256',p.expected_live_manifest_sha256,
   'previousLiveManifestSHA256',p.previous_live_manifest_sha256,
   'artifacts',json(p.artifacts_json),'servicePlan',json(p.service_plan_json),
   'previousServiceStates',json(p.previous_service_states_json),
   'expectedServiceGeneration',p.expected_service_generation,
   'expectedConfigDigest',p.expected_config_digest,
   'firewallTransactionId',p.firewall_transaction_id,
   'previousFirewallDigest',p.previous_firewall_digest,
   'intendedFirewallDigest',p.intended_firewall_digest,
   'healthEvidence',json(p.health_evidence_json),'disposition',p.disposition),
 COALESCE((SELECT json_group_array(json_object(
   'phase',h.phase,'generation',h.generation,'evidence',json(h.evidence_json),'committedAt',h.committed_at))
   FROM runtime_publication_phases h WHERE h.job_id=p.job_id),'[]'),?
FROM runtime_publications p WHERE p.job_id=?`, finalPhase, finalizedAt, jobID)
	if err != nil {
		return fmt.Errorf("apply: archive runtime publication evidence: %w", err)
	}
	return nil
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
		StatusRecoveryPending, finished, cause.Error(), jobID, owner, generation)
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
 previous_live_manifest_sha256,artifacts_json,live_root,confirmations_json,service_phase,firewall_phase,
 COALESCE((SELECT evidence_json FROM runtime_publication_phases h WHERE h.job_id=runtime_publications.job_id ORDER BY committed_at DESC,rowid DESC LIMIT 1),'{}')
FROM runtime_publications ORDER BY revision, published_at, job_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var receipts []runtimePublication
	for rows.Next() {
		var receipt runtimePublication
		var operations, artifacts, confirmations, phaseEvidence string
		if err := rows.Scan(&receipt.JobID, &receipt.Revision, &receipt.Generation, &receipt.SnapshotSHA256, &operations, &receipt.PublishedAt,
			&receipt.OwnerProcess, &receipt.OperationID, &receipt.LeaseExpiresAt, &receipt.Phase,
			&receipt.ExpectedLiveManifestSHA256, &receipt.PreviousLiveManifestSHA256, &artifacts, &receipt.LiveRoot, &confirmations,
			&receipt.ServicePhase, &receipt.FirewallPhase, &phaseEvidence); err != nil {
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
		if err := json.Unmarshal([]byte(phaseEvidence), &receipt.PhaseEvidence); err != nil {
			return nil, fmt.Errorf("apply: decode publication phase evidence: %w", err)
		}
		receipts = append(receipts, receipt)
	}
	return receipts, rows.Err()
}

type updateHelperEvidence struct {
	Version              int    `json:"version"`
	TransactionID        string `json:"transactionId"`
	ExpectedBinaryDigest string `json:"expectedBinaryDigest"`
	OldBinaryDigest      string `json:"oldBinaryDigest"`
	InstalledPathInode   string `json:"installedPathInode"`
	TargetVersion        string `json:"targetVersion"`
	ActivationManifest   string `json:"activationManifest"`
	CommitPhase          string `json:"commitPhase"`
}

func readUpdateHelperEvidence(details PublicationDetails) (string, PublicationDetails, error) {
	path := filepath.Clean(details.ActivationManifest)
	if filepath.Base(path) != ".veil-update-evidence.json" || !filepath.IsAbs(path) {
		return "absent", details, nil
	}
	directoryInfo, directoryErr := os.Stat(filepath.Dir(path))
	if directoryErr != nil {
		return "invalid", details, directoryErr
	}
	if stat, ok := directoryInfo.Sys().(*syscall.Stat_t); !ok || stat.Uid != 0 || directoryInfo.Mode().Perm()&0o022 != 0 {
		return "invalid", details, errors.New("update helper evidence directory is not root-controlled")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "absent", details, nil
	}
	if err != nil {
		return "invalid", details, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return "invalid", details, errors.New("update helper evidence is not immutable root-owned data")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != 0 || stat.Nlink != 1 {
		return "invalid", details, errors.New("update helper evidence ownership is invalid")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "invalid", details, err
	}
	var evidence updateHelperEvidence
	if err := json.Unmarshal(body, &evidence); err != nil {
		return "invalid", details, err
	}
	if evidence.TransactionID != details.UpdateTransactionID || evidence.TargetVersion != details.TargetVersion {
		return "absent", details, nil
	}
	if evidence.CommitPhase == "intent" {
		return "intent", details, nil
	}
	if evidence.CommitPhase != "committed" || len(evidence.ExpectedBinaryDigest) != 64 || evidence.ActivationManifest != path {
		return "invalid", details, errors.New("update helper evidence is incomplete")
	}
	binaryPath := filepath.Join(filepath.Dir(path), "veil")
	binaryBody, err := os.ReadFile(binaryPath)
	if err != nil {
		return "invalid", details, err
	}
	digest := sha256.Sum256(binaryBody)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), evidence.ExpectedBinaryDigest) {
		return "invalid", details, errors.New("installed panel binary digest differs from helper evidence")
	}
	binaryInfo, err := os.Stat(binaryPath)
	if err != nil {
		return "invalid", details, err
	}
	if stat, ok := binaryInfo.Sys().(*syscall.Stat_t); ok {
		inode := fmt.Sprintf("%d:%d:%d:%d", stat.Dev, stat.Ino, stat.Ctim.Sec, stat.Ctim.Nsec)
		if inode != evidence.InstalledPathInode {
			return "invalid", details, errors.New("installed panel inode differs from helper evidence")
		}
	}
	details.ExpectedBinaryDigest = evidence.ExpectedBinaryDigest
	details.OldBinaryDigest = evidence.OldBinaryDigest
	details.InstalledInode = evidence.InstalledPathInode
	details.CommitPhase = evidence.CommitPhase
	return "committed", details, nil
}

type restartHelperEvidence struct {
	Version                  int    `json:"version"`
	TransactionID            string `json:"transactionId"`
	ExpectedExecutableDigest string `json:"expectedExecutableDigest"`
	PreviousStartGeneration  uint64 `json:"previousStartGeneration"`
	NewStartGeneration       uint64 `json:"newStartGeneration"`
	MainPID                  int    `json:"mainPid"`
	ServiceActive            bool   `json:"serviceActive"`
	ActivationManifest       string `json:"activationManifest"`
	CommitPhase              string `json:"commitPhase"`
}

func readRestartHelperEvidence(details PublicationDetails) (string, PublicationDetails, error) {
	path := filepath.Clean(details.ActivationManifest)
	if filepath.Base(path) != ".veil-restart-evidence.json" || !filepath.IsAbs(path) {
		return "absent", details, nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "absent", details, nil
	}
	if err != nil {
		return "invalid", details, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return "invalid", details, errors.New("restart helper evidence is not immutable")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != 0 || stat.Nlink != 1 {
		return "invalid", details, errors.New("restart helper evidence ownership is invalid")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "invalid", details, err
	}
	var evidence restartHelperEvidence
	if err := json.Unmarshal(body, &evidence); err != nil {
		return "invalid", details, err
	}
	if evidence.TransactionID != details.UpdateTransactionID {
		return "absent", details, nil
	}
	if evidence.CommitPhase == "intent" {
		return "intent", details, nil
	}
	if evidence.CommitPhase != "committed" || !evidence.ServiceActive || evidence.MainPID <= 0 || evidence.NewStartGeneration <= evidence.PreviousStartGeneration || len(evidence.ExpectedExecutableDigest) != 64 || evidence.ActivationManifest != path {
		return "invalid", details, errors.New("restart helper evidence is incomplete")
	}
	binaryBody, err := os.ReadFile(filepath.Join(filepath.Dir(path), "veil"))
	if err != nil {
		return "invalid", details, err
	}
	digest := sha256.Sum256(binaryBody)
	if !strings.EqualFold(hex.EncodeToString(digest[:]), evidence.ExpectedExecutableDigest) {
		return "invalid", details, errors.New("running panel executable digest differs from restart evidence")
	}
	details.ExpectedBinaryDigest = evidence.ExpectedExecutableDigest
	details.InstalledInode = fmt.Sprintf("pid:%d:start:%d", evidence.MainPID, evidence.NewStartGeneration)
	details.CommitPhase = evidence.CommitPhase
	return "committed", details, nil
}

func recoverRuntimePublications(db *sql.DB, leases *LeaseStore, jobs *JobStore, owner string, now func() time.Time, ttl time.Duration) error {
	receipts, err := listRuntimePublications(db)
	if err != nil {
		return fmt.Errorf("apply: list runtime publication receipts: %w", err)
	}
	for _, receipt := range receipts {
		if receipt.Phase == "publishing" || receipt.Phase == PublicationPhaseArtifactsPrepared {
			if receipt.ExpectedLiveManifestSHA256 == "" || receipt.PreviousLiveManifestSHA256 == "" {
				if receipt.ServicePhase == "restart-panel" || receipt.ServicePhase == "update-install" {
					// Entering the side-effect publication phase proves only intent. A
					// helper-owned commit receipt is required before recovery may claim
					// that restart or installation happened. With no such evidence this
					// operation is safely classified as not started, never successful.
					if _, err := db.Exec(`UPDATE runtime_publications SET phase='rolled_back',updated_at=? WHERE job_id=? AND generation=? AND phase=?`,
						now().UTC().Unix(), receipt.JobID, receipt.Generation, receipt.Phase); err != nil {
						return err
					}
					receipt.Phase = "rolled_back"
				} else {
					// The executor crossed the durable phase boundary but did not leave
					// manifest evidence proving whether an artifact changed. Preserve the
					// journal and transfer it to recovery; never infer success or rollback.
					receipt.Phase = PublicationPhaseArtifactsPrepared
				}
			} else {
				current, err := liveArtifactManifestDigest(receipt.LiveRoot, receipt.Artifacts)
				if err != nil {
					return fmt.Errorf("apply: verify publishing transaction %s: %w", receipt.JobID, err)
				}
				switch current {
				case receipt.ExpectedLiveManifestSHA256:
					// Filesystem convergence proves only artifact publication. Services,
					// health and firewall still require their own durable evidence.
					if _, err := db.Exec(`UPDATE runtime_publications SET phase='artifacts_committed',updated_at=? WHERE job_id=? AND generation=?`,
						now().UTC().Unix(), receipt.JobID, receipt.Generation); err != nil {
						return err
					}
					receipt.Phase = PublicationPhaseArtifactsCommitted
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
		if _, err := db.Exec(`UPDATE runtime_publications SET owner_process=?,generation=?,operation_id=?,lease_expires_at=?,updated_at=? WHERE job_id=? AND generation=?`,
			owner, lease.Generation, lease.Operation, lease.ExpiresAt, now().UTC().Unix(), receipt.JobID, receipt.Generation); err != nil {
			_ = leases.Release(owner, lease.Generation)
			return err
		}
		receipt.Generation = lease.Generation
		if receipt.Phase == PublicationPhaseSideEffectPlanned {
			if receipt.ServicePhase == "update-install" {
				evidenceState, evidence, evidenceErr := readUpdateHelperEvidence(receipt.PhaseEvidence)
				if evidenceErr != nil || evidenceState == "intent" || evidenceState == "invalid" {
					// Preserve the helper and publication journals for bounded recovery.
				} else if evidenceState == "committed" {
					if err := advanceRuntimePublicationPhase(db, receipt.JobID, receipt.Generation, PublicationPhaseSideEffectCommitted, evidence, now().UTC().Unix()); err != nil {
						_ = leases.Release(owner, lease.Generation)
						return err
					}
					if err := advanceRuntimePublicationPhase(db, receipt.JobID, receipt.Generation, PublicationPhaseSideEffectVerified, evidence, now().UTC().Unix()); err != nil {
						_ = leases.Release(owner, lease.Generation)
						return err
					}
					receipt.Phase = PublicationPhaseSideEffectVerified
				} else {
					if _, err := db.Exec(`UPDATE runtime_publications SET phase=?,updated_at=? WHERE job_id=? AND generation=? AND phase=?`, PublicationPhaseRolledBack, now().UTC().Unix(), receipt.JobID, receipt.Generation, PublicationPhaseSideEffectPlanned); err != nil {
						_ = leases.Release(owner, lease.Generation)
						return err
					}
					receipt.Phase = PublicationPhaseRolledBack
				}
			} else if receipt.ServicePhase == "restart-panel" {
				evidenceState, evidence, evidenceErr := readRestartHelperEvidence(receipt.PhaseEvidence)
				if evidenceErr != nil || evidenceState == "intent" || evidenceState == "invalid" {
					// Keep both journals for the recovery monitor.
				} else if evidenceState == "committed" {
					if err := advanceRuntimePublicationPhase(db, receipt.JobID, receipt.Generation, PublicationPhaseSideEffectCommitted, evidence, now().UTC().Unix()); err != nil {
						_ = leases.Release(owner, lease.Generation)
						return err
					}
					if err := advanceRuntimePublicationPhase(db, receipt.JobID, receipt.Generation, PublicationPhaseSideEffectVerified, evidence, now().UTC().Unix()); err != nil {
						_ = leases.Release(owner, lease.Generation)
						return err
					}
					receipt.Phase = PublicationPhaseSideEffectVerified
				} else {
					if _, err := db.Exec(`UPDATE runtime_publications SET phase=?,updated_at=? WHERE job_id=? AND generation=? AND phase=?`, PublicationPhaseRolledBack, now().UTC().Unix(), receipt.JobID, receipt.Generation, PublicationPhaseSideEffectPlanned); err != nil {
						_ = leases.Release(owner, lease.Generation)
						return err
					}
					receipt.Phase = PublicationPhaseRolledBack
				}
			}
		}
		if receipt.Phase == PublicationPhaseSideEffectVerified {
			if _, err := db.Exec(`UPDATE runtime_publications SET phase=?,published_at=?,updated_at=? WHERE job_id=? AND generation=? AND phase=?`, PublicationPhasePublished, now().UTC().Unix(), now().UTC().Unix(), receipt.JobID, receipt.Generation, PublicationPhaseSideEffectVerified); err != nil {
				_ = leases.Release(owner, lease.Generation)
				return err
			}
			receipt.Phase = PublicationPhasePublished
			receipt.PublishedAt = now().UTC().Unix()
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
		if receipt.Phase == PublicationPhaseArtifactsCommitted && receipt.PublishedAt > 0 {
			if err := finalizeFencedJob(db, owner, lease.Generation, now(), job, StatusSucceeded, "", "", receipt.Operations, nil, false, true); err != nil {
				_ = leases.Release(owner, lease.Generation)
				return err
			}
			continue
		}
		if receipt.Phase != PublicationPhasePublished && receipt.Phase != "finalization_pending" {
			// Transfer the durable lease and evidence to the recovery owner. No
			// later publication can proceed while this exact runtime generation is
			// unresolved.
			if _, err := db.Exec(`UPDATE runtime_publications SET owner_process=?,generation=?,operation_id=?,lease_expires_at=?,updated_at=? WHERE job_id=?`,
				owner, lease.Generation, lease.Operation, lease.ExpiresAt, now().UTC().Unix(), receipt.JobID); err != nil {
				_ = leases.Release(owner, lease.Generation)
				return err
			}
			if _, err := db.Exec(`UPDATE apply_jobs SET status=?,owner_process=?,lease_generation=?,finished_at=NULL,error_code='RECOVERY_PENDING',error_message='runtime publication requires exact-phase recovery' WHERE id=?`,
				StatusRecoveryPending, owner, lease.Generation, receipt.JobID); err != nil {
				_ = leases.Release(owner, lease.Generation)
				return err
			}
			continue
		}
		markApplied := receipt.Phase == "published" || receipt.Phase == "finalization_pending"
		if err := finalizeFencedJob(db, owner, lease.Generation, now(), job, StatusSucceeded, "", "", receipt.Operations, receipt.Confirmations, markApplied, true); err != nil {
			_ = leases.Release(owner, lease.Generation)
			return err
		}
	}
	return nil
}
