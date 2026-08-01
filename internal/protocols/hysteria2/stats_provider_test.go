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
	if len(readings) != 2 || readings["binding-1"].UploadBytes != 1024 || readings["binding-2"].DownloadBytes != 256 {
		t.Fatalf("unexpected readings: %+v", readings)
	}
}

func TestStatsProviderRejectsCounterOverflowAndUnknownIdentity(t *testing.T) {
	for name, payload := range map[string]string{
		"overflow": `{"alice":{"tx":18446744073709551615,"rx":1}}`,
		"unknown":  `{"unknown":{"tx":1,"rx":1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(payload))
			}))
			defer server.Close()
			provider := NewStatsProvider("test", server.URL+"/traffic", map[string]string{"alice": "binding-1"})
			if _, err := provider.Read(); err == nil {
				t.Fatalf("accepted invalid stats payload %s", payload)
			}
		})
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
