package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
)

func newSubscriptionRouterWithTrustedProxies(t *testing.T) (http.Handler, *managementState) {
	t.Helper()
	origValidator := stagedConfigValidator
	origRunner := serviceActionRunner
	origHealth := serviceHealthChecker
	origAutoApply := autoApplyAfterMutation
	origFirewall := currentFirewallApplier()
	t.Cleanup(func() {
		stagedConfigValidator = origValidator
		serviceActionRunner = origRunner
		serviceHealthChecker = origHealth
		autoApplyAfterMutation = origAutoApply
		swapFirewallApplier(origFirewall)
	})
	swapFirewallApplier(&fakeFirewallApplier{})
	stagedConfigValidator = func(paths []string) []ConfigValidationResult {
		out := make([]ConfigValidationResult, 0, len(paths))
		for _, p := range paths {
			out = append(out, ConfigValidationResult{Name: p, Config: p, Valid: true})
		}
		return out
	}
	serviceActionRunner = func(command []string) ServiceActionResult {
		return ServiceActionResult{Command: command, Success: true}
	}
	serviceHealthChecker = func(serviceName string) ServiceHealthResult {
		return ServiceHealthResult{Name: serviceName, Healthy: true}
	}
	autoApplyAfterMutation = true

	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := atomicfile.Write(statePath, []byte(`{"schemaVersion":4,"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","domain":"hy.example.com"}}`), 0o600, 0o700); err != nil {
		t.Fatalf("write state: %v", err)
	}
	router, reloader := newTestRouter(ServerInfo{
		Version:           "test",
		Mode:              "dev",
		StatePath:         statePath,
		ApplyRoot:         dir,
		TrustedProxyCIDRs: []string{"127.0.0.0/8", "::1/128"},
	})
	state, ok := reloader.(*managementState)
	if !ok {
		t.Fatalf("reloader is not *managementState: %T", reloader)
	}
	t.Cleanup(func() { _ = state.Close() })
	return router, state
}

func TestSubscriptionRateLimitUsesTrustedClientAddress(t *testing.T) {
	router, state := newSubscriptionRouterWithTrustedProxies(t)
	now := time.Now()
	for i := 0; i < 300; i++ {
		if !state.subscriptionLimiter.allow(fmt.Sprintf("prefill-%d", i), "127.0.0.1:1", now) {
			t.Fatalf("prefill %d rejected", i)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/s/dummy-token", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.10")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code == http.StatusTooManyRequests {
		t.Fatalf("trusted forwarded client shared the proxy peer bucket: %d %s", w.Code, w.Body.String())
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after independent source budget, got %d %s", w.Code, w.Body.String())
	}
}

func TestSubscriptionRateLimitIgnoresUntrustedForwardedFor(t *testing.T) {
	router, state := newSubscriptionRouterWithTrustedProxies(t)
	now := time.Now()
	for i := 0; i < 300; i++ {
		if !state.subscriptionLimiter.allow(fmt.Sprintf("prefill-%d", i), "198.51.100.20", now) {
			t.Fatalf("prefill %d rejected", i)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/s/dummy-token", nil)
	req.RemoteAddr = "198.51.100.20:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("untrusted X-Forwarded-For bypassed source limit: %d %s", w.Code, w.Body.String())
	}
}

func TestSubscriptionRateLimitDistinctForwardedClientsDoNotShareBucket(t *testing.T) {
	router, _ := newSubscriptionRouterWithTrustedProxies(t)
	for i := 0; i < 8; i++ {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/s/dummy-token-%d", i), nil)
		req.RemoteAddr = "127.0.0.1:40000"
		req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			t.Fatalf("independent forwarded client %d was rate limited", i)
		}
	}
}

func TestSubscriptionRateLimitSharedResolvedIPSharesBucket(t *testing.T) {
	router, state := newSubscriptionRouterWithTrustedProxies(t)
	now := time.Now()
	for i := 0; i < 300; i++ {
		if !state.subscriptionLimiter.allow(fmt.Sprintf("prefill-%d", i), "203.0.113.50", now) {
			t.Fatalf("prefill %d rejected", i)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/s/dummy-token", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("shared resolved IP did not share the source bucket: %d %s", w.Code, w.Body.String())
	}
}

func TestSubscriptionRateLimitIPv6Peer(t *testing.T) {
	limiter := subscriptionRateLimiter{}
	now := time.Now()
	if !limiter.allow("token-a", "[2001:db8::1]:443", now) {
		t.Fatal("first IPv6 request rejected")
	}
	req := httptest.NewRequest(http.MethodGet, "/s/dummy", nil)
	req.RemoteAddr = "[2001:db8::1]:9999"
	if got := clientIP(req); got != "2001:db8::1" {
		t.Fatalf("clientIP = %q", got)
	}
}
