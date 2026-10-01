package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/hysteria2"
)

// #1173: the internal Hysteria2 auth callback authenticates the same
// credential set the renderer would have emitted, then enforces
// deviceLimit/ipLimit at admission. Every deny is HTTP 200 + {"ok":false}
// (the daemon's explicit wire contract); transport failure is also a deny
// because the daemon fails closed on anything but 200/ok.

func newHy2AuthTestState(t *testing.T) (*managementState, *client.Service) {
	t.Helper()
	s, svc := newBindingTestState(t)
	s.hy2Limiter = newHy2AdmissionTracker(defaultHy2AuthSessionTTL, nil)
	return s, svc
}

func hy2AuthPath(s *managementState, inboundName string) string {
	for _, in := range s.inbounds {
		if in.Name == inboundName {
			return hysteria2.HTTPAuthPathPrefix + inboundName + "/" + hysteria2.HTTPAuthSecret(s.settings, in)
		}
	}
	return ""
}

func doHy2Auth(s *managementState, path string, req hy2AuthRequest) hy2AuthResponse {
	payload, _ := json.Marshal(req)
	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(payload)))
	s.handleHy2Auth(rec, httpReq)
	var out hy2AuthResponse
	_ = json.NewDecoder(rec.Body).Decode(&out)
	return out
}

func stubOnline(n int64) func(context.Context, model.Settings, model.Inbound, map[string]string) (map[string]int64, []string, error) {
	return func(ctx context.Context, settings model.Settings, inbound model.Inbound, identities map[string]string) (map[string]int64, []string, error) {
		counts := make(map[string]int64, len(identities))
		for _, bindingID := range identities {
			counts[bindingID] = n
		}
		return counts, nil, nil
	}
}

func intPtr(v int) *int { return &v }

func TestHy2AuthRejectsBadPathSecretAndMethod(t *testing.T) {
	s, _ := newHy2AuthTestState(t)
	path := hy2AuthPath(s, "hy2")
	if path == "" {
		t.Fatal("no auth path for inbound")
	}

	// Wrong per-inbound secret — the URL path IS the daemon credential.
	bad := path[:len(path)-4] + "dead"
	if got := doHy2Auth(s, bad, hy2AuthRequest{Addr: "1.2.3.4:1000", Auth: "u:p"}); got.OK {
		t.Fatal("bad secret admitted")
	}
	// Unknown inbound — fail closed.
	if got := doHy2Auth(s, hysteria2.HTTPAuthPathPrefix+"missing/"+strings.Repeat("a", 32), hy2AuthRequest{Addr: "1.2.3.4:1000", Auth: "u:p"}); got.OK {
		t.Fatal("unknown inbound admitted")
	}
	// Non-POST is a hard method failure.
	rec := httptest.NewRecorder()
	s.handleHy2Auth(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET status = %d, want 405", rec.Code)
	}
}

func TestHy2AuthAuthenticatesNormalizedCredential(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	s.hy2AuthOnline = stubOnline(0)
	view, err := svc.Create(client.Client{Name: "alice", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "client-secret"); err != nil {
		t.Fatal(err)
	}
	path := hy2AuthPath(s, "hy2")
	resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:5000", Auth: b.RuntimeIdentity + ":client-secret"})
	if !resp.OK || resp.ID != b.RuntimeIdentity {
		t.Fatalf("valid credential denied: %+v", resp)
	}
	// Username is case-insensitive upstream (splitUserPass lowercases).
	resp = doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:5001", Auth: strings.ToUpper(b.RuntimeIdentity) + ":client-secret"})
	if !resp.OK {
		t.Fatal("uppercase username denied")
	}
	// Wrong password and unknown user both deny.
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:5002", Auth: b.RuntimeIdentity + ":wrong"}); resp.OK {
		t.Fatal("wrong password admitted")
	}
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:5003", Auth: "nobody:client-secret"}); resp.OK {
		t.Fatal("unknown user admitted")
	}
	// Missing separator is malformed.
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:5004", Auth: "noseparator"}); resp.OK {
		t.Fatal("malformed auth payload admitted")
	}
}

func TestHy2AuthSharedPasswordInbound(t *testing.T) {
	s, _ := newHy2AuthTestState(t)
	s.hy2AuthOnline = stubOnline(0)
	path := hy2AuthPath(s, "hy2")
	want := hysteria2.SharedPassword(s.settings, s.inbounds[0])
	if want == "" {
		t.Fatal("no shared password derived")
	}
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:5000", Auth: want}); !resp.OK {
		t.Fatal("shared password denied")
	}
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:5000", Auth: "wrong"}); resp.OK {
		t.Fatal("wrong shared password admitted")
	}
}

