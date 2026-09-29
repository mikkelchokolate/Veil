package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
	"github.com/mikkelchokolate/Veil/internal/caddyassembly"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// This file carries the panel's direct-mode IP certificate lifecycle
// (#1169/#1170): the shared request builder used by the apply-time
// best-effort issuance and the in-daemon renewal worker, plus the worker
// itself. Issuance goes through the privileged helper's issue_ip_cert
// operation so the panel never runs acme.sh in-process.

// ipCertRenewalInterval polls the panel certificate roughly four times a
// day. The "shortlived" profile yields ~6-day certificates and the renewal
// gate opens 72h before expiry, so a broken renewal has many retry windows
// before the certificate actually lapses (#1170).
const ipCertRenewalInterval = 6 * time.Hour

// Test seams: the renewal gate, public-IP resolution, environment access,
// managed-cert SAN extraction, the render-plan check and the clock are
// overridable so apply/worker tests never touch the network, the filesystem
// or a real ACME directory.
var (
	ipCertNeedsRenewal     = acmeip.NeedsRenewal
	ipCertResolvePublicIPs = acmeip.ResolvePublicIPs
	ipCertGetenv           = os.Getenv
	ipCertManagedIPSANs    = acmeip.ManagedCertIPIdentities
	ipCertCaddyFronted     = caddyassembly.PanelIPCertCaddyFronted
	ipCertNow              = time.Now
)

// panelIPCertPaths resolves the certificate/key paths the privileged helper
// is allowed to issue into. VEIL_TLS_CERT / VEIL_TLS_KEY overrides are
// honored only while they stay inside <etc>/panel — an override pointing
// elsewhere is operator-managed material the helper's allowlist must never
// touch, so issuance is skipped entirely (ok=false) rather than attempted
// and rejected.
func panelIPCertPaths() (certPath, keyPath string, ok bool) {
	dir := filepath.Join(hostenv.EtcDir(), "panel")
	certPath = filepath.Join(dir, "tls.crt")
	keyPath = filepath.Join(dir, "tls.key")
	if v := strings.TrimSpace(os.Getenv("VEIL_TLS_CERT")); v != "" {
		certPath = v
	}
	if v := strings.TrimSpace(os.Getenv("VEIL_TLS_KEY")); v != "" {
		keyPath = v
	}
	allowed, err := filepath.Abs(dir)
	if err != nil {
		return "", "", false
	}
	for _, p := range []string{certPath, keyPath} {
		abs, err := filepath.Abs(p)
		if err != nil || filepath.Dir(abs) != allowed {
			return "", "", false
		}
	}
	return certPath, keyPath, true
}

// ipCertEmail picks the ACME account contact for the panel certificate:
// the panel-specific email first, then the legacy global email that is
// reserved for the panel's own identity (audit #305).
func ipCertEmail(settings Settings) string {
	if v := strings.TrimSpace(settings.PanelEmail); v != "" {
		return v
	}
	return strings.TrimSpace(settings.Email)
}

// panelIPCertOptedOut reports whether the install explicitly disabled the
// LE IP certificate (--le-ip-cert=false persists VEIL_PANEL_LE_IP_CERT=0
// into veil.env, which veil.service loads via EnvironmentFile). An opted-out
// direct panel must be skipped silently by every renewal entry point — no
// warning, no ACME command (#1187). Unset or unparsable values keep the
// enabled-by-default contract.
func panelIPCertOptedOut() bool {
	v := strings.TrimSpace(ipCertGetenv("VEIL_PANEL_LE_IP_CERT"))
	if v == "" {
		return false
	}
	if parsed, err := strconv.ParseBool(v); err == nil {
		return !parsed
	}
	return false
}

// ipCertACMEConfig reads the controlled-CA knobs from the process
// environment. veil.service loads veil.env via EnvironmentFile, so values
// persisted at install time are visible here exactly as they are to the
// renderers (material_acme_ca_test.go pins that contract); VEIL_ACME_CA_ROOT
// additionally becomes acme.sh's --ca-bundle (#1189).
func ipCertACMEConfig() (caServer string, insecure bool, caRoot string) {
	return strings.TrimSpace(ipCertGetenv("VEIL_ACME_CA_URL")),
		strings.TrimSpace(ipCertGetenv("VEIL_ACME_INSECURE")) != "",
		strings.TrimSpace(ipCertGetenv("VEIL_ACME_CA_ROOT"))
}

// ipCertHTTP01Port returns the persisted --le-ip-cert-port
// (VEIL_PANEL_HTTP01_PORT); 0 lets the helper default to 80. A malformed
// persisted value fails the request loudly instead of silently rebinding
// :80.
func ipCertHTTP01Port() (int, error) {
	v := strings.TrimSpace(ipCertGetenv("VEIL_PANEL_HTTP01_PORT"))
	if v == "" {
		return 0, nil
	}
	port, err := strconv.Atoi(v)
	if err != nil || port < 0 || port > 65535 {
		return 0, fmt.Errorf("VEIL_PANEL_HTTP01_PORT %q is not a valid port", v)
	}
	return port, nil
}

