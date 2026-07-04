package status

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewQueryUsesDefaultAuthResolver(t *testing.T) {
	q := NewQuery(Options{AuthToken: "tok"}, io.Discard, nil)
	gotToken, gotSource := q.resolveAuth("tok")
	if gotToken != "tok" {
		t.Fatalf("token = %q, want %q", gotToken, "tok")
	}
	if gotSource != "flag" {
		t.Fatalf("source = %q, want %q", gotSource, "flag")
	}
}

func TestNewQueryUsesCustomAuthResolver(t *testing.T) {
	resolver := func(token string) (string, string) { return "resolved-" + token, "file" }
	q := NewQuery(Options{AuthToken: "tok"}, io.Discard, resolver)
	gotToken, gotSource := q.resolveAuth("tok")
	if gotToken != "resolved-tok" {
		t.Fatalf("token = %q, want %q", gotToken, "resolved-tok")
	}
	if gotSource != "file" {
		t.Fatalf("source = %q, want %q", gotSource, "file")
	}
}

func TestResolveListen(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "127.0.0.1:2096"},
		{"  ", "127.0.0.1:2096"},
		{"1.2.3.4:8080", "1.2.3.4:8080"},
		{"example.com", "example.com"},
	}
	for _, c := range cases {
		if got := ResolveListen(c.in); got != c.want {
			t.Errorf("ResolveListen(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCandidateAddrs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"127.0.0.1:2096", []string{"https://127.0.0.1:2096", "http://127.0.0.1:2096"}},
		{"https://example.com", []string{"https://example.com"}},
		{"http://example.com", []string{"http://example.com"}},
		{"tcp://example.com", []string{"tcp://example.com"}},
	}
	for _, c := range cases {
		got := CandidateAddrs(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("CandidateAddrs(%q) = %+v, want %+v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("CandidateAddrs(%q) = %+v, want %+v", c.in, got, c.want)
			}
		}
	}
}

func TestQueryRunSuccessOnFirstCandidate(t *testing.T) {
	status := &Response{Version: "0.4.0", Mode: "server", Services: []ServiceStatus{{Name: "veil", ActiveState: "active"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("X-Veil-Token"); got != "token" {
			t.Errorf("X-Veil-Token = %q, want %q", got, "token")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	}))
	defer server.Close()

	old := HTTPClient
	HTTPClient = func(string) *http.Client { return server.Client() }
	t.Cleanup(func() { HTTPClient = old })

	var out bytes.Buffer
	q := NewQuery(Options{Listen: server.Listener.Addr().String(), AuthToken: "token"}, &out, nil)
	if err := q.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "Veil 0.4.0") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestQueryRunSuccessOnSecondCandidate(t *testing.T) {
	status := &Response{Version: "0.4.0", Mode: "server"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(status)
	}))
	defer server.Close()

	calls := 0
	old := HTTPClient
	HTTPClient = func(string) *http.Client {
		calls++
		if calls == 1 {
			return &http.Client{Transport: alwaysErrorRoundTripper{}}
		}
		return server.Client()
	}
	t.Cleanup(func() { HTTPClient = old })

	var out bytes.Buffer
	q := NewQuery(Options{Listen: server.Listener.Addr().String()}, &out, nil)
	if err := q.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if calls < 2 {
		t.Fatalf("expected at least 2 HTTP calls, got %d", calls)
	}
}

func TestQueryRunAllCandidatesFail(t *testing.T) {
	old := HTTPClient
	HTTPClient = func(string) *http.Client { return &http.Client{Transport: alwaysErrorRoundTripper{}} }
	t.Cleanup(func() { HTTPClient = old })

	q := NewQuery(Options{Listen: "127.0.0.1:1"}, io.Discard, nil)
	err := q.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fetch status from") {
		t.Fatalf("error = %v", err)
	}
}

func TestQueryRunContextTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	old := HTTPClient
	HTTPClient = func(string) *http.Client { return server.Client() }
	t.Cleanup(func() { HTTPClient = old })

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	q := NewQuery(Options{Listen: server.Listener.Addr().String()}, io.Discard, nil)
	if err := q.Run(ctx); err == nil {
		t.Fatal("expected context timeout error")
	}
}

