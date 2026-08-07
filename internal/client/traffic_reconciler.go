package client

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// QuotaMutation is one atomic quota-state transition. ResetPeriod clears only
// current-period counters; NextResetAt is the first future UTC boundary.
type QuotaMutation struct {
	ClientID          string
	Depleted          bool
	ResetPeriod       bool
	NextResetAt       *int64
	TargetGeneration  int64
	TargetPayloadHash string
	TargetPeriodEpoch int64
}

// Reconciler periodically evaluates current-period usage. Production supplies
// onMutation so counter reset, Client fields, revision and snapshot can share
// one transaction. onChange is retained for compatibility with standalone
// callers and is never used by the Panel production path.
type Reconciler struct {
	repo       *Repository
	traffic    *TrafficStore
	interval   time.Duration
	now        func() time.Time
	onChange   func(clientID string, depleted bool) error
	onMutation func(QuotaMutation) error

	mu      sync.Mutex
	running bool
	stop    chan struct{}
	done    chan struct{}
}

func NewReconciler(repo *Repository, traffic *TrafficStore, interval time.Duration, onChange func(string, bool) error) *Reconciler {
	return newReconciler(repo, traffic, interval, onChange, nil)
}

func NewTransactionalReconciler(repo *Repository, traffic *TrafficStore, interval time.Duration, onMutation func(QuotaMutation) error) *Reconciler {
	return newReconciler(repo, traffic, interval, nil, onMutation)
}

func newReconciler(repo *Repository, traffic *TrafficStore, interval time.Duration, onChange func(string, bool) error, onMutation func(QuotaMutation) error) *Reconciler {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Reconciler{
		repo: repo, traffic: traffic, interval: interval, now: time.Now,
		onChange: onChange, onMutation: onMutation,
	}
}

// ReconcileOnce evaluates every client with bounded pagination and continues
// after individual failures, returning one aggregate error.
func (r *Reconciler) ReconcileOnce() (changed int, err error) {
	now := r.now().UTC()
	var reconcileErrors []error
	type plannedMutation struct {
		clientID string
		mutation QuotaMutation
	}
	var afterCreated int64
	var afterID string
	for {
		clients, listErr := r.repo.ListKeyset(afterCreated, afterID, 100)
		if listErr != nil {
			return changed, errors.Join(append(reconcileErrors, listErr)...)
		}
		if len(clients) == 0 {
			break
		}
		planned := make([]plannedMutation, 0, len(clients))
		clientIDs := make([]string, 0, len(clients))
		for _, current := range clients {
			clientIDs = append(clientIDs, current.ID)
		}
		trafficTotals, totalsErr := r.traffic.TotalsForClients(clientIDs)
		if totalsErr != nil {
			return changed, errors.Join(append(reconcileErrors, totalsErr)...)
		}
		pendingTargets, pendingErr := r.pendingEnforcementTargets(clientIDs)
		if pendingErr != nil {
			return changed, errors.Join(append(reconcileErrors, pendingErr)...)
		}
		for _, current := range clients {
			mutation, needed, planErr := r.planWithTotals(current, now, trafficTotals[current.ID])
			if planErr != nil {
				reconcileErrors = append(reconcileErrors, fmt.Errorf("client %s: %w", current.ID, planErr))
				continue
			}
			pendingEntry := pendingTargets[current.ID]
			pendingMutation, pending := pendingEntry.mutation, pendingEntry.pending
			if needed {
				mutation = BindQuotaTarget(current, mutation)
			} else if pending {
				mutation = pendingMutation
				needed = true
			}
			if needed {
				planned = append(planned, plannedMutation{clientID: current.ID, mutation: mutation})
			}
		}
		pendingUpdates := make([]enforcementUpdate, 0, len(planned))
		for _, item := range planned {
			pendingUpdates = append(pendingUpdates, enforcementUpdate{clientID: item.clientID, state: "pending", mutation: item.mutation})
		}
		if markErr := r.markEnforcementBatch(pendingUpdates, now.Unix()); markErr != nil {
			reconcileErrors = append(reconcileErrors, fmt.Errorf("reserve quota enforcement keyset after %s: %w", afterID, markErr))
			afterCreated, afterID = clients[len(clients)-1].CreatedAt, clients[len(clients)-1].ID
			if len(clients) < 100 {
				break
			}
			continue
		}
		terminalUpdates := make([]enforcementUpdate, 0, len(planned))
		for _, item := range planned {
			if applyErr := r.applyMutation(item.mutation); applyErr != nil {
				terminalUpdates = append(terminalUpdates, enforcementUpdate{clientID: item.clientID, state: "failed", cause: applyErr, mutation: item.mutation})
				reconcileErrors = append(reconcileErrors, fmt.Errorf("client %s: %w", item.clientID, applyErr))
				continue
			}
			terminalUpdates = append(terminalUpdates, enforcementUpdate{clientID: item.clientID, state: "enforced", mutation: item.mutation})
			changed++
		}
		if markErr := r.markEnforcementBatch(terminalUpdates, now.Unix()); markErr != nil {
			reconcileErrors = append(reconcileErrors, fmt.Errorf("finalize quota enforcement keyset after %s: %w", afterID, markErr))
		}
		afterCreated, afterID = clients[len(clients)-1].CreatedAt, clients[len(clients)-1].ID
		if len(clients) < 100 {
			break
		}
	}
	return changed, errors.Join(reconcileErrors...)
}

