package api

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// ipCertPrivilegedClient is a privileged.Client stub that also implements
// IPCertIssuer, recording the issuance request for assertions.
type ipCertPrivilegedClient struct {
	recordingPrivilegedClient
	issueCalls atomic.Int32
	issueErr   error
	gotRequest privileged.IssueIPCertRequest
}

func (c *ipCertPrivilegedClient) IssueIPCert(_ context.Context, request privileged.IssueIPCertRequest) (privileged.IssueIPCertResult, error) {
	c.issueCalls.Add(1)
	c.gotRequest = request
	return privileged.IssueIPCertResult{CertPath: request.CertPath, KeyPath: request.KeyPath}, c.issueErr
}

// swapIPCertSeams points the renewal gate / resolver / clock at test stubs
// and returns a restore function. The env seam defaults to empty so tests
// stay hermetic regardless of the developer shell's VEIL_* variables.
func swapIPCertSeams(t *testing.T, needs bool, err error) func() {
	t.Helper()
	oldNeeds, oldResolve, oldNow := ipCertNeedsRenewal, ipCertResolvePublicIPs, ipCertNow
	oldEnv, oldSANs, oldFronted := ipCertGetenv, ipCertManagedIPSANs, ipCertCaddyFronted
	ipCertNeedsRenewal = func(string, time.Time) bool { return needs }
	ipCertResolvePublicIPs = func(context.Context, string) (string, string, error) {
		return "203.0.113.9", "2001:db8::7", err
	}
	ipCertNow = func() time.Time { return time.Unix(1_700_000_000, 0) }
	ipCertGetenv = func(string) string { return "" }
	ipCertManagedIPSANs = func(string) []string { return nil }
	ipCertCaddyFronted = func(Settings, []Inbound) bool { return false }
	return func() {
		ipCertNeedsRenewal, ipCertResolvePublicIPs, ipCertNow = oldNeeds, oldResolve, oldNow
		ipCertGetenv, ipCertManagedIPSANs, ipCertCaddyFronted = oldEnv, oldSANs, oldFronted
	}
}

// stubIPCertEnv serves a fixed map through the getenv seam.
func stubIPCertEnv(t *testing.T, env map[string]string) {
	t.Helper()
	old := ipCertGetenv
	ipCertGetenv = func(key string) string { return env[key] }
	t.Cleanup(func() { ipCertGetenv = old })
}

func directIPCertState(backend privileged.Client) *managementState {
	return &managementState{
		settings:   Settings{PanelAccess: "direct", PanelEmail: "panel@example.com"},
		privileged: backend,
	}
}

func TestPostServiceActionsSkipsNonDirectAccess(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	for _, access := range []string{"", "local", "caddy"} {
		backend := &ipCertPrivilegedClient{}
		state := &managementState{settings: Settings{PanelAccess: access}, privileged: backend}
		if got := NewManagementApplyContext(state).PostServiceActionsLocked(nil); got != nil {
			t.Fatalf("panelAccess=%q produced actions: %+v", access, got)
		}
		if backend.issueCalls.Load() != 0 {
			t.Fatalf("panelAccess=%q invoked issuance", access)
		}
	}
}

func TestPostServiceActionsSkipsWhenCertHealthy(t *testing.T) {
	defer swapIPCertSeams(t, false, nil)()
	backend := &ipCertPrivilegedClient{}
	state := directIPCertState(backend)
	if got := NewManagementApplyContext(state).PostServiceActionsLocked(nil); got != nil {
		t.Fatalf("healthy certificate produced actions: %+v", got)
	}
	if backend.issueCalls.Load() != 0 {
		t.Fatal("issuance ran for a certificate outside the renewal window")
	}
}

func TestPostServiceActionsIssuesForDirectAccess(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	backend := &ipCertPrivilegedClient{}
	state := directIPCertState(backend)
	actions := NewManagementApplyContext(state).PostServiceActionsLocked(nil)
	if len(actions) != 1 || !actions[0].Success {
		t.Fatalf("actions = %+v, want one successful issuance action", actions)
	}
	if backend.issueCalls.Load() != 1 {
		t.Fatalf("issuer invoked %d times", backend.issueCalls.Load())
	}
	request := backend.gotRequest
	if !request.DeferPanelRestart {
		t.Fatal("panel self-issuance must defer the veil.service restart")
	}
	if request.PublicIPv4 != "203.0.113.9" || request.PublicIPv6 != "2001:db8::7" {
		t.Fatalf("dual-stack addresses not propagated: %+v", request)
	}
	if request.Email != "panel@example.com" {
		t.Fatalf("email = %q, want panel email", request.Email)
	}
	if request.CertPath == "" || request.KeyPath == "" {
		t.Fatalf("cert paths must resolve to the helper allowlist: %+v", request)
	}
}