func TestHy2AuthDeviceLimitEnforcedAcrossOnlineAndPending(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	s.hy2AuthOnline = stubOnline(0)
	view, err := svc.Create(client.Client{Name: "limited", Enabled: true, DeviceLimit: intPtr(1)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	path := hy2AuthPath(s, "hy2")
	auth := b.RuntimeIdentity + ":pw"
	// First admission registers a pending session; the second concurrent
	// request sees it even though /online has not caught up yet.
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:1000", Auth: auth}); !resp.OK {
		t.Fatal("first session denied")
	}
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "5.6.7.8:2000", Auth: auth}); resp.OK {
		t.Fatal("second session raced past deviceLimit=1 while /online lagged")
	}
	// Same tuple re-admits without consuming a second slot.
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:1000", Auth: auth}); !resp.OK {
		t.Fatal("re-auth for the same tuple denied")
	}
}

func TestHy2AuthDeviceLimitCountsOnlineSessions(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	view, err := svc.Create(client.Client{Name: "online-capped", Enabled: true, DeviceLimit: intPtr(2)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	path := hy2AuthPath(s, "hy2")
	auth := b.RuntimeIdentity + ":pw"
	s.hy2AuthOnline = stubOnline(2)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:1000", Auth: auth}); resp.OK {
		t.Fatal("admitted while online count already at deviceLimit")
	}
	s.hy2AuthOnline = stubOnline(1)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:1001", Auth: auth}); !resp.OK {
		t.Fatal("denied while online count below deviceLimit")
	}
}

func TestHy2AuthOnlineFailureFailsClosed(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	s.hy2AuthOnline = func(context.Context, model.Settings, model.Inbound, map[string]string) (map[string]int64, []string, error) {
		return nil, nil, errors.New("stats endpoint unreachable")
	}
	view, err := svc.Create(client.Client{Name: "closed", Enabled: true, DeviceLimit: intPtr(4)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	if resp := doHy2Auth(s, hy2AuthPath(s, "hy2"), hy2AuthRequest{Addr: "1.2.3.4:1000", Auth: b.RuntimeIdentity + ":pw"}); resp.OK {
		t.Fatal("admitted despite /online failure — enforcement must fail closed")
	}
}

func TestHy2AuthIPLimitDistinctAddresses(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	s.hy2AuthOnline = stubOnline(0)
	view, err := svc.Create(client.Client{Name: "ip-limited", Enabled: true, IPLimit: intPtr(1)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	path := hy2AuthPath(s, "hy2")
	auth := b.RuntimeIdentity + ":pw"
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:1000", Auth: auth}); !resp.OK {
		t.Fatal("first IP denied")
	}
	// Same IP, new source port — still one distinct IP.
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:9999", Auth: auth}); !resp.OK {
		t.Fatal("same IP re-admission denied")
	}
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "5.6.7.8:1000", Auth: auth}); resp.OK {
		t.Fatal("second distinct IP admitted past ipLimit=1")
	}
	// Unparseable addr cannot be accounted — fail closed.
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "garbage", Auth: auth}); resp.OK {
		t.Fatal("unparseable addr admitted")
	}
}

// Pending admissions that never reach the /online table expire at the
// session TTL and free their slots — the registration window is the ONLY
// wall-clock part of the tracker, and it must not kill registered sessions.
func TestHy2AuthTrackerTTLExpiry(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	s.hy2AuthOnline = stubOnline(0)
	now := time.Now()
	s.hy2Limiter = newHy2AdmissionTracker(time.Minute, func() time.Time { return now })
	view, err := svc.Create(client.Client{Name: "ttl", Enabled: true, DeviceLimit: intPtr(1), IPLimit: intPtr(1)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	path := hy2AuthPath(s, "hy2")
	auth := b.RuntimeIdentity + ":pw"
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:1000", Auth: auth}); !resp.OK {
		t.Fatal("first admission denied")
	}
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "5.6.7.8:2000", Auth: auth}); resp.OK {
		t.Fatal("admission inside TTL admitted past both limits")
	}
	// The first session never reached /online (count stays 0), so its
	// pending entries die at the TTL and the slot+IP free again.
	now = now.Add(2 * time.Minute)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "5.6.7.8:2000", Auth: auth}); !resp.OK {
		t.Fatal("expired tracker entries still counted")
	}
}