func TestFetchSuccess(t *testing.T) {
	want := &Response{Version: "0.4.0", Mode: "client", Services: []ServiceStatus{{Name: "veil", ActiveState: "active"}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Veil-Token"); got != "secret" {
			t.Errorf("token = %q, want %q", got, "secret")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer server.Close()

	old := HTTPClient
	HTTPClient = func(string) *http.Client { return server.Client() }
	t.Cleanup(func() { HTTPClient = old })

	got, err := Fetch(context.Background(), server.URL+"/api/status", "secret")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got.Version != want.Version || got.Mode != want.Mode || len(got.Services) != len(want.Services) {
		t.Fatalf("Fetch = %+v, want %+v", got, want)
	}
}

func TestFetchWithoutTokenOmitsHeader(t *testing.T) {
	var headerValue string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headerValue = r.Header.Get("X-Veil-Token")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&Response{})
	}))
	defer server.Close()

	old := HTTPClient
	HTTPClient = func(string) *http.Client { return server.Client() }
	t.Cleanup(func() { HTTPClient = old })

	if _, err := Fetch(context.Background(), server.URL+"/api/status", ""); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if headerValue != "" {
		t.Fatalf("token header = %q, want empty", headerValue)
	}
}

func TestFetchNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "unauthorized\n")
	}))
	defer server.Close()

	old := HTTPClient
	HTTPClient = func(string) *http.Client { return server.Client() }
	t.Cleanup(func() { HTTPClient = old })

	_, err := Fetch(context.Background(), server.URL+"/api/status", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "401 Unauthorized") || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("error = %v", err)
	}
}

func TestFetchInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "not-json")
	}))
	defer server.Close()

	old := HTTPClient
	HTTPClient = func(string) *http.Client { return server.Client() }
	t.Cleanup(func() { HTTPClient = old })

	_, err := Fetch(context.Background(), server.URL+"/api/status", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchNetworkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	old := HTTPClient
	HTTPClient = func(string) *http.Client { return server.Client() }
	t.Cleanup(func() { HTTPClient = old })

	_, err := Fetch(context.Background(), server.URL+"/api/status", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchInvalidURL(t *testing.T) {
	_, err := Fetch(context.Background(), "://invalid-url", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRenderHuman(t *testing.T) {
	response := &Response{
		Version: "0.4.0",
		Mode:    "server",
		Services: []ServiceStatus{
			{Name: "veil", ActiveState: "active", Transport: "wireguard"},
			{Name: "dns", ActiveState: "failed", Error: "oops"},
			{Name: "proxy", ActiveState: "inactive", Transport: "http"},
		},
	}
	var out bytes.Buffer
	if err := NewQuery(Options{JSON: false}, &out, nil).Render(response); err != nil {
		t.Fatalf("Render: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Veil 0.4.0",
		"Mode: server",
		"● veil (wireguard): active",
		"✕ dns: failed (error: oops)",
		"○ proxy (http): inactive",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestRenderJSON(t *testing.T) {
	response := &Response{Version: "test", Mode: "server", Services: []ServiceStatus{{Name: "veil", ActiveState: "active"}}}
	var out bytes.Buffer
	if err := NewQuery(Options{JSON: true}, &out, nil).Render(response); err != nil {
		t.Fatalf("Render: %v", err)
	}
	var got Response
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if got.Version != response.Version || got.Mode != response.Mode {
		t.Fatalf("Render = %+v, want %+v", got, response)
	}
}

type alwaysErrorRoundTripper struct{}

func (alwaysErrorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("simulated network error")
}

func TestHTTPClientDefault(t *testing.T) {
	client := HTTPClient("https://example.com/api/status")
	if client != http.DefaultClient {
		t.Fatal("expected HTTPClient to return http.DefaultClient by default")
	}
}

func BenchmarkRenderHuman(b *testing.B) {
	response := &Response{
		Version: "0.4.0",
		Mode:    "server",
		Services: []ServiceStatus{
			{Name: "veil", ActiveState: "active"},
			{Name: "dns", ActiveState: "failed"},
		},
	}
	q := NewQuery(Options{}, io.Discard, nil)
	for i := 0; i < b.N; i++ {
		if err := q.Render(response); err != nil {
			b.Fatal(err)
		}
	}
}
