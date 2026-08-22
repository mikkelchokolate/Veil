package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"time"

	"github.com/mikkelchokolate/Veil/internal/apply"
)

const expirationSafetySweepInterval = 5 * time.Minute

type expirationReconciler struct {
	state  *managementState
	cancel context.CancelFunc
	done   chan struct{}
	wake   chan struct{}
}

func newExpirationReconciler(state *managementState) *expirationReconciler {
	return &expirationReconciler{state: state, done: make(chan struct{}), wake: make(chan struct{}, 1)}
}

func (r *expirationReconciler) Start() {
	ctx, cancel := context.WithCancel(r.state.lifecycleContext())
	r.cancel = cancel
	go func() {
		defer close(r.done)
		for {
			if err := r.ReconcileOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("client expiry reconciliation: %v", err)
			}
			wait := expirationSafetySweepInterval
			if boundary, ok, err := r.nextBoundary(ctx); err != nil {
				log.Printf("client expiry next boundary: %v", err)
			} else if ok {
				wait = time.Until(time.Unix(boundary, 0))
				if wait < 0 {
					wait = 0
				}
				if wait > expirationSafetySweepInterval {
					wait = expirationSafetySweepInterval
				}
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-r.wake:
				if !timer.Stop() {
					<-timer.C
				}
			case <-timer.C:
			}
		}
	}()
}