// Regression for the review finding on #1173: once an admitted session shows
// up in /online, its pending tracker entry keeps living for the TTL, so
// online+pending double-counted the overlap and under-admitted for
// deviceLimit >= 2. Entries carry the /online watermark seen at admission
// and stay pending until the count credibly absorbs them (or the TTL
// retires a dead admission).
func TestHy2AuthOnlinePendingOverlapDoesNotUnderAdmit(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	s.hy2AuthOnline = stubOnline(0)
	now := time.Now()
	s.hy2Limiter = newHy2AdmissionTracker(time.Minute, func() time.Time { return now })
	view, err := svc.Create(client.Client{Name: "overlap", Enabled: true, DeviceLimit: intPtr(2)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	path := hy2AuthPath(s, "hy2")
	auth := b.RuntimeIdentity + ":pw"
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:1000", Auth: auth}); !resp.OK {
		t.Fatal("first admission denied")
	}
	// Session A registers in /online (count moves above its watermark); its
	// pending entry stops counting — the second slot is genuinely free.
	s.hy2AuthOnline = stubOnline(1)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "5.6.7.8:2000", Auth: auth}); !resp.OK {
		t.Fatal("second admission denied by online∩pending double-count under deviceLimit=2")
	}
	// Third must still be denied: online=1 (A) + pending B = 2 — and it stays
	// denied no matter how long B's registration lags, because B's watermark
	// keeps it pending until /online absorbs it.
	now = now.Add(10 * time.Second)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "9.9.9.9:3000", Auth: auth}); resp.OK {
		t.Fatal("third admission raced past deviceLimit=2")
	}
	// B registers too: /online alone is at the limit.
	s.hy2AuthOnline = stubOnline(2)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "9.9.9.9:3001", Auth: auth}); resp.OK {
		t.Fatal("admission past deviceLimit=2 with two live sessions")
	}
	// If a pending session dies before ever registering, its entry frees the
	// slot when the TTL retires it rather than squatting forever.
	now = now.Add(2 * time.Minute)
	s.hy2AuthOnline = stubOnline(1)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "9.9.9.9:3002", Auth: auth}); !resp.OK {
		t.Fatal("admission denied by an expired dead-admission entry")
	}
}

func TestHy2AuthConcurrentAdmissionsCannotRacePastLimit(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	s.hy2AuthOnline = stubOnline(0)
	view, err := svc.Create(client.Client{Name: "racy", Enabled: true, DeviceLimit: intPtr(1)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	path := hy2AuthPath(s, "hy2")
	auth := b.RuntimeIdentity + ":pw"
	var wg sync.WaitGroup
	admitted := make(chan bool, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Every goroutine auths from a distinct source tuple.
			admitted <- doHy2Auth(s, path, hy2AuthRequest{
				Addr: fmt.Sprintf("1.2.3.4:%d", 40000+i),
				Auth: auth,
			}).OK
		}(i)
	}
	wg.Wait()
	close(admitted)
	count := 0
	for ok := range admitted {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("admitted %d sessions under deviceLimit=1", count)
	}
}

// A renamed or deleted inbound must not leave its derived path secret
// resident: the prune drops cache entries whose name left the inbound set.
func TestHy2AuthSecretCachePrunesRemovedInbounds(t *testing.T) {
	s, _ := newHy2AuthTestState(t)
	s.hy2AuthSecrets.Store("hy2", hy2AuthSecretEntry{password: "p", secret: "s"})
	s.hy2AuthSecrets.Store("gone", hy2AuthSecretEntry{password: "p", secret: "s"})
	s.pruneHy2AuthSecretsLocked()
	if _, ok := s.hy2AuthSecrets.Load("gone"); ok {
		t.Fatal("secret cache kept an entry for a removed inbound")
	}
	if _, ok := s.hy2AuthSecrets.Load("hy2"); !ok {
		t.Fatal("secret cache dropped a live inbound's entry")
	}
}

// #1180: ipLimit is a LIVE-session bound, not an admission-history window.
// The pre-fix tracker aged an IP out ~4 minutes after admission while the
// QUIC session stayed connected indefinitely, so a second IP was admitted
// while the first session was still live. Regression: a session that
// registered in /online must hold its IP for the session's whole lifetime,
// and release it only when the daemon's count actually drops.
func TestHy2AuthIPLimitDeniesSecondIPWhileSessionLive(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	now := time.Now()
	s.hy2Limiter = newHy2AdmissionTracker(defaultHy2AuthSessionTTL, func() time.Time { return now })
	view, err := svc.Create(client.Client{Name: "ip-live", Enabled: true, IPLimit: intPtr(1)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	path := hy2AuthPath(s, "hy2")
	auth := b.RuntimeIdentity + ":pw"
	s.hy2AuthOnline = stubOnline(0)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "198.51.100.10:1000", Auth: auth}); !resp.OK {
		t.Fatal("first session denied")
	}
	// Device A's session registers and stays connected indefinitely.
	s.hy2AuthOnline = stubOnline(1)
	// While A's pending record is still inside its TTL, the next reconcile —
	// here a denied attempt from a second IP — promotes it into a registered
	// slot because the count moved past A's admission watermark.
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "203.0.113.19:1999", Auth: auth}); resp.OK {
		t.Fatal("second IP admitted while A's session is live")
	}
	// Well past the old 4-minute admission window: device B must still be
	// denied because A's session is live in /online.
	now = now.Add(30 * time.Minute)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "203.0.113.20:2000", Auth: auth}); resp.OK {
		t.Fatal("second IP admitted while the first session is still live")
	}
	// Arbitrarily far out, a live session still holds its IP.
	now = now.Add(72 * time.Hour)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "203.0.113.21:2001", Auth: auth}); resp.OK {
		t.Fatal("second IP admitted while the first session is still live")
	}
	// The same IP may always re-admit — reconnects and extra ports from A's
	// address never consume a second IP slot.
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "198.51.100.10:40000", Auth: auth}); !resp.OK {
		t.Fatal("re-admission from the live IP denied")
	}
	// Only when the daemon's count really drops does the IP free again: the
	// registered record retires on the count drop, and the pending record
	// from the :40000 admission — which never made the table — expires at
	// the session TTL.
	s.hy2AuthOnline = stubOnline(0)
	now = now.Add(2 * time.Minute)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "203.0.113.20:2000", Auth: auth}); !resp.OK {
		t.Fatal("second IP still denied after the first session disconnected")
	}
}

