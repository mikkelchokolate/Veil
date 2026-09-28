package hysteria2

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// newStatsMuxServer serves /traffic and /online behind the stats secret and
// records whether each endpoint saw a correct Authorization header.
func newStatsMuxServer(t *testing.T, trafficBody string, onlineHandler http.HandlerFunc) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	onlineAuthed := &atomic.Bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "stats-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/traffic":
			_, _ = w.Write([]byte(trafficBody))
		case "/online":
			onlineAuthed.Store(true)
			onlineHandler(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server, onlineAuthed
}

// TestStatsProviderReadsOnlineTable: GET <listener>/online returns the
// runtime's live session map (identity -> connection count); identities fold
// onto bindings exactly like the traffic table and the same secret
// authenticates both endpoints.
func TestStatsProviderReadsOnlineTable(t *testing.T) {
	server, onlineAuthed := newStatsMuxServer(t,
		`{"alice":{"tx":1024,"rx":2048},"v_b":{"tx":1,"rx":1}}`,
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"alice":2,"v_b":3}`))
		})
	provider := NewStatsProvider("test", server.URL+"/traffic", map[string]string{
		"alice": "binding-1",
		"v_b":   "binding-2",
	})
	provider.secret = "stats-secret"

	batch, err := provider.Read()
	if err != nil {
		t.Fatal(err)
	}
	if !onlineAuthed.Load() {
		t.Fatal("/online was not requested with the stats secret")
	}
	if batch.Online == nil {
		t.Fatal("batch.Online is nil — the presence table read was skipped")
	}
	if batch.Online["binding-1"] != 2 || batch.Online["binding-2"] != 3 {
		t.Fatalf("online=%v, want binding-1=2 binding-2=3", batch.Online)
	}
	if len(batch.Readings) != 2 {
		t.Fatalf("readings=%+v — the online read must not disturb accounting", batch.Readings)
	}
}

// TestStatsProviderOnlineEmptyMapIsAuthoritative: {} is a proven "nobody is
// online" — a non-nil empty map, distinct from a failed read (nil).
func TestStatsProviderOnlineEmptyMapIsAuthoritative(t *testing.T) {
	server, _ := newStatsMuxServer(t,
		`{"alice":{"tx":10,"rx":20}}`,
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{}`))
		})
	provider := NewStatsProvider("test", server.URL+"/traffic", map[string]string{"alice": "binding-1"})
	provider.secret = "stats-secret"

	batch, err := provider.Read()
	if err != nil {
		t.Fatal(err)
	}
	if batch.Online == nil || len(batch.Online) != 0 {
		t.Fatalf("online=%v, want non-nil empty map (authoritative empty)", batch.Online)
	}
}

// TestStatsProviderOnlineMergesAliasedIdentities: leftover usernames and the
// migrated v_* identity can both hold sessions for one binding — the counts
// sum, like the traffic merge.
func TestStatsProviderOnlineMergesAliasedIdentities(t *testing.T) {
	server, _ := newStatsMuxServer(t,
		`{"legacy":{"tx":1,"rx":1},"v_b":{"tx":1,"rx":1}}`,
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"legacy":1,"v_b":2}`))
		})
	provider := NewStatsProvider("test", server.URL+"/traffic", map[string]string{
		"legacy": "binding-1",
		"v_b":    "binding-1",
	})
	provider.secret = "stats-secret"

	batch, err := provider.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Online) != 1 || batch.Online["binding-1"] != 3 {
		t.Fatalf("online=%v, want binding-1=3 merged", batch.Online)
	}
}

// TestStatsProviderOnlineUnknownIdentity: sessions for identities the panel
// does not serve are surfaced as unknown — never silently attributed.
func TestStatsProviderOnlineUnknownIdentity(t *testing.T) {
	server, _ := newStatsMuxServer(t,
		`{"alice":{"tx":1,"rx":1}}`,
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"alice":2,"ghost":9}`))
		})
	provider := NewStatsProvider("test", server.URL+"/traffic", map[string]string{"alice": "binding-1"})
	provider.secret = "stats-secret"

	batch, err := provider.Read()
	if err != nil {
		t.Fatal(err)
	}
	if batch.Online["binding-1"] != 2 {
		t.Fatalf("online=%v, want binding-1=2", batch.Online)
	}
	found := false
	for _, identity := range batch.UnknownIdentities {
		if identity == "ghost" {
			found = true
		}
	}
	if !found {
		t.Fatalf("unknown identities=%v, want ghost reported", batch.UnknownIdentities)
	}
}

