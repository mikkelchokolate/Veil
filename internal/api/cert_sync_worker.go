package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// certSyncWorker periodically re-copies Caddy-managed certificates into the
// hysteria2 certificate directory. Apply-time Phase 2 syncs certificates only
// when a config is promoted, but Caddy renews ACME certificates continuously
// in between — without this worker the copy in <etc>/certs/<domain>.{crt,key}
// goes stale and hysteria2 keeps serving the old (eventually expired)
// certificate until the next apply (#1103).
//
// The copy itself reuses the privileged sync_caddy_cert operation — the same
// path validation, O_NOFOLLOW reads, atomic writes and ownership handling as
// the apply path. The helper reports whether bytes actually changed, and only
// a changed certificate restarts its hysteria2 instance (restart drops the
// runtime's sessions, so it must not run on every poll). A fencing lease is
// acquired like every other privileged mutation so a helper running with
// RequireFence still accepts the sync.
//
// After a successful apply, domains whose ACME issuance is still pending are
// retried on a bounded schedule (certSyncRetry*) instead of waiting for the
// next hourly pass — when the port-80 block clears the inbound converges to
// the real certificate within minutes, and when it never clears the visible
// status simply stays self-signed (#1168).
const certSyncInterval = time.Hour

// Default post-apply retry shape: an immediate pass on signal plus up to
// certSyncMaxAttempts follow-up passes every certSyncRetryInterval, all inside
// certSyncRetryWindow — a few attempts spread over ~3 minutes, bounded well
// under the 5-minute budget, after which the pending mark is dropped and the
// hourly pass remains the steady-state safety net (#1168).
const (
	certSyncRetryInterval = 30 * time.Second
	certSyncRetryWindow   = 5 * time.Minute
	certSyncMaxAttempts   = 6
)

type certSyncWorker struct {
	state    *managementState
	interval time.Duration
	// retryInterval, retryWindow and maxAttempts bound the post-apply pending
	// retry; they are fields (not the certSyncRetry* consts directly) so tests
	// can compress the schedule (#1168).
	retryInterval time.Duration
	retryWindow   time.Duration
	maxAttempts   int
	cancel        context.CancelFunc
	done          chan struct{}
	wake          chan struct{}

	pendingMu sync.Mutex
	// pending tracks domains whose ACME issuance was still missing at apply
	// time. Entries carry the attempts already made and a hard deadline so a
	// never-issued domain cannot retry forever (#1168).
	pending map[string]certSyncPending
	// now is a test seam for deadline checks.
	now func() time.Time
}

// certSyncPending is one domain whose certificate still needs ACME material.
type certSyncPending struct {
	attempts int
	deadline time.Time
}

// certSyncOutcome is the per-domain result of one sync pass.
type certSyncOutcome int

const (
	// certSyncMissing means ACME material is still absent — the helper may
	// have seeded fallback material but the domain stays pending (#1168).
	certSyncMissing certSyncOutcome = iota
	// certSyncFound means real ACME material is present in the destination.
	certSyncFound
	// certSyncErrored means the helper call failed; the attempt does not burn
	// retry budget (the hard deadline still bounds it).
	certSyncErrored
)

func newCertSyncWorker(state *managementState) *certSyncWorker {
	return &certSyncWorker{
		state:         state,
		interval:      certSyncInterval,
		retryInterval: certSyncRetryInterval,
		retryWindow:   certSyncRetryWindow,
		maxAttempts:   certSyncMaxAttempts,
		done:          make(chan struct{}),
		wake:          make(chan struct{}, 1),
		pending:       map[string]certSyncPending{},
		now:           time.Now,
	}
}

func (w *certSyncWorker) Start() {
	if w == nil {
		return
	}
	ctx, cancel := context.WithCancel(w.state.lifecycleContext())
	w.cancel = cancel
	go func() {
		defer close(w.done)
		// First pass shortly after startup: a previous instance may have died
		// between Caddy's renewal and its own next apply.
		wait := 30 * time.Second
		for {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-w.wake:
				if !timer.Stop() {
					<-timer.C
				}
			case <-timer.C:
			}
			outcomes, targets, err := w.syncOnce(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("cert-sync: %v", err)
			}
			w.reconcilePending(outcomes, targets)
			wait = w.nextWait()
		}
	}()
}