// #1180: ipLimit enforcement needs the live /online table; when the stats
// listener is unreachable the limit cannot be proven, so admission fails
// closed exactly like deviceLimit.
func TestHy2AuthIPLimitStatsUnreachableFailsClosed(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	s.hy2AuthOnline = func(context.Context, model.Settings, model.Inbound, map[string]string) (map[string]int64, []string, error) {
		return nil, nil, errors.New("stats endpoint unreachable")
	}
	view, err := svc.Create(client.Client{Name: "ip-closed", Enabled: true, IPLimit: intPtr(4)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	if resp := doHy2Auth(s, hy2AuthPath(s, "hy2"), hy2AuthRequest{Addr: "1.2.3.4:1000", Auth: b.RuntimeIdentity + ":pw"}); resp.OK {
		t.Fatal("admitted despite /online failure — ipLimit must fail closed")
	}
}

// #1180 secondary defect: an attempt denied by ipLimit must not leave a
// deviceLimit pending slot behind. Both limits evaluate before either
// commits, so the foreign-IP deny writes nothing.
func TestHy2AuthIPLimitDenyLeavesNoDevicePending(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	s.hy2AuthOnline = stubOnline(0)
	view, err := svc.Create(client.Client{Name: "two-phase", Enabled: true, DeviceLimit: intPtr(2), IPLimit: intPtr(1)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	path := hy2AuthPath(s, "hy2")
	auth := b.RuntimeIdentity + ":pw"
	// The owner's first session registers in /online.
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:1000", Auth: auth}); !resp.OK {
		t.Fatal("first session denied")
	}
	s.hy2AuthOnline = stubOnline(1)
	// A foreign-IP attempt passes the device check but is denied by ipLimit.
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "5.6.7.8:2000", Auth: auth}); resp.OK {
		t.Fatal("foreign IP admitted past ipLimit=1")
	}
	// The owner's second session from the live IP must not be denied by the
	// phantom pending slot the denied attempt used to leave behind.
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:3000", Auth: auth}); !resp.OK {
		t.Fatal("owner's second session denied by a stale pending slot from the ipLimit deny")
	}
}

// #1180: sessions the daemon counts but the tracker never recorded — e.g.
// live connections surviving a panel restart — occupy unknown-IP slots, so
// new IPs deny fail-closed instead of assuming the unknown sessions are
// covered by a tracked address.
func TestHy2AuthIPLimitCountsUnaccountedOnlineSessions(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	s.hy2AuthOnline = stubOnline(0)
	view, err := svc.Create(client.Client{Name: "restart", Enabled: true, IPLimit: intPtr(1)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	path := hy2AuthPath(s, "hy2")
	auth := b.RuntimeIdentity + ":pw"
	// Simulate a panel restart: the daemon still counts a live session but
	// the tracker is empty.
	s.hy2AuthOnline = stubOnline(1)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:1000", Auth: auth}); resp.OK {
		t.Fatal("admitted while an unaccounted live session already occupies the IP limit")
	}
	// Once the unaccounted session disconnects, admissions proceed again.
	s.hy2AuthOnline = stubOnline(0)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:1000", Auth: auth}); !resp.OK {
		t.Fatal("admission denied after the unaccounted session disconnected")
	}
}

