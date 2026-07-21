package client

import (
	"sync"
	"time"
)

// Reconciler periodically marks clients as depleted when their cumulative
// usage crosses their quota, and clears the flag when a reset policy window
// rolls over. The onChange callback OWNS the depleted-flag write: callers
// route it through the unified mutation orchestration (atomic flag flip +
// desired-revision bump + immutable snapshot + one apply job). When onChange
// is nil the reconciler flips the flag directly (standalone/test use).
type Reconciler struct {
	repo     *Repository
	traffic  *TrafficStore
	interval time.Duration
	now      func() time.Time
	onChange func(clientID string, depleted bool) error

	mu      sync.Mutex
	running bool
	stop    chan struct{}
}

func NewReconciler(repo *Repository, traffic *TrafficStore, interval time.Duration, onChange func(string, bool) error) *Reconciler {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &Reconciler{repo: repo, traffic: traffic, interval: interval, now: time.Now, onChange: onChange}
}

// ReconcileOnce scans all clients and flips the depleted flag where usage has
// crossed quota. Returns the number of clients whose state changed.
func (r *Reconciler) ReconcileOnce() (changed int, err error) {
	clients, _, err := r.repo.List(ListFilter{PageSize: 10000})
	if err != nil {
		return 0, err
	}
	now := r.now()
	for _, c := range clients {
		depleted, reset := r.evaluate(c, now)
		if reset {
			// Reset window rolled over: clear counters. The flag flip (when the
			// client was depleted) goes through onChange like any other flip.
			_ = r.traffic.ResetForClient(c.ID)
			if c.Depleted {
				if err := r.applyChange(c.ID, false); err != nil {
					return changed, err
				}
				changed++
			}
			continue
		}
		if depleted != c.Depleted {
			if err := r.applyChange(c.ID, depleted); err != nil {
				return changed, err
			}
			changed++
		}
	}
	return changed, nil
}

// applyChange performs the depleted-flag flip: through the owning callback
// when one is attached (the callback persists the flag inside its atomic
// mutation), or directly when running standalone.
func (r *Reconciler) applyChange(clientID string, depleted bool) error {
	if r.onChange != nil {
		return r.onChange(clientID, depleted)
	}
	return r.repo.SetDepleted(clientID, depleted)
}

// evaluate decides whether the client is depleted and whether a reset window
// rolled over (so usage should reset to zero for the new period).
func (r *Reconciler) evaluate(c Client, now time.Time) (depleted, reset bool) {
	if c.QuotaBytes == nil {
		return false, false
	}
	// Reset rollover.
	if c.QuotaResetPolicy != "" && c.QuotaResetPolicy != ResetNever {
		if c.QuotaResetAt != nil && now.Unix() >= *c.QuotaResetAt {
			return false, true
		}
	}
	up, down, err := r.traffic.TotalsForClient(c.ID)
	if err != nil {
		return c.Depleted, false
	}
	return (up + down) >= *c.QuotaBytes, false
}

// Start begins periodic reconciliation until Stop. Non-blocking.
func (r *Reconciler) Start() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.stop = make(chan struct{})
	r.mu.Unlock()
	go func() {
		t := time.NewTicker(r.interval)
		defer t.Stop()
		for {
			select {
			case <-r.stop:
				return
			case <-t.C:
				_, _ = r.ReconcileOnce()
			}
		}
	}()
}

// Stop halts periodic reconciliation.
func (r *Reconciler) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}
	r.running = false
	close(r.stop)
}
