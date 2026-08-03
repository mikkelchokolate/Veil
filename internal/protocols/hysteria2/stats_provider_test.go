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