// #1180: a disconnected session's record must be retired when the /online
// count drops, freeing its IP slot for a genuinely new address — without
// this the tracker would squat forever on dead sessions.
func TestHy2AuthIPLimitFreesSlotAfterDisconnect(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	s.hy2AuthOnline = stubOnline(0)
	view, err := svc.Create(client.Client{Name: "churn", Enabled: true, IPLimit: intPtr(2)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	path := hy2AuthPath(s, "hy2")
	auth := b.RuntimeIdentity + ":pw"
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.1.1.1:1000", Auth: auth}); !resp.OK {
		t.Fatal("session A denied")
	}
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "2.2.2.2:2000", Auth: auth}); !resp.OK {
		t.Fatal("session B denied")
	}
	s.hy2AuthOnline = stubOnline(2)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "3.3.3.3:3000", Auth: auth}); resp.OK {
		t.Fatal("third IP admitted past ipLimit=2 with two live sessions")
	}
	// One session drops: whichever record the tracker retires, exactly one
	// IP slot frees — a third IP may now connect, a fourth may not.
	s.hy2AuthOnline = stubOnline(1)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "3.3.3.3:3000", Auth: auth}); !resp.OK {
		t.Fatal("third IP still denied after a session disconnected")
	}
	s.hy2AuthOnline = stubOnline(2)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "4.4.4.4:4000", Auth: auth}); resp.OK {
		t.Fatal("fourth IP admitted past ipLimit=2")
	}
}

// A dead pending admission must expire before promotion: with ipLimit >= 2,
// an expired record promoted ahead of a live pending peer would become
// registered — and registered records are never swept — pinning its IP
// forever. Admit A, then admit B half a TTL later while /online counts
// nothing; let only B reach the table and advance so A is expired but B is
// still live. Under the old promote-then-expire order A would win the single
// promotion slot (oldest admittedAt) and pin 1.1.1.1; the fixed order sweeps
// A, promotes B, and C takes the free IP slot — while A's stale IP can never
// come back.
func TestHy2AuthIPLimitDeadPendingCannotPinRegisteredSlot(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	now := time.Now()
	s.hy2Limiter = newHy2AdmissionTracker(time.Minute, func() time.Time { return now })
	s.hy2AuthOnline = stubOnline(0)
	view, err := svc.Create(client.Client{Name: "partial", Enabled: true, IPLimit: intPtr(2)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	path := hy2AuthPath(s, "hy2")
	auth := b.RuntimeIdentity + ":pw"

	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.1.1.1:1000", Auth: auth}); !resp.OK {
		t.Fatal("session A denied")
	}
	now = now.Add(30 * time.Second)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "2.2.2.2:2000", Auth: auth}); !resp.OK {
		t.Fatal("session B denied")
	}
	// Only B reaches the daemon table; jump so A (admitted at +0s) is past
	// its TTL while B (admitted at +30s) is still inside it.
	s.hy2AuthOnline = stubOnline(1)
	now = now.Add(45 * time.Second)
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "3.3.3.3:3000", Auth: auth}); !resp.OK {
		t.Fatal("third IP denied — a dead pending record pinned a registered slot")
	}
	// A's IP was swept, not promoted: a new tuple from it is a brand-new
	// address against a full ipLimit and must be denied.
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.1.1.1:5000", Auth: auth}); resp.OK {
		t.Fatal("stale pending IP admitted as if its dead record had registered")
	}
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "4.4.4.4:4000", Auth: auth}); resp.OK {
		t.Fatal("fourth IP admitted past ipLimit=2")
	}
}

// #1205: the memoized path secret is keyed by BOTH inputs the derivation
// consumes — a rotation of the per-install CredentialDerivationSecret must
// replace the cached entry and rotate the working URL, and the shared
// password stays part of the cache key.
func TestHy2AuthSecretCacheTracksInstallSecret(t *testing.T) {
	s, _ := newHy2AuthTestState(t)
	s.hy2AuthOnline = stubOnline(0)
	s.settings.CredentialDerivationSecret = "install-one"
	oldPath := hy2AuthPath(s, "hy2")
	want := hysteria2.SharedPassword(s.settings, s.inbounds[0])
	// First request populates s.hy2AuthSecrets for the inbound.
	if resp := doHy2Auth(s, oldPath, hy2AuthRequest{Addr: "1.2.3.4:1", Auth: want}); !resp.OK {
		t.Fatal("baseline auth denied")
	}

	s.settings.CredentialDerivationSecret = "install-two"
	newPath := hy2AuthPath(s, "hy2")
	if newPath == oldPath {
		t.Fatal("install-secret rotation did not rotate the path secret")
	}
	// The stale cached entry must not keep authorizing the old URL.
	if resp := doHy2Auth(s, oldPath, hy2AuthRequest{Addr: "1.2.3.4:2", Auth: want}); resp.OK {
		t.Fatal("stale path secret accepted after install-secret rotation")
	}
	if resp := doHy2Auth(s, newPath, hy2AuthRequest{Addr: "1.2.3.4:3", Auth: want}); !resp.OK {
		t.Fatal("rotated path secret denied")
	}

	// A shared-password change invalidates the cache entry the same way.
	s.inbounds[0].Password = "rotated-shared"
	rotatedPath := hy2AuthPath(s, "hy2")
	if rotatedPath == newPath {
		t.Fatal("shared-password rotation did not rotate the path secret")
	}
	wantRotated := hysteria2.SharedPassword(s.settings, s.inbounds[0])
	if resp := doHy2Auth(s, rotatedPath, hy2AuthRequest{Addr: "1.2.3.4:4", Auth: wantRotated}); !resp.OK {
		t.Fatal("password-rotated path secret denied")
	}
}