// nextWait picks the next poll delay: pending domains retry on the short
// retry interval, otherwise the steady-state hourly interval applies (#1168).
func (w *certSyncWorker) nextWait() time.Duration {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()
	if len(w.pending) > 0 && w.retryInterval < w.interval {
		return w.retryInterval
	}
	return w.interval
}

func (w *certSyncWorker) Stop() {
	if w == nil || w.cancel == nil {
		return
	}
	w.cancel()
	<-w.done
}

// Signal forces the next pass to run immediately (tests and post-apply nudges).
func (w *certSyncWorker) Signal() {
	if w == nil || w.wake == nil {
		return
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// clock returns the worker's time source, defaulting to the wall clock for
// workers built without newCertSyncWorker (hand-constructed test fixtures).
func (w *certSyncWorker) clock() time.Time {
	if w.now == nil {
		return time.Now()
	}
	return w.now()
}

// SignalPending marks domains whose ACME issuance was still pending at apply
// time and wakes the worker for an immediate retry pass. Repeats for a domain
// reset its attempt budget and deadline — each apply gets one fresh bounded
// retry window (#1168).
func (w *certSyncWorker) SignalPending(domains []string) {
	if w == nil {
		return
	}
	w.pendingMu.Lock()
	if w.pending == nil {
		w.pending = map[string]certSyncPending{}
	}
	retryWindow := w.retryWindow
	if retryWindow <= 0 {
		retryWindow = certSyncRetryWindow
	}
	for _, domain := range domains {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		w.pending[domain] = certSyncPending{deadline: w.clock().Add(retryWindow)}
	}
	w.pendingMu.Unlock()
	w.Signal()
}

// PendingDomain reports whether domain still has a pending ACME issuance the
// worker is retrying — the status endpoint uses it to mark an inbound's
// self-signed certificate as issuance-in-progress rather than final (#1168).
func (w *certSyncWorker) PendingDomain(domain string) bool {
	if w == nil {
		return false
	}
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()
	_, ok := w.pending[domain]
	return ok
}

// reconcilePending drops converged and stale pending entries and counts the
// attempts of the ones still missing ACME material. Entries are dropped when
// the domain left the live config set, when real material arrived, when the
// attempt budget is spent, or when the hard deadline passed — retries can
// never run unbounded (#1168).
func (w *certSyncWorker) reconcilePending(outcomes map[string]certSyncOutcome, targets []hysteria2CertSyncTarget) {
	w.pendingMu.Lock()
	defer w.pendingMu.Unlock()
	if len(w.pending) == 0 {
		return
	}
	targetDomains := make(map[string]bool, len(targets))
	for _, target := range targets {
		targetDomains[target.Domain] = true
	}
	now := w.clock()
	maxAttempts := w.maxAttempts
	if maxAttempts <= 0 {
		maxAttempts = certSyncMaxAttempts
	}
	for domain, pending := range w.pending {
		switch {
		case outcomes[domain] == certSyncFound:
			delete(w.pending, domain)
			continue
		case !targetDomains[domain]:
			// The domain is no longer served by any live hysteria2 config
			// (inbound removed, domain changed, apply rolled back).
			delete(w.pending, domain)
			continue
		case pending.attempts >= maxAttempts || !now.Before(pending.deadline):
			delete(w.pending, domain)
			continue
		}
		if outcomes[domain] == certSyncMissing {
			pending.attempts++
			w.pending[domain] = pending
		}
	}
}

// hysteria2CertSyncTarget pairs a Caddy-managed certificate domain with the
// hysteria2 systemd instance whose config serves it.
type hysteria2CertSyncTarget struct {
	Domain string
	Unit   string
}

// hysteria2CertSyncTargets maps each certificate domain found in the live
// hysteria2 configs to the unit that must restart when the certificate bytes
// change. Config filenames are the inbound names (<live>/hysteria2/<name>.yaml),
// which the runtime catalog renders as veil-hysteria2@<name>.service.
func hysteria2CertSyncTargets(configFiles []string) []hysteria2CertSyncTarget {
	var targets []hysteria2CertSyncTarget
	for _, path := range configFiles {
		if filepath.Base(filepath.Dir(path)) != "hysteria2" ||
			!strings.EqualFold(filepath.Ext(path), ".yaml") {
			continue
		}
		domains := hysteria2CertDomainsFromConfigs([]string{path})
		if len(domains) == 0 {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if name == "" {
			continue
		}
		for _, domain := range domains {
			targets = append(targets, hysteria2CertSyncTarget{
				Domain: domain,
				Unit:   "veil-hysteria2@" + name + ".service",
			})
		}
	}
	return targets
}

// SyncOnce copies renewed certificates and restarts the hysteria2 instances
// serving changed material. Best-effort per domain: individual failures are
// aggregated and the remaining domains still sync.
func (w *certSyncWorker) SyncOnce(ctx context.Context) error {
	_, _, err := w.syncOnce(ctx)
	return err
}

// syncOnce runs one pass and additionally reports each target's outcome and
// the target list itself so the caller can reconcile pending retries (#1168).
func (w *certSyncWorker) syncOnce(ctx context.Context) (map[string]certSyncOutcome, []hysteria2CertSyncTarget, error) {
	s := w.state
	if s == nil {
		return nil, nil, nil
	}
	s.mu.Lock()
	liveRoot := s.liveRoot
	privilegedBackend := s.privileged
	s.mu.Unlock()
	if privilegedBackend == nil || liveRoot == "" {
		return nil, nil, nil
	}
	configFiles, err := filepath.Glob(filepath.Join(liveRoot, "hysteria2", "*.yaml"))
	if err != nil {
		return nil, nil, fmt.Errorf("cert-sync: scan hysteria2 configs: %w", err)
	}
	targets := hysteria2CertSyncTargets(configFiles)
	if len(targets) == 0 {
		return nil, nil, nil
	}
	fence, release, err := s.acquireRuntimeFence("cert-sync")
	if err != nil {
		return nil, nil, fmt.Errorf("cert-sync: acquire fence: %w", err)
	}
	if release != nil {
		defer release()
	}
	certDir := filepath.Join(filepath.Dir(liveRoot), "certs")
	outcomes := make(map[string]certSyncOutcome, len(targets))
	var errs []error
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return outcomes, targets, errors.Join(append(errs, err)...)
		}
		result, err := privilegedBackend.SyncCaddyCert(ctx, privileged.SyncCaddyCertRequest{
			Domain: target.Domain,
			OutDir: certDir,
			Fence:  fence,
		})
		if err != nil {
			outcomes[target.Domain] = certSyncErrored
			errs = append(errs, fmt.Errorf("sync caddy cert %s: %w", target.Domain, err))
			continue
		}
		if !result.Found {
			// ACME material is still absent — the helper keeps or seeds the
			// self-signed fallback; the domain stays pending for retry (#1168).
			outcomes[target.Domain] = certSyncMissing
		} else {
			outcomes[target.Domain] = certSyncFound
		}
		// Not yet issued is a steady state between config apply and ACME
		// completion — the apply path already reported it; keep polling.
		// Restart only when the served bytes actually changed: a renewed ACME
		// pair or a freshly seeded fallback (#1103/#1168).
		if !result.Changed {
			continue
		}
		if result.Fallback {
			log.Printf("cert-sync: seeded self-signed fallback for %s; restarting %s", target.Domain, target.Unit)
		} else {
			log.Printf("cert-sync: renewed certificate for %s; restarting %s", target.Domain, target.Unit)
		}
		if err := privilegedBackend.ServiceAction(ctx, privileged.ServiceActionRequest{
			Unit: target.Unit, Action: privileged.ServiceActionRestart, Fence: fence,
		}); err != nil {
			errs = append(errs, fmt.Errorf("restart %s after cert renewal: %w", target.Unit, err))
		}
	}
	return outcomes, targets, errors.Join(errs...)
}

// signalPendingCertSync hands apply-pending ACME domains to the cert-sync
// worker for bounded retries. A nil worker (tests, local-only mode) is fine —
// the hourly pass or the next apply still converges eventually (#1168).
func (s *managementState) signalPendingCertSync(domains []string) {
	if len(domains) == 0 {
		return
	}
	if s.certSyncWorker != nil {
		s.certSyncWorker.SignalPending(domains)
	}
}