func (r *expirationReconciler) Signal() {
	if r == nil || r.wake == nil {
		return
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *expirationReconciler) nextBoundary(ctx context.Context) (int64, bool, error) {
	if r == nil || r.state == nil || r.state.db == nil {
		return 0, false, nil
	}
	now := time.Now().UTC().Unix()
	var boundary sql.NullInt64
	err := r.state.db.QueryRowContext(ctx, `
SELECT MIN(boundary) FROM (
  SELECT MIN(c.expires_at) AS boundary
  FROM clients c
  LEFT JOIN expiration_enforcement e ON e.client_id=c.id AND e.state<>'superseded'
  WHERE c.enabled=1 AND c.expires_at>? AND
        (e.id IS NULL OR NOT (e.state='enforced' AND e.target_generation=c.version AND e.target_expires_at=c.expires_at))
  UNION ALL
  SELECT MIN(next_retry_at) AS boundary FROM expiration_enforcement
  WHERE state='failed' AND next_retry_at>?
)`, now, now).Scan(&boundary)
	if err != nil {
		return 0, false, err
	}
	return boundary.Int64, boundary.Valid, nil
}

func (r *expirationReconciler) Stop() {
	if r == nil || r.cancel == nil {
		return
	}
	r.cancel()
	<-r.done
}

type expiredClientCandidate struct {
	ID                string
	ExpiresAt         int64
	CreatedAt         int64
	TargetGeneration  int64
	TargetPayloadHash string
}

// ReconcileOnce uses the unique (created_at,id) keyset. It also considers
// every missed boundary on startup because the predicate is expires_at<=now.
func (r *expirationReconciler) ReconcileOnce(ctx context.Context) error {
	if r == nil || r.state == nil || r.state.db == nil {
		return nil
	}
	now := time.Now().UTC().Unix()
	var afterCreated int64 = -1
	afterID := ""
	for {
		rows, err := r.state.db.QueryContext(ctx, `
SELECT c.id,c.expires_at,c.created_at,c.version
FROM clients c
LEFT JOIN expiration_enforcement e ON e.client_id=c.id AND e.state<>'superseded'
WHERE c.enabled=1 AND c.expires_at IS NOT NULL AND c.expires_at<=?
  AND (e.id IS NULL OR NOT (e.state='enforced' AND e.target_generation=c.version AND e.target_expires_at=c.expires_at))
  AND (c.created_at>? OR (c.created_at=? AND c.id>?))
ORDER BY c.created_at,c.id LIMIT 100`, now, afterCreated, afterCreated, afterID)
		if err != nil {
			return fmt.Errorf("list expired clients: %w", err)
		}
		candidates := make([]expiredClientCandidate, 0, 100)
		for rows.Next() {
			var candidate expiredClientCandidate
			if err := rows.Scan(&candidate.ID, &candidate.ExpiresAt, &candidate.CreatedAt, &candidate.TargetGeneration); err != nil {
				_ = rows.Close()
				return err
			}
			digest := sha256.Sum256([]byte(fmt.Sprintf("client=%s;generation=%d;expires=%d;depleted=true", candidate.ID, candidate.TargetGeneration, candidate.ExpiresAt)))
			candidate.TargetPayloadHash = hex.EncodeToString(digest[:])
			candidates = append(candidates, candidate)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil
		}
		for _, candidate := range candidates {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := r.state.enforceExpiration(ctx, candidate, now); err != nil {
				log.Printf("client expiry %s: %v", candidate.ID, err)
			}
			afterCreated, afterID = candidate.CreatedAt, candidate.ID
		}
		if len(candidates) < 100 {
			return nil
		}
	}
}

func (s *managementState) enforceExpiration(ctx context.Context, candidate expiredClientCandidate, effectiveAt int64) error {
	s.mu.Lock()
	if s.runtimeVerificationUnknown || s.clientSubsystemStopping {
		s.mu.Unlock()
		return errors.New("expiration enforcement paused while runtime verification is unknown")
	}
	revision, done, err := s.reserveExpirationRevisionLocked(candidate, effectiveAt)
	s.mu.Unlock()
	if err != nil || done || revision == 0 {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	job, applyErr := s.applyRunner.RunOperationContext(ctx, revision, "expiration", "system",
		apply.ContextExecutorFunc(func(operationContext context.Context, pinnedRevision uint64) (apply.Result, error) {
			result, runErr := s.executeApplyRevisionContext(operationContext, pinnedRevision)
			result.Confirmations = append(result.Confirmations, apply.EnforcementConfirmation{
				Kind: "expiration", ClientID: candidate.ID, TargetGeneration: candidate.TargetGeneration, TargetPayloadHash: candidate.TargetPayloadHash,
			})
			return result, runErr
		}))
	if applyErr != nil || job.Status != apply.StatusSucceeded {
		message := "expiration apply failed"
		if applyErr != nil {
			message = applyErr.Error()
		} else if job.ErrorMessage != "" {
			message = job.ErrorMessage
		}
		return s.recordExpirationFailure(candidate, revision, message)
	}
	return nil
}

func (s *managementState) reserveExpirationRevisionLocked(candidate expiredClientCandidate, effectiveAt int64) (revision uint64, done bool, err error) {
	if !s.applyTrackingEnabled() || s.applySnapshots == nil {
		return 0, false, errors.New("expiration enforcement requires durable apply tracking")
	}
	if candidate.TargetGeneration <= 0 || candidate.TargetPayloadHash == "" {
		if err := s.db.QueryRow(`SELECT version FROM clients WHERE id=?`, candidate.ID).Scan(&candidate.TargetGeneration); err != nil {
			return 0, false, err
		}
		digest := sha256.Sum256([]byte(fmt.Sprintf("client=%s;generation=%d;expires=%d;depleted=true", candidate.ID, candidate.TargetGeneration, candidate.ExpiresAt)))
		candidate.TargetPayloadHash = hex.EncodeToString(digest[:])
	}
	var existingExpires, desired, applied, nextRetry int64
	var state string
	err = s.db.QueryRow(`SELECT target_expires_at,state,desired_revision,applied_revision,next_retry_at
FROM expiration_enforcement WHERE client_id=? AND target_generation=? AND target_payload_hash=? AND state<>'superseded'`,
		candidate.ID, candidate.TargetGeneration, candidate.TargetPayloadHash).
		Scan(&existingExpires, &state, &desired, &applied, &nextRetry)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}
	if err == nil && existingExpires == candidate.ExpiresAt {
		if state == "enforced" && desired > 0 && applied >= desired {
			return uint64(desired), true, nil
		}
		if desired > 0 {
			current, readErr := s.applyRevisions.Get()
			if readErr != nil {
				return 0, false, readErr
			}
			if current.Applied >= uint64(desired) {
				_, updateErr := s.db.Exec(`UPDATE expiration_enforcement SET state='enforced',applied_revision=?,next_retry_at=0,last_error='',updated_at=? WHERE client_id=? AND target_generation=? AND target_payload_hash=? AND desired_revision=? AND state<>'superseded'`, desired, effectiveAt, candidate.ID, candidate.TargetGeneration, candidate.TargetPayloadHash, desired)
				return uint64(desired), true, updateErr
			}
			if nextRetry > effectiveAt {
				return uint64(desired), true, nil
			}
			_, updateErr := s.db.Exec(`UPDATE expiration_enforcement SET state='applying',attempts=attempts+1,next_retry_at=0,updated_at=? WHERE client_id=? AND target_generation=? AND target_payload_hash=? AND desired_revision=? AND state<>'superseded'`, effectiveAt, candidate.ID, candidate.TargetGeneration, candidate.TargetPayloadHash, desired)
			return uint64(desired), false, updateErr
		}
	}

	var enabled bool
	var currentExpiry sql.NullInt64
	if err := s.db.QueryRow(`SELECT enabled,expires_at FROM clients WHERE id=?`, candidate.ID).Scan(&enabled, &currentExpiry); err != nil {
		return 0, false, err
	}
	if !enabled || !currentExpiry.Valid || currentExpiry.Int64 != candidate.ExpiresAt || currentExpiry.Int64 > effectiveAt {
		return 0, true, nil
	}

	snapshot, err := s.snapshotLocked()
	if err != nil {
		return 0, false, err
	}
	if effectiveAt < candidate.ExpiresAt {
		effectiveAt = candidate.ExpiresAt
	}
	snapshot.EffectiveAt = effectiveAt
	if err := s.encryptSnapshot(&snapshot); err != nil {
		return 0, false, err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return 0, false, err
	}
	stateDigest := ""
	if s.statePath != "" {
		stateDigest, err = stateFileDigest(s.statePath)
		if err != nil {
			return 0, false, err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	revision, err = apply.BumpDesiredTx(tx)
	if err != nil {
		return 0, false, err
	}
	if s.statePath == "" {
		err = apply.SaveSnapshotTx(tx, revision, payload)
	} else {
		err = apply.SaveSnapshotTxBound(tx, revision, payload, stateDigest)
	}
	if err != nil {
		return 0, false, err
	}
	if _, err := tx.Exec(`UPDATE expiration_enforcement SET state='superseded',superseded_revision=?,updated_at=?
WHERE client_id=? AND state<>'superseded' AND (target_generation<>? OR target_payload_hash<>?)`,
		revision, effectiveAt, candidate.ID, candidate.TargetGeneration, candidate.TargetPayloadHash); err != nil {
		return 0, false, err
	}
	_, err = tx.Exec(`INSERT INTO expiration_enforcement
(client_id,target_generation,target_payload_hash,target_expires_at,state,desired_revision,applied_revision,effective_at,next_retry_at,last_error,attempts,updated_at)
VALUES(?,?,?,?,'applying',?,0,?,0,'',1,?)
ON CONFLICT(client_id,target_generation) DO UPDATE SET
 target_payload_hash=excluded.target_payload_hash,target_expires_at=excluded.target_expires_at,state='applying',
 desired_revision=excluded.desired_revision,applied_revision=0,effective_at=excluded.effective_at,
 next_retry_at=0,last_error='',attempts=1,updated_at=excluded.updated_at`,
		candidate.ID, candidate.TargetGeneration, candidate.TargetPayloadHash, candidate.ExpiresAt, revision, effectiveAt, effectiveAt)
	if err != nil {
		return 0, false, err
	}
	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	return revision, false, nil
}

func (s *managementState) recordExpirationFailure(candidate expiredClientCandidate, revision uint64, message string) error {
	var attempts int
	if err := s.db.QueryRow(`SELECT attempts FROM expiration_enforcement WHERE client_id=? AND target_generation=? AND target_payload_hash=? AND desired_revision=?`, candidate.ID, candidate.TargetGeneration, candidate.TargetPayloadHash, revision).Scan(&attempts); err != nil {
		return err
	}
	delay := expirationRetryDelay(candidate.ID, attempts)
	now := time.Now().UTC()
	_, err := s.db.Exec(`UPDATE expiration_enforcement
SET state='failed',next_retry_at=?,last_error=?,updated_at=?
WHERE client_id=? AND target_generation=? AND target_payload_hash=? AND desired_revision=? AND state<>'superseded'`,
		now.Add(delay).Unix(), message, now.Unix(), candidate.ID, candidate.TargetGeneration, candidate.TargetPayloadHash, revision)
	return err
}

func expirationRetryDelay(clientID string, attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > 8 {
		attempts = 8
	}
	base := time.Second << (attempts - 1)
	if base > 5*time.Minute {
		base = 5 * time.Minute
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(clientID))
	jitter := time.Duration(h.Sum32()%250) * time.Millisecond
	return base + jitter
}