// #1206: migrated legacy profiles are suppressed from the merged credential
// table the callback authenticates against — the same view the renderer
// emits. The marker set is read outside s.mu, but the suppression itself is
// identical, so the migrated username must stop authenticating the moment
// the marker lands while the normalized binding's credential works.
func TestHy2AuthSuppressesMigratedLegacyCredential(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	s.hy2AuthOnline = stubOnline(0)
	s.inbounds[0].Profiles = []model.ClientProfile{{
		Name: "alice", Username: "alice", Password: "legacy-pass", Enabled: true,
	}}
	path := hy2AuthPath(s, "hy2")
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:10", Auth: "alice:legacy-pass"}); !resp.OK {
		t.Fatal("legacy profile credential denied before migration")
	}

	// The migration shape: a normalized client on the stable derived ID with
	// a binding to this inbound, plus the per-profile marker.
	view, err := svc.Create(client.Client{
		ID: client.StableClientID("hy2", "alice"), Name: "alice", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "normalized-pass"); err != nil {
		t.Fatal(err)
	}
	if err := s.clientRepo.PutMigrationMarker(client.MigrationMarker{
		Key:       client.LegacyProfileMarkerKey("hy2", "alice"),
		Version:   client.LegacyProfileMarkerVersion,
		AppliedAt: time.Now().Unix(),
	}); err != nil {
		t.Fatal(err)
	}

	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:11", Auth: "alice:legacy-pass"}); resp.OK {
		t.Fatal("migrated legacy credential still authenticates")
	}
	if resp := doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:12", Auth: b.RuntimeIdentity + ":normalized-pass"}); !resp.OK {
		t.Fatal("normalized binding credential denied after migration")
	}
}

// #1206: the /online stats read must NOT run under s.mu — it is remote I/O
// on the per-admission path, and holding the state mutex across it would
// stall every management operation behind a slow stats call.
func TestHy2AuthOnlineReadRunsOutsideStateMutex(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	inOnline := make(chan struct{}, 1)
	release := make(chan struct{})
	s.hy2AuthOnline = func(context.Context, model.Settings, model.Inbound, map[string]string) (map[string]int64, []string, error) {
		inOnline <- struct{}{}
		<-release
		return map[string]int64{}, nil, nil
	}
	view, err := svc.Create(client.Client{Name: "mu", Enabled: true, DeviceLimit: intPtr(4)})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	path := hy2AuthPath(s, "hy2")

	done := make(chan hy2AuthResponse, 1)
	go func() {
		done <- doHy2Auth(s, path, hy2AuthRequest{Addr: "1.2.3.4:1", Auth: b.RuntimeIdentity + ":pw"})
	}()
	select {
	case <-inOnline:
	case <-time.After(5 * time.Second):
		t.Fatal("admission never reached the online read")
	}
	// While the handler is blocked inside the stats read, s.mu must be free.
	locked := make(chan struct{})
	go func() {
		s.mu.Lock()
		s.mu.Unlock()
		close(locked)
	}()
	select {
	case <-locked:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("state mutex held across the /online read")
	}
	close(release)
	if resp := <-done; !resp.OK {
		t.Fatal("admission denied after the online read")
	}
}

