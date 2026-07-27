package client

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// QuotaMutation is one atomic quota-state transition. ResetPeriod clears only
// current-period counters; NextResetAt is the first future UTC boundary.
type QuotaMutation struct {
	ClientID    string
	Depleted    bool
	ResetPeriod bool
	NextResetAt *int64
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

// ReconcileOnce applies at most one atomic mutation per affected client.
func (r *Reconciler) ReconcileOnce() (changed int, err error) {
	clients, _, err := r.repo.List(ListFilter{PageSize: 10000})
	if err != nil {
		return 0, err
	}
	now := r.now().UTC()
	for _, current := range clients {
		mutation, needed, err := r.plan(current, now)
		if err != nil {
			return changed, err
		}
		if !needed {
			continue
		}
		if err := r.applyMutation(mutation); err != nil {
			return changed, err
		}
		changed++
	}
	return changed, nil
}

func (r *Reconciler) plan(current Client, now time.Time) (QuotaMutation, bool, error) {
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

	upload, download, err := r.traffic.TotalsForClient(current.ID)
	if err != nil {
		return QuotaMutation{}, false, err
	}
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