func TestPostServiceActionsReportsFailureWithoutRollback(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	backend := &ipCertPrivilegedClient{issueErr: errors.New("helper refused: ACME unreachable")}
	state := directIPCertState(backend)
	actions := NewManagementApplyContext(state).PostServiceActionsLocked(nil)
	if len(actions) != 1 || actions[0].Success || actions[0].Error == "" {
		t.Fatalf("issuance failure must surface as a failed action, got %+v", actions)
	}
	if actions[0].Error != "helper refused: ACME unreachable" {
		t.Fatalf("error text dropped: %q", actions[0].Error)
	}
}

func TestPostServiceActionsReportsMissingIssuer(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	// A Client that does not implement IPCertIssuer.
	backend := &recordingPrivilegedClient{}
	state := directIPCertState(backend)
	actions := NewManagementApplyContext(state).PostServiceActionsLocked(nil)
	if len(actions) != 1 || actions[0].Success || actions[0].Error == "" {
		t.Fatalf("missing issuer must produce a failed action, got %+v", actions)
	}
}

func TestPostServiceActionsSkipsResolverFailureAsFailedAction(t *testing.T) {
	defer swapIPCertSeams(t, true, errors.New("no public IP"))()
	backend := &ipCertPrivilegedClient{}
	state := directIPCertState(backend)
	actions := NewManagementApplyContext(state).PostServiceActionsLocked(nil)
	if len(actions) != 1 || actions[0].Success {
		t.Fatalf("resolution failure must produce a failed action, got %+v", actions)
	}
	if backend.issueCalls.Load() != 0 {
		t.Fatal("issuer must not run when public IP resolution failed")
	}
}

func TestIPCertWorkerSyncOnceGates(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()

	// Non-direct: never calls the issuer.
	backend := &ipCertPrivilegedClient{}
	worker := newIPCertRenewalWorker(&managementState{
		settings:   Settings{PanelAccess: "local"},
		privileged: backend,
	})
	if err := worker.SyncOnce(context.Background()); err != nil {
		t.Fatalf("non-direct SyncOnce: %v", err)
	}
	if backend.issueCalls.Load() != 0 {
		t.Fatal("worker issued a certificate for non-direct panel access")
	}
}

func TestIPCertWorkerIssuesWhenRenewalDue(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	backend := &ipCertPrivilegedClient{}
	worker := newIPCertRenewalWorker(directIPCertState(backend))
	if err := worker.SyncOnce(context.Background()); err != nil {
		t.Fatalf("SyncOnce: %v", err)
	}
	if backend.issueCalls.Load() != 1 {
		t.Fatalf("issuer invoked %d times", backend.issueCalls.Load())
	}
	if !backend.gotRequest.DeferPanelRestart {
		t.Fatal("worker issuance must defer the panel restart — the panel is the caller")
	}
}

func TestIPCertWorkerPropagatesIssuerError(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	backend := &ipCertPrivilegedClient{issueErr: errors.New("boom")}
	worker := newIPCertRenewalWorker(directIPCertState(backend))
	if err := worker.SyncOnce(context.Background()); err == nil || err.Error() != "boom" {
		t.Fatalf("issuer error must propagate for the retry loop, got %v", err)
	}
}

func TestIPCertWorkerSignalTriggersPass(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	backend := &ipCertPrivilegedClient{}
	worker := newIPCertRenewalWorker(directIPCertState(backend))
	worker.interval = time.Hour // keep the periodic tick out of the test
	done := make(chan struct{})
	go func() {
		worker.Start()
		close(done)
	}()
	// Wait for the goroutine to park, then signal a pass.
	deadline := time.Now().Add(2 * time.Second)
	for backend.issueCalls.Load() == 0 && time.Now().Before(deadline) {
		worker.Signal()
		time.Sleep(5 * time.Millisecond)
	}
	worker.Stop()
	<-done
	if backend.issueCalls.Load() == 0 {
		t.Fatal("signalled pass never ran the issuer")
	}
}

func TestIPCertWorkerStopBeforeStartIsNoop(t *testing.T) {
	var worker *ipCertRenewalWorker
	worker.Stop()   // nil receiver
	worker.Signal() // nil receiver
	w := newIPCertRenewalWorker(directIPCertState(&ipCertPrivilegedClient{}))
	w.Stop() // never started — must not block
}

