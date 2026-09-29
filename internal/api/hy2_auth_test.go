package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