func BindQuotaTarget(current Client, mutation QuotaMutation) QuotaMutation {
	mutation.TargetGeneration = int64(current.Version) + 1
	if current.QuotaResetAt != nil {
		mutation.TargetPeriodEpoch = *current.QuotaResetAt
	}
	nextReset := int64(0)
	if mutation.NextResetAt != nil {
		nextReset = *mutation.NextResetAt
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("client=%s;generation=%d;depleted=%t;reset=%t;period=%d;next=%d",
		mutation.ClientID, mutation.TargetGeneration, mutation.Depleted, mutation.ResetPeriod, mutation.TargetPeriodEpoch, nextReset)))
	mutation.TargetPayloadHash = hex.EncodeToString(digest[:])
	return mutation
}

func (r *Reconciler) pendingEnforcementTargets(clientIDs []string) (map[string]struct {
	mutation QuotaMutation
	pending  bool
}, error) {
	out := make(map[string]struct {
		mutation QuotaMutation
		pending  bool
	}, len(clientIDs))
	if len(clientIDs) == 0 || r == nil || r.repo == nil || r.repo.db == nil {
		return out, nil
	}
	placeholders := make([]string, len(clientIDs))
	args := make([]any, len(clientIDs))
	for i, id := range clientIDs {
		placeholders[i], args[i] = "?", id
	}
	query := "SELECT client_id,state,next_retry_at,target_generation,target_payload_hash,target_depleted,target_period_epoch FROM quota_enforcement WHERE client_id IN (" + strings.Join(placeholders, ",") + ") AND state<>'superseded'"
	rows, err := r.repo.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, state, hash string
		var nextRetry, generation, period int64
		var depleted int
		if err := rows.Scan(&id, &state, &nextRetry, &generation, &hash, &depleted, &period); err != nil {
			return nil, err
		}
		mutation := QuotaMutation{ClientID: id, TargetGeneration: generation, TargetPayloadHash: hash, TargetPeriodEpoch: period, Depleted: depleted != 0}
		pending := state == "pending" || state == "applying" || state == "failed"
		if state == "failed" && nextRetry > r.now().UTC().Unix() {
			pending = false
		}
		out[id] = struct {
			mutation QuotaMutation
			pending  bool
		}{mutation: mutation, pending: pending}
	}
	return out, rows.Err()
}

type enforcementUpdate struct {
	clientID string
	state    string
	cause    error
	mutation QuotaMutation
}

