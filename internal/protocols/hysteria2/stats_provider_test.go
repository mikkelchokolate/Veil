package hysteria2

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

func TestStatsProviderRead(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "stats-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"alice":{"tx":1024,"rx":2048},"bob":{"tx":512,"rx":256}}`))
	}))
	defer server.Close()
	provider := NewStatsProvider("test", server.URL+"/traffic", map[string]string{"alice": "binding-1", "bob": "binding-2"})
	provider.secret = "stats-secret"
	readings, err := provider.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(readings.Readings) != 2 || readings.Readings[0].BindingID != "binding-1" || readings.Readings[0].UploadBytes != 1024 || readings.Readings[1].BindingID != "binding-2" || readings.Readings[1].DownloadBytes != 256 {
		t.Fatalf("unexpected readings: %+v", readings)
	}
}

func TestStatsProviderRejectsCounterOverflow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"alice":{"tx":18446744073709551615,"rx":1}}`))
	}))
	defer server.Close()
	provider := NewStatsProvider("test", server.URL+"/traffic", map[string]string{"alice": "binding-1"})
	if _, err := provider.Read(); err == nil {
		t.Fatal("accepted overflowing stats payload")
	}
}

func TestStatsProviderMergesAliasedIdentitiesOntoOneBinding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"legacy_user":{"tx":100,"rx":200},"v_abc":{"tx":10,"rx":20}}`))
	}))
	defer server.Close()
	provider := NewStatsProvider("test", server.URL+"/traffic", map[string]string{
		"legacy_user": "binding-1",
		"v_abc":       "binding-1",
	})
	batch, err := provider.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Readings) != 1 || batch.Readings[0].BindingID != "binding-1" {
		t.Fatalf("readings=%+v", batch.Readings)
	}
	if batch.Readings[0].UploadBytes != 110 || batch.Readings[0].DownloadBytes != 220 {
		t.Fatalf("merged counters=%+v, want 110/220", batch.Readings[0])
	}
	if len(batch.UnknownIdentities) != 0 {
		t.Fatalf("unknown=%v", batch.UnknownIdentities)
	}
}

func TestStatsProviderReturnsUnknownIdentityAlongsideValidReading(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"alice":{"tx":10,"rx":20},"unknown":{"tx":1,"rx":1}}`))
	}))
	defer server.Close()
	provider := NewStatsProvider("test", server.URL+"/traffic", map[string]string{"alice": "binding-1"})
	batch, err := provider.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Readings) != 1 || batch.Readings[0].BindingID != "binding-1" || len(batch.UnknownIdentities) != 1 || batch.UnknownIdentities[0] != "unknown" {
		t.Fatalf("batch=%+v", batch)
	}
}

func TestStatsProviderReportsUnavailableEndpoint(t *testing.T) {
	provider := NewStatsProvider("test", "http://127.0.0.1:1/traffic", nil)
	if _, err := provider.Read(); err == nil {
		t.Fatal("unavailable traffic API was silently treated as empty counters")
	}
}

func TestStatsProviderReadInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not json")) }))
	defer server.Close()
	provider := NewStatsProvider("test", server.URL, nil)
	if _, err := provider.Read(); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

var _ client.TrafficProvider = (*StatsProvider)(nil)

// TestStatsProviderFoldsMixedCaseStoredIdentity (#1111): hysteria2 reports
// per-user stats keyed by the lowercase username. A pre-migration binding
// row carrying "Alice" must still attribute the reported "alice" counters to
// its binding instead of dropping them as an unknown identity.
func TestStatsProviderFoldsMixedCaseStoredIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"alice":{"tx":100,"rx":200}}`))
	}))
	defer server.Close()
	provider := NewStatsProvider("test", server.URL+"/traffic", map[string]string{"Alice": "binding-1"})
	batch, err := provider.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Readings) != 1 || batch.Readings[0].BindingID != "binding-1" {
		t.Fatalf("readings=%+v", batch.Readings)
	}
	if batch.Readings[0].UploadBytes != 100 || batch.Readings[0].DownloadBytes != 200 {
		t.Fatalf("counters=%+v", batch.Readings[0])
	}
	if len(batch.UnknownIdentities) != 0 {
		t.Fatalf("folded identity reported unknown: %v", batch.UnknownIdentities)
	}
}

// TestStatsProviderCaseOnlyCollisionResolvesDeterministically (#1111): two
// stored identities differing only by case map to the same runtime user —
// folding must pick one binding deterministically (sorted input order) so a
// hand-edited pre-migration row cannot flip attribution between reads.
func TestStatsProviderCaseOnlyCollisionResolvesDeterministically(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"user":{"tx":5,"rx":6}}`))
	}))
	defer server.Close()
	provider := NewStatsProvider("test", server.URL+"/traffic", map[string]string{
		"USER": "binding-a",
		"user": "binding-b",
	})
	batch, err := provider.Read()
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Readings) != 1 || batch.Readings[0].BindingID != "binding-a" {
		// "USER" sorts before "user"; the first folded write wins.
		t.Fatalf("collision resolved to %+v, want deterministic binding-a", batch.Readings)
	}
}
