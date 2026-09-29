package privileged

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/acmeip"
)

// ipCertStub captures the IssueOptions a runIssueIPCert call produced so the
// allowlist/no-cron/deferred-restart contract can be asserted without acme.sh.
type ipCertStub struct {
	calls atomic.Int32
	got   acmeip.IssueOptions
	err   error
}

func (s *ipCertStub) issue(_ context.Context, opts acmeip.IssueOptions) (acmeip.IssuedCert, error) {
	s.calls.Add(1)
	s.got = opts
	return acmeip.IssuedCert{CertPath: opts.CertPath, KeyPath: opts.KeyPath}, s.err
}

func TestIssueIPCertOperationIsValidAndPayloadMatches(t *testing.T) {
	if !OperationIssueIPCert.Valid() {
		t.Fatal("issue_ip_cert must be a valid operation")
	}
	envelope := RequestEnvelope{Version: ProtocolVersion, RequestID: "x", Operation: OperationIssueIPCert}
	if err := envelope.Validate(); err == nil {
		t.Fatal("missing issueIpCert payload must be rejected")
	}
	envelope.IssueIPCert = &IssueIPCertRequest{PublicIPv4: "203.0.113.9"}
	if err := envelope.Validate(); err != nil {
		t.Fatalf("valid issue_ip_cert envelope rejected: %v", err)
	}
	// A mismatched payload must not satisfy the operation.
	mismatch := RequestEnvelope{Version: ProtocolVersion, RequestID: "x", Operation: OperationIssueIPCert, RestartPanel: &RestartPanelRequest{}}
	if err := mismatch.Validate(); err == nil {
		t.Fatal("issue_ip_cert with a foreign payload must be rejected")
	}
}