func (r *Reconciler) markEnforcementBatch(updates []enforcementUpdate, now int64) error {
	if len(updates) == 0 || r == nil || r.repo == nil || r.repo.db == nil {
		return nil
	}
	tx, err := r.repo.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, update := range updates {
		message := ""
		if update.cause != nil {
			message = update.cause.Error()
		}
		nextRetry := int64(0)
		if update.state != "enforced" {
			nextRetry = now
		}
		mutation := update.mutation
		if update.state == "pending" {
			if _, err := tx.Exec(`UPDATE quota_enforcement SET state='superseded',superseded_revision=desired_revision,updated_at=?
WHERE client_id=? AND state<>'superseded' AND (target_generation<>? OR target_payload_hash<>?)`,
				now, update.clientID, mutation.TargetGeneration, mutation.TargetPayloadHash); err != nil {
				return err
			}
			depleted := 0
			if mutation.Depleted {
				depleted = 1
			}
			if _, err := tx.Exec(`INSERT INTO quota_enforcement
  (client_id,target_generation,target_payload_hash,target_depleted,target_period_epoch,state,next_retry_at,last_error,attempts,updated_at)
  SELECT ?,?,?,?,?,?,?,?,1,? WHERE EXISTS(SELECT 1 FROM clients WHERE id=?)
  ON CONFLICT(client_id,target_generation) DO UPDATE SET
    state=CASE WHEN quota_enforcement.state='superseded' THEN quota_enforcement.state ELSE excluded.state END,
    next_retry_at=excluded.next_retry_at,last_error=excluded.last_error,
    attempts=quota_enforcement.attempts+1,updated_at=excluded.updated_at`,
				update.clientID, mutation.TargetGeneration, mutation.TargetPayloadHash, depleted, mutation.TargetPeriodEpoch,
				update.state, nextRetry, message, now, update.clientID); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.Exec(`UPDATE quota_enforcement SET state=?,next_retry_at=?,last_error=?,updated_at=?
WHERE client_id=? AND target_generation=? AND target_payload_hash=? AND state<>'superseded'`,
			update.state, nextRetry, message, now, update.clientID, mutation.TargetGeneration, mutation.TargetPayloadHash); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Reconciler) planWithTotals(current Client, now time.Time, totals [2]int64) (QuotaMutation, bool, error) {
	mutation := QuotaMutation{ClientID: current.ID, Depleted: current.Depleted}
	if current.QuotaBytes == nil {
		if current.Depleted {
			mutation.Depleted = false
			return mutation, true, nil
		}
		return QuotaMutation{}, false, nil
	}

	switch current.QuotaResetPolicy {
	case "", ResetNever:
	case ResetDaily, ResetWeekly, ResetMonthly:
		if current.QuotaResetAt == nil {
			next, err := nextQuotaBoundary(current.QuotaResetPolicy, now)
			if err != nil {
				return QuotaMutation{}, false, err
			}
			mutation.NextResetAt = &next
		} else if now.Unix() >= *current.QuotaResetAt {
			next, err := nextQuotaBoundary(current.QuotaResetPolicy, now)
			if err != nil {
				return QuotaMutation{}, false, err
			}
			mutation.Depleted = false
			mutation.ResetPeriod = true
			mutation.NextResetAt = &next
			return mutation, true, nil
		}
	default:
		return QuotaMutation{}, false, fmt.Errorf("client: unsupported quota reset policy %q", current.QuotaResetPolicy)
	}

	upload, download := totals[0], totals[1]
	mutation.Depleted = quotaReached(upload, download, *current.QuotaBytes)
	return mutation, mutation.Depleted != current.Depleted || mutation.NextResetAt != nil, nil
}

func quotaReached(upload, download, quota int64) bool {
	if quota <= 0 {
		return true
	}
	if upload >= quota || download >= quota {
		return true
	}
	return upload >= 0 && download >= quota-upload
}

func nextQuotaBoundary(policy string, now time.Time) (int64, error) {
	now = now.UTC()
	var next time.Time
	switch policy {
	case ResetDaily:
		next = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	case ResetWeekly:
		days := (int(time.Monday) - int(now.Weekday()) + 7) % 7
		if days == 0 {
			days = 7
		}
		next = time.Date(now.Year(), now.Month(), now.Day()+days, 0, 0, 0, 0, time.UTC)
	case ResetMonthly:
		next = time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	default:
		return 0, fmt.Errorf("client: unsupported quota reset policy %q", policy)
	}
	return next.Unix(), nil
}

func (r *Reconciler) applyMutation(mutation QuotaMutation) error {
	if r.onMutation != nil {
		return r.onMutation(mutation)
	}
	// Compatibility callback runs before any period reset, so an error cannot
	// leave counters reset while the Client mutation failed.
	if r.onChange != nil {
		if mutation.ResetPeriod {
			if err := r.preflightPeriodReset(mutation.ClientID); err != nil {
				return err
			}
		}
		if err := r.onChange(mutation.ClientID, mutation.Depleted); err != nil {
			return err
		}
	}
	return r.applyDirect(mutation)
}

func (r *Reconciler) preflightPeriodReset(clientID string) error {
	return r.traffic.WithRecordLock(func() error {
		tx, err := r.repo.BeginTx()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		return ResetQuotaPeriodTx(tx, clientID)
	})
}

func (r *Reconciler) applyDirect(mutation QuotaMutation) error {
	return r.traffic.WithRecordLock(func() error {
		return r.repo.WithTx(func(tx *Tx) error {
			if mutation.ResetPeriod {
				if err := ResetQuotaPeriodTx(tx, mutation.ClientID); err != nil {
					return err
				}
			}
			current, err := tx.Get(mutation.ClientID)
			if err != nil {
				return err
			}
			current.Depleted = mutation.Depleted
			if mutation.NextResetAt != nil {
				next := *mutation.NextResetAt
				current.QuotaResetAt = &next
			}
			_, err = tx.Update(current, current.Version)
			return err
		})
	})
}

// Start begins periodic reconciliation until Stop. Non-blocking.
func (r *Reconciler) Start() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	stop := make(chan struct{})
	done := make(chan struct{})
	r.stop = stop
	r.done = done
	r.mu.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if _, err := r.ReconcileOnce(); err != nil {
					log.Printf("traffic: quota reconciliation failed: %v", err)
				}
			}
		}
	}()
}

// Stop halts periodic reconciliation and joins the worker.
func (r *Reconciler) Stop() {
	r.mu.Lock()
	if !r.running {
		done := r.done
		r.mu.Unlock()
		if done != nil {
			<-done
		}
		return
	}
	r.running = false
	stop := r.stop
	done := r.done
	close(stop)
	r.mu.Unlock()
	<-done
	r.mu.Lock()
	if r.stop == stop {
		r.stop = nil
		r.done = nil
	}
	r.mu.Unlock()
}

func (r *Reconciler) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}