// TestStatsProviderOnlineFailureKeepsAccounting: a 500 on /online must not
// fail the batch — traffic readings still land and Online stays nil so
// presence falls back to the activity heuristic.
func TestStatsProviderOnlineFailureKeepsAccounting(t *testing.T) {
	server, _ := newStatsMuxServer(t,
		`{"alice":{"tx":10,"rx":20}}`,
		func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		})
	provider := NewStatsProvider("test", server.URL+"/traffic", map[string]string{"alice": "binding-1"})
	provider.secret = "stats-secret"

	batch, err := provider.Read()
	if err != nil {
		t.Fatalf("read failed on a broken /online: %v", err)
	}
	if len(batch.Readings) != 1 || batch.Readings[0].UploadBytes != 10 {
		t.Fatalf("readings=%+v lost to the /online failure", batch.Readings)
	}
	if batch.Online != nil {
		t.Fatalf("online=%v, want nil after a failed read", batch.Online)
	}
}

// TestStatsProviderOnlineMalformedKeepsAccounting: a non-JSON /online body
// likewise degrades to nil Online without failing accounting.
func TestStatsProviderOnlineMalformedKeepsAccounting(t *testing.T) {
	server, _ := newStatsMuxServer(t,
		`{"alice":{"tx":10,"rx":20}}`,
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`not-json`))
		})
	provider := NewStatsProvider("test", server.URL+"/traffic", map[string]string{"alice": "binding-1"})
	provider.secret = "stats-secret"

	batch, err := provider.Read()
	if err != nil {
		t.Fatalf("read failed on malformed /online: %v", err)
	}
	if batch.Online != nil {
		t.Fatalf("online=%v, want nil after a malformed read", batch.Online)
	}
	if len(batch.Readings) != 1 {
		t.Fatalf("readings=%+v lost to the malformed /online", batch.Readings)
	}
}

// TestStatsProviderOnlineTimeoutKeepsAccounting: when /online stalls past
// the read deadline the batch still returns with the traffic readings and a
// nil Online table.
func TestStatsProviderOnlineTimeoutKeepsAccounting(t *testing.T) {
	release := make(chan struct{})
	server, _ := newStatsMuxServer(t,
		`{"alice":{"tx":10,"rx":20}}`,
		func(w http.ResponseWriter, _ *http.Request) {
			<-release
		})
	defer close(release)
	provider := NewStatsProvider("test", server.URL+"/traffic", map[string]string{"alice": "binding-1"})
	provider.secret = "stats-secret"

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	batch, err := provider.ReadContext(ctx)
	if err != nil {
		t.Fatalf("read failed on a stalled /online: %v", err)
	}
	if len(batch.Readings) != 1 {
		t.Fatalf("readings=%+v lost to the /online stall", batch.Readings)
	}
	if batch.Online != nil {
		t.Fatalf("online=%v, want nil after the read deadline passed", batch.Online)
	}
}

// TestOnlineStatsEndpointDerivesSiblingPath locks the endpoint derivation:
// whatever directory the rendered listener lives under, the sibling keeps
// scheme/host/port and only swaps the final path element for /online.
func TestOnlineStatsEndpointDerivesSiblingPath(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1:9999/traffic":     "http://127.0.0.1:9999/online",
		"http://127.0.0.1:9999/traffic/":    "http://127.0.0.1:9999/online",
		"http://127.0.0.1:9999/api/traffic": "http://127.0.0.1:9999/api/online",
		"http://127.0.0.1:9999":             "http://127.0.0.1:9999/online",
		"http://127.0.0.1:9999/traffic?x=1": "http://127.0.0.1:9999/online",
	}
	for input, want := range cases {
		if got := onlineStatsEndpoint(input); got != want {
			t.Errorf("onlineStatsEndpoint(%q)=%q, want %q", input, got, want)
		}
	}
}