func TestIssueIPCertRequestJSONRoundTrip(t *testing.T) {
	envelope := RequestEnvelope{
		Version:   ProtocolVersion,
		RequestID: "ip-cert",
		Operation: OperationIssueIPCert,
		IssueIPCert: &IssueIPCertRequest{
			PublicIPv4:        "203.0.113.9",
			PublicIPv6:        "2001:db8::7",
			HTTPPort:          80,
			Email:             "ops@example.com",
			CertPath:          "/etc/veil/panel/tls.crt",
			KeyPath:           "/etc/veil/panel/tls.key",
			CAServer:          "https://ca.example.test/dir",
			Insecure:          true,
			DeferPanelRestart: true,
			Fence:             FenceToken{Owner: "pid:1:x", Generation: 3, LeaseExpiresAt: 9999999999, OperationID: "op"},
		},
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRequest(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.IssueIPCert == nil || decoded.IssueIPCert.PublicIPv6 != "2001:db8::7" ||
		!decoded.IssueIPCert.DeferPanelRestart || decoded.IssueIPCert.Fence.Generation != 3 {
		t.Fatalf("issue_ip_cert payload lost in round-trip: %+v", decoded.IssueIPCert)
	}
}

func ipCertTestConfig(t *testing.T, stub *ipCertStub) ProductionConfig {
	t.Helper()
	root := t.TempDir()
	return ProductionConfig{
		IPCertDir:     filepath.Join(root, "panel"),
		IPCertHomeDir: filepath.Join(root, "acme"),
		IssueIPCert:   stub.issue,
	}
}

func TestRunIssueIPCertRejectsPathsOutsideAllowlist(t *testing.T) {
	stub := &ipCertStub{}
	config := ipCertTestConfig(t, stub)
	badPaths := []string{
		"../escape.crt",    // traversal
		"../../etc/passwd", // absolute traversal
		filepath.Join(config.IPCertDir, "..", "evil.crt"), // traversal that still resolves outside
		"/etc/passwd",  // arbitrary absolute path
		"relative.crt", // not absolute
		filepath.Join(config.IPCertDir, "sub", "x.crt"),    // subdirectory of allowlist
		filepath.Join(config.IPCertDir+"-evil", "tls.crt"), // sibling sharing the prefix
	}
	for _, bad := range badPaths {
		_, err := runIssueIPCert(context.Background(), IssueIPCertRequest{
			PublicIPv4: "203.0.113.9", CertPath: bad,
		}, config)
		var operationError *Error
		if err == nil || !errors.As(err, &operationError) || operationError.Code != ErrorForbiddenOperation {
			t.Fatalf("certPath %q: err = %v, want forbidden_operation", bad, err)
		}
	}
	if stub.calls.Load() != 0 {
		t.Fatalf("issuer ran %d times for rejected paths", stub.calls.Load())
	}
}

func TestRunIssueIPCertDefaultsAndOptionMapping(t *testing.T) {
	stub := &ipCertStub{}
	config := ipCertTestConfig(t, stub)
	result, err := runIssueIPCert(context.Background(), IssueIPCertRequest{
		PublicIPv4:        "203.0.113.9",
		PublicIPv6:        "2001:db8::7",
		Email:             "ops@example.com",
		CAServer:          "https://ca.example.test/dir",
		Insecure:          true,
		DeferPanelRestart: true,
	}, config)
	if err != nil {
		t.Fatalf("runIssueIPCert: %v", err)
	}
	wantCert := filepath.Join(config.IPCertDir, "tls.crt")
	wantKey := filepath.Join(config.IPCertDir, "tls.key")
	if result.CertPath != wantCert || result.KeyPath != wantKey {
		t.Fatalf("result paths = %q/%q, want %q/%q", result.CertPath, result.KeyPath, wantCert, wantKey)
	}
	got := stub.got
	if got.CertPath != wantCert || got.KeyPath != wantKey {
		t.Fatalf("cert paths not mapped into the allowlist dir: %+v", got)
	}
	if !got.NoCron {
		t.Fatal("helper issuance must disable acme.sh cron — Veil owns renewal (#1170)")
	}
	if got.HomeDir != config.IPCertHomeDir {
		t.Fatalf("acme.sh home = %q, want state-rooted %q", got.HomeDir, config.IPCertHomeDir)
	}
	if !got.DeferPanelRestart {
		t.Fatal("deferred panel restart flag not propagated")
	}
	if got.CAServer != "https://ca.example.test/dir" || !got.Insecure {
		t.Fatalf("controlled-CA settings not propagated: %+v", got)
	}
	if got.System != nil {
		t.Fatal("helper issuance must run on the root system implementation")
	}
}

func TestRunIssueIPCertRejectsInvalidAddressesAndPorts(t *testing.T) {
	stub := &ipCertStub{}
	config := ipCertTestConfig(t, stub)
	tests := []struct {
		name string
		req  IssueIPCertRequest
	}{
		{"no addresses", IssueIPCertRequest{}},
		{"ipv6 in v4 slot", IssueIPCertRequest{PublicIPv4: "2001:db8::1"}},
		{"ipv4 in v6 slot", IssueIPCertRequest{PublicIPv6: "203.0.113.9"}},
		{"garbage v4", IssueIPCertRequest{PublicIPv4: "not-an-ip"}},
		{"negative port", IssueIPCertRequest{PublicIPv4: "1.2.3.4", HTTPPort: -1}},
		{"port too high", IssueIPCertRequest{PublicIPv4: "1.2.3.4", HTTPPort: 70000}},
		{"ca with whitespace", IssueIPCertRequest{PublicIPv4: "1.2.3.4", CAServer: "bad ca"}},
		{"ca not a name or url", IssueIPCertRequest{PublicIPv4: "1.2.3.4", CAServer: "BAD_CA!"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runIssueIPCert(context.Background(), tc.req, config)
			var operationError *Error
			if err == nil || !errors.As(err, &operationError) || operationError.Code != ErrorInvalidRequest {
				t.Fatalf("err = %v, want invalid_request", err)
			}
		})
	}
	if stub.calls.Load() != 0 {
		t.Fatalf("issuer ran %d times for invalid requests", stub.calls.Load())
	}
}

func TestRunIssueIPCertIssuerErrorPropagates(t *testing.T) {
	stub := &ipCertStub{err: errors.New("ACME validation failed")}
	_, err := runIssueIPCert(context.Background(), IssueIPCertRequest{PublicIPv4: "203.0.113.9"}, ipCertTestConfig(t, stub))
	if err == nil || !strings.Contains(err.Error(), "ACME validation failed") {
		t.Fatalf("issuer error must propagate, got %v", err)
	}
}

func TestIssueIPCertRequiresFenceWhenPolicyDemands(t *testing.T) {
	policy := testPolicy(t)
	policy.PanelCertDir = filepath.Join(t.TempDir(), "panel")
	policy.FencePath = filepath.Join(t.TempDir(), "fence.json")
	policy.RequireFence = true
	var calls atomic.Int32
	adapter := NewLocalAdapter(policy, Executor{
		IssueIPCert: func(context.Context, IssueIPCertRequest) (IssueIPCertResult, error) {
			calls.Add(1)
			return IssueIPCertResult{}, nil
		},
	})
	_, err := adapter.IssueIPCert(context.Background(), IssueIPCertRequest{PublicIPv4: "203.0.113.9"})
	var operationError *Error
	if err == nil || !errors.As(err, &operationError) || operationError.Code != ErrorConflict {
		t.Fatalf("unfenced issuance must be rejected with conflict, got %v", err)
	}
	if calls.Load() != 0 {
		t.Fatal("executor ran without a fencing token")
	}
	// A valid token reaches the executor.
	_, err = adapter.IssueIPCert(context.Background(), IssueIPCertRequest{
		PublicIPv4: "203.0.113.9",
		Fence: FenceToken{
			Owner:          "pid:1:test",
			Generation:     1,
			LeaseExpiresAt: time.Now().Add(time.Hour).Unix(),
			OperationID:    "test-op",
		},
	})
	if err != nil {
		t.Fatalf("fenced issuance rejected: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("executor ran %d times, want 1", calls.Load())
	}
}

func TestIssueIPCertSocketRoundTrip(t *testing.T) {
	var gotIssue atomic.Int32
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{
		IssueIPCert: func(_ context.Context, request IssueIPCertRequest) (IssueIPCertResult, error) {
			gotIssue.Add(1)
			if request.PublicIPv4 != "203.0.113.9" || !request.DeferPanelRestart {
				t.Fatalf("request mangled in dispatch: %+v", request)
			}
			return IssueIPCertResult{CertPath: "/etc/veil/panel/tls.crt", KeyPath: "/etc/veil/panel/tls.key"}, nil
		},
	}))
	response := servePipeRequest(t, server, RequestEnvelope{
		Version:   ProtocolVersion,
		RequestID: "issue",
		Operation: OperationIssueIPCert,
		IssueIPCert: &IssueIPCertRequest{
			PublicIPv4:        "203.0.113.9",
			DeferPanelRestart: true,
		},
	})
	if !response.OK || response.Error != nil {
		t.Fatalf("unexpected response: %+v", response)
	}
	if gotIssue.Load() != 1 {
		t.Fatalf("executor invoked %d times", gotIssue.Load())
	}
}

func TestIssueIPCertServerRejectsMissingPayload(t *testing.T) {
	var calls atomic.Int32
	server := NewServer(NewLocalAdapter(testPolicy(t), Executor{
		IssueIPCert: func(context.Context, IssueIPCertRequest) (IssueIPCertResult, error) {
			calls.Add(1)
			return IssueIPCertResult{}, nil
		},
	}))
	response := servePipeRequest(t, server, RequestEnvelope{
		Version:   ProtocolVersion,
		RequestID: "bad",
		Operation: OperationIssueIPCert,
	})
	if response.OK || response.Error == nil || response.Error.Code != ErrorInvalidRequest {
		t.Fatalf("expected invalid_request, got %+v", response)
	}
	if calls.Load() != 0 {
		t.Fatal("missing payload reached the executor")
	}
}

func TestIssueIPCertClassifiesAsMutation(t *testing.T) {
	if got := operationBudget(OperationIssueIPCert, time.Second, time.Hour, 2*time.Hour); got != time.Hour {
		t.Fatalf("issue_ip_cert budget = %v, want the mutation budget", got)
	}
}

func TestDefaultProductionConfigPopulatesIPCert(t *testing.T) {
	policy := testPolicy(t)
	policy.PanelCertDir = filepath.Join(t.TempDir(), "panel")
	config := DefaultProductionConfig(policy, "test")
	if config.IPCertDir != policy.PanelCertDir {
		t.Fatalf("IPCertDir = %q, want %q", config.IPCertDir, policy.PanelCertDir)
	}
	if config.IPCertHomeDir != filepath.Join(policy.StateRoot, "acme") {
		t.Fatalf("IPCertHomeDir = %q", config.IPCertHomeDir)
	}
	executor := NewProductionExecutor(config)
	if executor.IssueIPCert == nil {
		t.Fatal("production executor must wire the IP certificate issuer")
	}
}
