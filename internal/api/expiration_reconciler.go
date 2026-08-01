package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log"
	"time"

	"github.com/mikkelchokolate/Veil/internal/apply"
)

const expirationReconcileInterval = 250 * time.Millisecond

type expirationReconciler struct {
	state  *managementState
	cancel context.CancelFunc
	done   chan struct{}
}

func newExpirationReconciler(state *managementState) *expirationReconciler {
	return &expirationReconciler{state: state, done: make(chan struct{})}
}

func (r *expirationReconciler) Start() {
	ctx, cancel := context.WithCancel(r.state.lifecycleContext())
	r.cancel = cancel
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(expirationReconcileInterval)
		defer ticker.Stop()
		for {
			if err := r.ReconcileOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("client expiry reconciliation: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (r *expirationReconciler) Stop() {
	if r == nil || r.cancel == nil {
		return
	}
	r.cancel()
	<-r.done
}

type expiredClientCandidate struct {
	ID        string
	ExpiresAt int64
	CreatedAt int64
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
SELECT id,expires_at,created_at
FROM clients
WHERE enabled=1 AND expires_at IS NOT NULL AND expires_at<=?
  AND (created_at>? OR (created_at=? AND id>?))
ORDER BY created_at,id LIMIT 100`, now, afterCreated, afterCreated, afterID)
		if err != nil {
			return fmt.Errorf("list expired clients: %w", err)
		}
		candidates := make([]expiredClientCandidate, 0, 100)
		for rows.Next() {
			var candidate expiredClientCandidate
			if err := rows.Scan(&candidate.ID, &candidate.ExpiresAt, &candidate.CreatedAt); err != nil {
				_ = rows.Close()
				return err
			}
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
				Kind: "expiration", ClientID: candidate.ID,
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
		return s.recordExpirationFailure(candidate.ID, revision, message)
	}
	return nil
}

func (s *managementState) reserveExpirationRevisionLocked(candidate expiredClientCandidate, effectiveAt int64) (revision uint64, done bool, err error) {
	if !s.applyTrackingEnabled() || s.applySnapshots == nil {
		return 0, false, errors.New("expiration enforcement requires durable apply tracking")
	}
	var existingExpires, desired, applied, nextRetry int64
	var state string
	err = s.db.QueryRow(`SELECT expires_at,state,desired_revision,applied_revision,next_retry_at
FROM expiration_enforcement WHERE client_id=?`, candidate.ID).
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
				_, updateErr := s.db.Exec(`UPDATE expiration_enforcement SET state='enforced',applied_revision=?,next_retry_at=0,last_error='',updated_at=? WHERE client_id=? AND desired_revision=?`, desired, effectiveAt, candidate.ID, desired)
				return uint64(desired), true, updateErr
			}
			if nextRetry > effectiveAt {
				return uint64(desired), true, nil
			}
			_, updateErr := s.db.Exec(`UPDATE expiration_enforcement SET state='applying',attempts=attempts+1,next_retry_at=0,updated_at=? WHERE client_id=? AND desired_revision=?`, effectiveAt, candidate.ID, desired)
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
	_, err = tx.Exec(`INSERT INTO expiration_enforcement
(client_id,expires_at,state,desired_revision,applied_revision,effective_at,next_retry_at,last_error,attempts,updated_at)
VALUES(?,?,'applying',?,0,?,0,'',1,?)
ON CONFLICT(client_id) DO UPDATE SET expires_at=excluded.expires_at,state='applying',
 desired_revision=excluded.desired_revision,applied_revision=0,effective_at=excluded.effective_at,
 next_retry_at=0,last_error='',attempts=1,updated_at=excluded.updated_at`,
		candidate.ID, candidate.ExpiresAt, revision, effectiveAt, effectiveAt)
	if err != nil {
		return 0, false, err
	}
	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	return revision, false, nil
}

func (s *managementState) recordExpirationFailure(clientID string, revision uint64, message string) error {
	var attempts int
	if err := s.db.QueryRow(`SELECT attempts FROM expiration_enforcement WHERE client_id=? AND desired_revision=?`, clientID, revision).Scan(&attempts); err != nil {
		return err
	}
	delay := expirationRetryDelay(clientID, attempts)
	now := time.Now().UTC()
	_, err := s.db.Exec(`UPDATE expiration_enforcement
SET state='failed',next_retry_at=?,last_error=?,updated_at=?
WHERE client_id=? AND desired_revision=?`, now.Add(delay).Unix(), message, now.Unix(), clientID, revision)
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
