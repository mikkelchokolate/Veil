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
	s.hy2IPTracker = newHy2IPTracker(defaultHy2AuthIPTTL, nil)
	s.hy2SessionTracker = newHy2SessionTracker(defaultHy2AuthSessionTTL, nil)
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

func TestHy2AuthTrackerTTLExpiry(t *testing.T) {
	s, svc := newHy2AuthTestState(t)
	s.hy2AuthOnline = stubOnline(0)
	now := time.Now()
	s.hy2IPTracker = newHy2IPTracker(time.Minute, func() time.Time { return now })
	s.hy2SessionTracker = newHy2SessionTracker(time.Minute, func() time.Time { return now })
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
	// Both trackers age out together once the TTL lapses.
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
	s.hy2SessionTracker = newHy2SessionTracker(time.Minute, func() time.Time { return now })
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