// ipCertRequestPublicIPs resolves the public addresses the renewal must
// certify. The install-pinned VEIL_PANEL_PUBLIC_IP wins — an operator who
// passed --public-ip never wants renewal re-probing the detection endpoints
// and certifying a different egress identity (#1186). Older installs that
// predate persistence fall back to the IP SANs of the certificate they
// already issued (only when it classifies as ACME-managed — the self-signed
// fallback's interface IPs must never steer issuance), and only when neither
// source has an answer does renewal auto-detect.
func ipCertRequestPublicIPs(ctx context.Context, certPath string) (publicIPv4, publicIPv6 string, err error) {
	if spec := strings.TrimSpace(ipCertGetenv("VEIL_PANEL_PUBLIC_IP")); spec != "" {
		publicIPv4, publicIPv6, err = acmeip.ParsePublicIPSpec(spec)
		if err != nil {
			return "", "", fmt.Errorf("VEIL_PANEL_PUBLIC_IP: %w", err)
		}
		return publicIPv4, publicIPv6, nil
	}
	if sans := ipCertManagedIPSANs(certPath); len(sans) > 0 {
		for _, san := range sans {
			if ip := net.ParseIP(san); ip != nil {
				if ip.To4() != nil {
					publicIPv4 = san
				} else {
					publicIPv6 = san
				}
			}
		}
		if publicIPv4 != "" || publicIPv6 != "" {
			return publicIPv4, publicIPv6, nil
		}
	}
	return ipCertResolvePublicIPs(ctx, "auto")
}

// panelIPCertIssueRequest builds the privileged issue_ip_cert request. It
// resolves the public addresses from the persisted install-time identity
// (or the issued cert's SANs, or auto-detection as a last resort, #1186)
// so the certificate covers both families on dual-stack hosts (#665),
// defaulting cert/key to the helper's allowlisted panel directory.
// HTTP01ViaCaddy marks the request when the rendered plan keeps a Veil-owned
// Caddy listener on :80 — the -acme challenge server a hysteria2 domain
// forces — so the helper parks acme.sh on the internal port the rendered
// /.well-known/acme-challenge/ proxy route forwards to instead of failing
// on the busy public port (#1181). DeferPanelRestart is always set: the
// panel itself is the caller, so the acme.sh reloadcmd must restart
// veil.service on a transient timer instead of killing the process before
// the helper can answer.
func panelIPCertIssueRequest(ctx context.Context, settings Settings, inbounds []Inbound, certPath, keyPath string, fence privileged.FenceToken) (privileged.IssueIPCertRequest, error) {
	publicIPv4, publicIPv6, err := ipCertRequestPublicIPs(ctx, certPath)
	if err != nil {
		return privileged.IssueIPCertRequest{}, fmt.Errorf("detect public IP: %w", err)
	}
	httpPort, err := ipCertHTTP01Port()
	if err != nil {
		return privileged.IssueIPCertRequest{}, err
	}
	caServer, insecure, caRoot := ipCertACMEConfig()
	return privileged.IssueIPCertRequest{
		PublicIPv4:        publicIPv4,
		PublicIPv6:        publicIPv6,
		HTTPPort:          httpPort,
		Email:             ipCertEmail(settings),
		CertPath:          certPath,
		KeyPath:           keyPath,
		CAServer:          caServer,
		Insecure:          insecure,
		CARoot:            caRoot,
		HTTP01ViaCaddy:    ipCertCaddyFronted(settings, inbounds),
		DeferPanelRestart: true,
		Fence:             fence,
	}, nil
}