func TestIPCertRequestUsesDualStackResolution(t *testing.T) {
	old := ipCertResolvePublicIPs
	defer func() { ipCertResolvePublicIPs = old }()
	ipCertResolvePublicIPs = func(_ context.Context, value string) (string, string, error) {
		if value != "auto" {
			t.Fatalf("worker/apply must auto-detect, got %q", value)
		}
		return "192.0.2.1", "", nil
	}
	request, err := panelIPCertIssueRequest(context.Background(), Settings{Email: "ops@example.com"}, nil, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key", privileged.FenceToken{Owner: "o", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if request.PublicIPv4 != "192.0.2.1" || request.PublicIPv6 != "" {
		t.Fatalf("single-stack resolution mangled: %+v", request)
	}
	if request.Email != "ops@example.com" {
		t.Fatalf("email fallback failed: %+v", request)
	}
	if request.Fence.Generation != 1 {
		t.Fatal("fence token not propagated into the request")
	}
}

// Issue #1187: --le-ip-cert=false persists VEIL_PANEL_LE_IP_CERT=0, and
// every renewal entry point must then skip the gate entirely — no resolver
// call, no issuance attempt.
func TestPostServiceActionsSkipsOptedOutPanel(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	stubIPCertEnv(t, map[string]string{"VEIL_PANEL_LE_IP_CERT": "0"})
	backend := &ipCertPrivilegedClient{}
	state := directIPCertState(backend)
	if got := NewManagementApplyContext(state).PostServiceActionsLocked(nil); got != nil {
		t.Fatalf("opted-out panel produced actions: %+v", got)
	}
	if backend.issueCalls.Load() != 0 {
		t.Fatal("opted-out panel must never invoke issuance")
	}
}

func TestIPCertWorkerSkipsOptedOutPanel(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	stubIPCertEnv(t, map[string]string{"VEIL_PANEL_LE_IP_CERT": "false"})
	backend := &ipCertPrivilegedClient{}
	worker := newIPCertRenewalWorker(directIPCertState(backend))
	if err := worker.SyncOnce(context.Background()); err != nil {
		t.Fatalf("opted-out SyncOnce must be a silent no-op, got %v", err)
	}
	if backend.issueCalls.Load() != 0 {
		t.Fatal("worker issued a certificate for an opted-out panel")
	}
}