// #1206: the in-flight bound denies overflow instead of queueing — each
// admission can run decryption plus a stats read, so unbounded concurrency
// is a remote-triggered amplification surface.
func TestHy2AuthInFlightBoundDeniesOverflow(t *testing.T) {
	s, _ := newHy2AuthTestState(t)
	s.hy2AuthOnline = stubOnline(0)
	s.hy2AuthInflight = make(chan struct{}, 1)
	s.hy2AuthInflight <- struct{}{} // occupy the only slot

	rec := httptest.NewRecorder()
	s.serveHy2Auth(rec, httptest.NewRequest(http.MethodPost, hy2AuthPath(s, "hy2"),
		strings.NewReader(`{"addr":"1.2.3.4:1","auth":"x:y"}`)))
	var denied hy2AuthResponse
	if err := json.NewDecoder(rec.Body).Decode(&denied); err != nil {
		t.Fatalf("decode deny body: %v", err)
	}
	if denied.OK {
		t.Fatal("over-cap admission was allowed")
	}
	<-s.hy2AuthInflight

	// With a slot free the request reaches the real handler — a valid
	// credential must still admit.
	svc := s.clientService
	view, err := svc.Create(client.Client{Name: "slot", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "pw"); err != nil {
		t.Fatal(err)
	}
	rec2 := httptest.NewRecorder()
	body, _ := json.Marshal(hy2AuthRequest{Addr: "1.2.3.4:2", Auth: b.RuntimeIdentity + ":pw"})
	s.serveHy2Auth(rec2, httptest.NewRequest(http.MethodPost, hy2AuthPath(s, "hy2"), strings.NewReader(string(body))))
	var admitted hy2AuthResponse
	if err := json.NewDecoder(rec2.Body).Decode(&admitted); err != nil {
		t.Fatalf("decode admit body: %v", err)
	}
	if !admitted.OK {
		t.Fatal("valid credential denied after the in-flight slot freed")
	}
}

// #1206: when the listener's Serve loop dies unexpectedly the state must
// rebind — a permanently dead listener fails every admission closed while
// the panel looks healthy. Closing the listener socket externally simulates
// the failure.
func TestHy2AuthListenerRebindsAfterServeFailure(t *testing.T) {
	s, _ := newHy2AuthTestState(t)
	s.hy2AuthListenAddr = "127.0.0.1:0"
	s.clientLifecycleMu.Lock()
	s.ensureHy2AuthLocked()
	s.clientLifecycleMu.Unlock()
	first := s.hy2Auth
	if first == nil {
		t.Fatal("listener did not bind")
	}
	defer func() {
		s.mu.Lock()
		detached := s.detachHy2AuthLocked()
		s.mu.Unlock()
		if detached != nil {
			_ = detached.server.Close()
		}
	}()

	// Kill the listener underneath Serve — the recovery path must detach the
	// dead server and bind a fresh listener.
	_ = first.listener.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s.mu.Lock()
		current := s.hy2Auth
		s.mu.Unlock()
		if current != nil && current != first {
			return // rebound
		}
		if time.Now().After(deadline) {
			t.Fatal("listener did not rebind after the Serve failure")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// #1206: tracker prune hooks — dropClient removes every record for a deleted
// client, dropInbound removes only the detached inbound's tuples, and the
// retain* forms mirror wholesale client/inbound replacement (rollback,
// restore, reload).
func TestHy2AdmissionTrackerPrunesRemovedState(t *testing.T) {
	tracker := newHy2AdmissionTracker(time.Minute, nil)
	ip, _ := netip.ParseAddr("203.0.113.7")
	ip2, _ := netip.ParseAddr("203.0.113.8")
	ip3, _ := netip.ParseAddr("203.0.113.9")
	admit := func(clientID, inbound, addr string, source netip.Addr) {
		t.Helper()
		ok, reason := tracker.admit(clientID, inbound+"\x00"+addr, source, 0, 4, 4)
		if !ok {
			t.Fatalf("admit %s/%s denied: %s", clientID, addr, reason)
		}
	}
	admit("client-a", "hy2", "203.0.113.7:1000", ip)
	admit("client-a", "other", "203.0.113.7:1001", ip)
	admit("client-b", "hy2", "203.0.113.8:2000", ip2)

	// Binding detach prunes ONLY that inbound's tuples for the client.
	tracker.dropInbound("client-a", "hy2")
	tracker.mu.Lock()
	if _, ok := tracker.sessions["client-a"]["hy2\x00203.0.113.7:1000"]; ok {
		t.Fatal("detached inbound tuple still tracked")
	}
	if _, ok := tracker.sessions["client-a"]["other\x00203.0.113.7:1001"]; !ok {
		t.Fatal("cross-inbound tuple dropped by detach prune")
	}
	if _, ok := tracker.sessions["client-b"]["hy2\x00203.0.113.8:2000"]; !ok {
		t.Fatal("unrelated client tuple dropped by detach prune")
	}
	tracker.mu.Unlock()

	// Client delete drops all of that client's records.
	tracker.dropClient("client-a")
	tracker.mu.Lock()
	if len(tracker.sessions["client-a"]) != 0 || len(tracker.pending["client-a"]) != 0 {
		t.Fatal("deleted client left admission records behind")
	}
	tracker.mu.Unlock()

	// Inbound-set replacement drops tuples for inbounds that no longer exist.
	tracker.retainInbounds(map[string]struct{}{"other": {}})
	tracker.mu.Lock()
	if _, ok := tracker.sessions["client-b"]; ok {
		t.Fatal("stale inbound tuple survived the inbound-set prune")
	}
	tracker.mu.Unlock()

	// Wholesale client replacement keeps only live client IDs.
	admit("client-c", "other", "203.0.113.9:3000", ip3)
	tracker.retainClients(map[string]struct{}{"client-c": {}})
	tracker.mu.Lock()
	if len(tracker.sessions) != 1 || len(tracker.pending) != 1 {
		t.Fatalf("retainClients kept stale clients: sessions=%v pending=%v", tracker.sessions, tracker.pending)
	}
	if _, ok := tracker.sessions["client-c"]["other\x00203.0.113.9:3000"]; !ok {
		t.Fatal("retainClients dropped a live client's session")
	}
	tracker.mu.Unlock()
}

// #1206: deleting a client through the API must drop its admission records —
// nothing reconciles a client that no longer exists, so its pending/session
// maps would pin memory for the process lifetime.
func TestHy2AuthClientDeletePrunesAdmissionState(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)

	inboundResponse := v1Request(t, router, http.MethodPost, "/api/inbounds",
		`{"name":"prune-hy","protocol":"hysteria2","transport":"udp","port":27460,"enabled":true}`)
	if inboundResponse.Code != http.StatusCreated && inboundResponse.Code != http.StatusOK {
		t.Fatalf("create inbound: %d %s", inboundResponse.Code, inboundResponse.Body.String())
	}
	clientResponse := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"prune-client","deviceLimit":2,"bindings":[{"inboundId":"prune-hy","credential":"pw"}]}`)
	if clientResponse.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", clientResponse.Code, clientResponse.Body.String())
	}
	clientID := unwrapClient(t, clientResponse.Body.Bytes())["id"].(string)

	if state.hy2Limiter == nil {
		t.Fatal("admission tracker not initialized")
	}
	ip, _ := netip.ParseAddr("203.0.113.10")
	if ok, reason := state.hy2Limiter.admit(clientID, "prune-hy\x00203.0.113.10:1000", ip, 0, 2, 2); !ok {
		t.Fatalf("seed admission denied: %s", reason)
	}

	del := v1Request(t, router, http.MethodDelete, "/api/v1/clients/"+clientID, "")
	if del.Code != http.StatusOK {
		t.Fatalf("delete client: %d %s", del.Code, del.Body.String())
	}
	state.hy2Limiter.mu.Lock()
	defer state.hy2Limiter.mu.Unlock()
	if len(state.hy2Limiter.pending[clientID]) != 0 || len(state.hy2Limiter.sessions[clientID]) != 0 {
		t.Fatal("deleted client left admission records behind")
	}
}

// #1206: detaching one binding prunes only that inbound's session tuples —
// the client's sessions on other bindings keep tracking.
func TestHy2AuthBindingDeletePrunesOnlyThatInbound(t *testing.T) {
	router, state := newApplyTrackedRouterWithState(t)

	for _, tc := range []struct {
		name string
		port int
	}{{"detach-a", 27461}, {"detach-b", 27462}} {
		resp := v1Request(t, router, http.MethodPost, "/api/inbounds",
			fmt.Sprintf(`{"name":%q,"protocol":"hysteria2","transport":"udp","port":%d,"enabled":true}`, tc.name, tc.port))
		if resp.Code != http.StatusCreated && resp.Code != http.StatusOK {
			t.Fatalf("create inbound %s: %d %s", tc.name, resp.Code, resp.Body.String())
		}
	}
	clientResponse := v1Request(t, router, http.MethodPost, "/api/v1/clients",
		`{"name":"detach-client","deviceLimit":4,"bindings":[{"inboundId":"detach-a","credential":"pw"},{"inboundId":"detach-b","credential":"pw"}]}`)
	if clientResponse.Code != http.StatusCreated {
		t.Fatalf("create client: %d %s", clientResponse.Code, clientResponse.Body.String())
	}
	created := unwrapClient(t, clientResponse.Body.Bytes())
	clientID := created["id"].(string)
	var bindingAID string
	for _, raw := range created["bindings"].([]any) {
		b := raw.(map[string]any)
		if b["inboundId"] == "detach-a" {
			bindingAID = b["id"].(string)
		}
	}
	if bindingAID == "" {
		t.Fatal("detach-a binding id missing from create response")
	}

	if state.hy2Limiter == nil {
		t.Fatal("admission tracker not initialized")
	}
	ip, _ := netip.ParseAddr("203.0.113.11")
	for _, inbound := range []string{"detach-a", "detach-b"} {
		if ok, reason := state.hy2Limiter.admit(clientID, inbound+"\x00203.0.113.11:1000", ip, 0, 4, 4); !ok {
			t.Fatalf("seed admission on %s denied: %s", inbound, reason)
		}
	}

	del := v1Request(t, router, http.MethodDelete, "/api/v1/clients/"+clientID+"/bindings/"+bindingAID, "")
	if del.Code != http.StatusOK {
		t.Fatalf("detach binding: %d %s", del.Code, del.Body.String())
	}
	state.hy2Limiter.mu.Lock()
	defer state.hy2Limiter.mu.Unlock()
	if _, ok := state.hy2Limiter.sessions[clientID]["detach-a\x00203.0.113.11:1000"]; ok {
		t.Fatal("detached inbound's session tuple still tracked")
	}
	if _, ok := state.hy2Limiter.sessions[clientID]["detach-b\x00203.0.113.11:1000"]; !ok {
		t.Fatal("binding detach pruned the other inbound's session tuple")
	}
}