// issuePanelIPCert runs one gated issuance pass against an explicit
// settings/backend snapshot — the caller resolves those under whatever lock
// discipline it owns: the apply hook is invoked with s.mu already held (the
// "Locked" contract) while the renewal worker snapshots the fields and
// releases the mutex before the helper call so a multi-minute ACME run can
// never stall the API.
//
// The pass is a no-op (nil error) unless the effective panel access is
// "direct", the install did not opt out of the IP certificate
// (VEIL_PANEL_LE_IP_CERT=0, #1187), and the on-disk certificate is inside
// the shared renewal gate. When the supplied fence token is empty
// (worker/CLI entry points that are not inside a durable apply operation) a
// one-shot runtime fencing lease is minted and released around the helper
// call, matching every other privileged mutation.
func (s *managementState) issuePanelIPCert(ctx context.Context, settings Settings, inbounds []Inbound, backend privileged.Client, fence privileged.FenceToken) error {
	if settings.PanelAccess != "direct" || panelIPCertOptedOut() {
		return nil
	}
	certPath, keyPath, ok := panelIPCertPaths()
	if !ok {
		// Operator-managed TLS material outside <etc>/panel: nothing the
		// helper is allowed to renew.
		return nil
	}
	if !ipCertNeedsRenewal(certPath, ipCertNow()) {
		return nil
	}
	issuer, ok := backend.(privileged.IPCertIssuer)
	if !ok || issuer == nil {
		return errors.New("privileged IP certificate issuer is unavailable")
	}
	var release func()
	if fence.Owner == "" || fence.Generation == 0 {
		var err error
		fence, release, err = s.acquireRuntimeFence("ip-cert-issue")
		if err != nil {
			return fmt.Errorf("acquire fencing lease: %w", err)
		}
	}
	if release != nil {
		defer release()
	}
	request, err := panelIPCertIssueRequest(ctx, settings, inbounds, certPath, keyPath, fence)
	if err != nil {
		return err
	}
	_, err = issuer.IssueIPCert(ctx, request)
	return err
}

// PostServiceActionsLocked is the applyflow post-policy hook (#1169): in
// direct panel-access mode a successful apply best-effort renews the
// short-lived IP certificate so the panel never keeps serving the install's
// self-signed fallback or drifts into expiry. The workflow evaluates the
// returned action only for reporting — a failure is appended to
// ServiceActions but cannot roll the apply back. Caller holds s.mu (the
// Locked contract), so fields are read directly, never re-locked.
func (ctx ManagementApplyContext) PostServiceActionsLocked([]string) []ServiceActionResult {
	s := ctx.state
	if s == nil || s.settings.PanelAccess != "direct" || panelIPCertOptedOut() {
		return nil
	}
	certPath, _, ok := panelIPCertPaths()
	if !ok || !ipCertNeedsRenewal(certPath, ipCertNow()) {
		return nil
	}
	action := ServiceActionResult{
		Name:    "issue-ip-cert",
		Command: []string{"acme.sh", "--issue", "--standalone"},
	}
	if err := s.issuePanelIPCert(ctx.operationContext(), s.settings, s.inbounds, s.privileged, ctx.fenceToken()); err != nil {
		action.Error = err.Error()
		return []ServiceActionResult{action}
	}
	action.Success = true
	return []ServiceActionResult{action}
}

// ipCertRenewalWorker renews the panel's Let's Encrypt IP certificate from
// inside the daemon instead of relying on acme.sh's cron entry (#1170): the
// helper installs acme.sh with --no-cron for privileged issuance, so this
// worker is the renewal driver, and its cadence is visible in the logs. The
// lifecycle mirrors certSyncWorker — periodic ticks on the management
// lifecycle context, a wake channel for tests, and a fenced privileged call
// per pass.
type ipCertRenewalWorker struct {
	state    *managementState
	interval time.Duration
	cancel   context.CancelFunc
	done     chan struct{}
	wake     chan struct{}
}

func newIPCertRenewalWorker(state *managementState) *ipCertRenewalWorker {
	return &ipCertRenewalWorker{state: state, interval: ipCertRenewalInterval, done: make(chan struct{}), wake: make(chan struct{}, 1)}
}

func (w *ipCertRenewalWorker) Start() {
	if w == nil {
		return
	}
	ctx, cancel := context.WithCancel(w.state.lifecycleContext())
	w.cancel = cancel
	go func() {
		defer close(w.done)
		// First pass shortly after startup: the certificate may already be
		// inside the renewal window, or the panel may have been offline when
		// the previous window opened.
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
				log.Printf("ip-cert-renewal: %v", err)
			}
			wait = w.interval
		}
	}()
}

func (w *ipCertRenewalWorker) Stop() {
	if w == nil || w.cancel == nil {
		return
	}
	w.cancel()
	<-w.done
}

// Signal forces the next pass to run immediately (tests and post-apply
// nudges).
func (w *ipCertRenewalWorker) Signal() {
	if w == nil || w.wake == nil {
		return
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// SyncOnce performs one renewal pass. The settings/backend are snapshotted
// under s.mu and released before the helper call so a slow ACME run never
// blocks API reads; issuePanelIPCert applies the access-mode and renewal
// gates. The helper's reloadcmd restarts veil.service on a deferred
// transient timer (DeferPanelRestart), so a successful renewal reloads the
// certificate shortly after this call returns without depending on the
// worker goroutine still being alive.
func (w *ipCertRenewalWorker) SyncOnce(ctx context.Context) error {
	s := w.state
	if s == nil {
		return nil
	}
	s.mu.Lock()
	settings := s.settings
	inbounds := s.inbounds
	backend := s.privileged
	s.mu.Unlock()
	return s.issuePanelIPCert(ctx, settings, inbounds, backend, privileged.FenceToken{})
}