// Issue #1186: the install-pinned public IP wins and the detection
// endpoints are never probed during renewal.
func TestIPCertRequestUsesPersistedPublicIP(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	stubIPCertEnv(t, map[string]string{"VEIL_PANEL_PUBLIC_IP": "203.0.113.9,2001:db8::7"})
	probed := false
	old := ipCertResolvePublicIPs
	defer func() { ipCertResolvePublicIPs = old }()
	ipCertResolvePublicIPs = func(context.Context, string) (string, string, error) {
		probed = true
		return "", "", errors.New("detection endpoints must not be probed")
	}
	request, err := panelIPCertIssueRequest(context.Background(), Settings{}, nil, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key", privileged.FenceToken{})
	if err != nil {
		t.Fatal(err)
	}
	if probed {
		t.Fatal("renewal probed the detection endpoints despite the persisted IP")
	}
	if request.PublicIPv4 != "203.0.113.9" || request.PublicIPv6 != "2001:db8::7" {
		t.Fatalf("persisted dual-stack addresses not propagated: %+v", request)
	}
}

// Issue #1186 (upgrade path): installs that predate VEIL_PANEL_PUBLIC_IP
// still avoid the probe by reusing the IP SANs of the certificate they
// already issued — the renewal renews the same identity.
func TestIPCertRequestReusesIssuedCertSANs(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	probed := false
	oldResolve, oldSANs := ipCertResolvePublicIPs, ipCertManagedIPSANs
	defer func() { ipCertResolvePublicIPs, ipCertManagedIPSANs = oldResolve, oldSANs }()
	ipCertResolvePublicIPs = func(context.Context, string) (string, string, error) {
		probed = true
		return "", "", errors.New("detection endpoints must not be probed")
	}
	ipCertManagedIPSANs = func(path string) []string {
		if path != "/etc/veil/panel/tls.crt" {
			t.Fatalf("SAN helper read %q, want the panel cert path", path)
		}
		return []string{"203.0.113.10", "2001:db8::9"}
	}
	request, err := panelIPCertIssueRequest(context.Background(), Settings{}, nil, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key", privileged.FenceToken{})
	if err != nil {
		t.Fatal(err)
	}
	if probed {
		t.Fatal("renewal probed despite reusable IP SANs on the issued cert")
	}
	if request.PublicIPv4 != "203.0.113.10" || request.PublicIPv6 != "2001:db8::9" {
		t.Fatalf("issued cert SANs not reused: %+v", request)
	}
}

// The self-signed fallback's SANs (loopback/interface addresses) must never
// steer issuance — ManagedCertIPIdentities already filters them, and the
// seam returns nil so the resolver path is exercised.
func TestIPCertRequestFallsBackToResolver(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	request, err := panelIPCertIssueRequest(context.Background(), Settings{}, nil, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key", privileged.FenceToken{})
	if err != nil {
		t.Fatal(err)
	}
	if request.PublicIPv4 != "203.0.113.9" || request.PublicIPv6 != "2001:db8::7" {
		t.Fatalf("resolver result not propagated: %+v", request)
	}
}

// Issue #1189: the controlled-CA knobs persisted in veil.env reach the
// privileged issuance request — CA URL, insecure flag, and the root bundle
// that becomes acme.sh's --ca-bundle.
func TestIPCertRequestPropagatesControlledCAConfig(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	stubIPCertEnv(t, map[string]string{
		"VEIL_ACME_CA_URL":   "https://127.0.0.1:14000/dir",
		"VEIL_ACME_INSECURE": "1",
		"VEIL_ACME_CA_ROOT":  "/etc/veil/acme-root.pem",
	})
	request, err := panelIPCertIssueRequest(context.Background(), Settings{}, nil, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key", privileged.FenceToken{})
	if err != nil {
		t.Fatal(err)
	}
	if request.CAServer != "https://127.0.0.1:14000/dir" {
		t.Fatalf("controlled CA URL dropped: %q", request.CAServer)
	}
	if !request.Insecure {
		t.Fatal("VEIL_ACME_INSECURE did not reach the request")
	}
	if request.CARoot != "/etc/veil/acme-root.pem" {
		t.Fatalf("VEIL_ACME_CA_ROOT dropped: %q", request.CARoot)
	}
}

// Issue #1181: when the rendered plan keeps a Veil-owned Caddy listener on
// :80, the request asks the helper to park acme.sh on the internal port
// that the challenge proxy route forwards to.
func TestIPCertRequestMarksCaddyFrontedIssuance(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	old := ipCertCaddyFronted
	ipCertCaddyFronted = func(Settings, []Inbound) bool { return true }
	defer func() { ipCertCaddyFronted = old }()
	request, err := panelIPCertIssueRequest(context.Background(), Settings{}, nil, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key", privileged.FenceToken{})
	if err != nil {
		t.Fatal(err)
	}
	if !request.HTTP01ViaCaddy {
		t.Fatal("Caddy-owned :80 was not marked on the issuance request")
	}
}

func TestIPCertHTTP01PortValidation(t *testing.T) {
	stubIPCertEnv(t, map[string]string{"VEIL_PANEL_HTTP01_PORT": "not-a-port"})
	if _, err := ipCertHTTP01Port(); err == nil {
		t.Fatal("malformed persisted port must fail loudly")
	}
	stubIPCertEnv(t, map[string]string{"VEIL_PANEL_HTTP01_PORT": "8080"})
	port, err := ipCertHTTP01Port()
	if err != nil || port != 8080 {
		t.Fatalf("persisted port = %d, %v; want 8080", port, err)
	}
}

// Compile-time guard: the issue_ip_cert request carries the fence through.

// Issue #1208: acme.sh installs with --no-cron, so the in-daemon renewal
// worker is the only renewal driver — it must start and run even when the
// SQLite subsystem never came up (degraded-DB mode). The fencing lease falls
// back to an ad-hoc veil.db handle and proceeds unfenced when none exists.
func TestInitClientSubsystemStartsIPCertWorkerWithoutDB(t *testing.T) {
	defer swapIPCertSeams(t, false, nil)()
	state := &managementState{settings: Settings{PanelAccess: "direct"}}
	initClientSubsystem(state)
	if state.ipCertRenewalWorker == nil {
		t.Fatal("renewal worker absent in degraded-DB mode")
	}
	state.ipCertRenewalWorker.Stop()
}

func TestIPCertWorkerSyncOnceWithDegradedDB(t *testing.T) {
	defer swapIPCertSeams(t, true, nil)()
	backend := &ipCertPrivilegedClient{}
	state := directIPCertState(backend)
	// statePath without a veil.db on disk: the lease store stays nil and the
	// issuance proceeds unfenced — degraded DB must not starve renewals.
	state.statePath = filepath.Join(t.TempDir(), "state.json")
	worker := newIPCertRenewalWorker(state)
	if err := worker.SyncOnce(context.Background()); err != nil {
		t.Fatalf("degraded-DB SyncOnce: %v", err)
	}
	if backend.issueCalls.Load() != 1 {
		t.Fatal("degraded-DB mode must still reach the issuer")
	}
	if backend.gotRequest.Fence.Owner != "" {
		t.Fatal("expected an unfenced request when no veil.db exists")
	}
}
