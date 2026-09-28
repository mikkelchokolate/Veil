package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
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
const certSyncInterval = time.Hour

type certSyncWorker struct {
	state    *managementState
	interval time.Duration
	cancel   context.CancelFunc
	done     chan struct{}
	wake     chan struct{}
}

func newCertSyncWorker(state *managementState) *certSyncWorker {
	return &certSyncWorker{state: state, interval: certSyncInterval, done: make(chan struct{}), wake: make(chan struct{}, 1)}
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
			if err := w.SyncOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("cert-sync: %v", err)
			}
			wait = w.interval
		}
	}()
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
	s := w.state
	if s == nil {
		return nil
	}
	s.mu.Lock()
	liveRoot := s.liveRoot
	privilegedBackend := s.privileged
	s.mu.Unlock()
	if privilegedBackend == nil || liveRoot == "" {
		return nil
	}
	configFiles, err := filepath.Glob(filepath.Join(liveRoot, "hysteria2", "*.yaml"))
	if err != nil {
		return fmt.Errorf("cert-sync: scan hysteria2 configs: %w", err)
	}
	targets := hysteria2CertSyncTargets(configFiles)
	if len(targets) == 0 {
		return nil
	}
	fence, release, err := s.acquireRuntimeFence("cert-sync")
	if err != nil {
		return fmt.Errorf("cert-sync: acquire fence: %w", err)
	}
	if release != nil {
		defer release()
	}
	certDir := filepath.Join(filepath.Dir(liveRoot), "certs")
	var errs []error
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(errs, err)...)
		}
		result, err := privilegedBackend.SyncCaddyCert(ctx, privileged.SyncCaddyCertRequest{
			Domain: target.Domain,
			OutDir: certDir,
			Fence:  fence,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("sync caddy cert %s: %w", target.Domain, err))
			continue
		}
		// Not yet issued is a steady state between config apply and ACME
		// completion — the apply path already reported it; keep polling.
		if !result.Found || !result.Changed {
			continue
		}
		log.Printf("cert-sync: renewed certificate for %s; restarting %s", target.Domain, target.Unit)
		if err := privilegedBackend.ServiceAction(ctx, privileged.ServiceActionRequest{
			Unit: target.Unit, Action: privileged.ServiceActionRestart, Fence: fence,
		}); err != nil {
			errs = append(errs, fmt.Errorf("restart %s after cert renewal: %w", target.Unit, err))
		}
	}
	return errors.Join(errs...)
}
