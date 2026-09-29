package api

import (
	"context"
	"errors"
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
// and returns a restore function.
func swapIPCertSeams(t *testing.T, needs bool, err error) func() {
	t.Helper()
	oldNeeds, oldResolve, oldNow := ipCertNeedsRenewal, ipCertResolvePublicIPs, ipCertNow
	ipCertNeedsRenewal = func(string, time.Time) bool { return needs }
	ipCertResolvePublicIPs = func(context.Context, string) (string, string, error) {
		return "203.0.113.9", "2001:db8::7", err
	}
	ipCertNow = func() time.Time { return time.Unix(1_700_000_000, 0) }
	return func() {
		ipCertNeedsRenewal, ipCertResolvePublicIPs, ipCertNow = oldNeeds, oldResolve, oldNow
	}
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
	request, err := panelIPCertIssueRequest(context.Background(), Settings{Email: "ops@example.com"}, "/etc/veil/panel/tls.crt", "/etc/veil/panel/tls.key", privileged.FenceToken{Owner: "o", Generation: 1})
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

// Compile-time guard: the issue_ip_cert request carries the fence through.
