package status

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalHealthURLRewritesWildcardBinds(t *testing.T) {
	cases := []struct {
		listen, scheme, base, want string
	}{
		{"0.0.0.0:2096", "http", "/", "http://127.0.0.1:2096/healthz"},
		{"127.0.0.1:2096", "http", "/", "http://127.0.0.1:2096/healthz"},
		{"0.0.0.0:8443", "https", "/", "https://127.0.0.1:8443/healthz"},
		{"[::]:443", "https", "/secret-panel/", "https://[::1]:443/secret-panel/healthz"},
		{"0.0.0.0:2096", "http", "secret-panel", "http://127.0.0.1:2096/secret-panel/healthz"},
	}
	for _, tc := range cases {
		got, err := LocalHealthURL(ContainerHealthContract{Listen: tc.listen, Scheme: tc.scheme, WebBasePath: tc.base})
		if err != nil {
			t.Fatalf("LocalHealthURL(%q): %v", tc.listen, err)
		}
		if got != tc.want {
			t.Fatalf("LocalHealthURL(%q, %q, %q)=%q want %q", tc.listen, tc.scheme, tc.base, got, tc.want)
		}
	}
}

func TestProbeHealthDefaultHTTP(t *testing.T) {
	server := newHealthzServer(t, "/", "", http.StatusOK)
	contract := ContainerHealthContract{Listen: server.Listener.Addr().String(), Scheme: "http", WebBasePath: "/"}
	if err := Probe(context.Background(), contract, ""); err != nil {
		t.Fatalf("Probe: %v", err)
	}
}

func TestProbeHealthCustomPortAndBasePath(t *testing.T) {
	server := newHealthzServer(t, "/secret-panel/", "token", http.StatusOK)
	contract := ContainerHealthContract{Listen: server.Listener.Addr().String(), Scheme: "http", WebBasePath: "/secret-panel/"}
	if err := Probe(context.Background(), contract, "token"); err != nil {
		t.Fatalf("Probe: %v", err)
	}
}

func TestProbeHealthHTTPSUsesSuppliedTrustMaterial(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(server.Close)
	certPath := filepath.Join(t.TempDir(), "tls.crt")
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	contract := ContainerHealthContract{
		Listen:      server.Listener.Addr().String(),
		Scheme:      "https",
		WebBasePath: "/",
		TLSCert:     certPath,
	}
	if err := Probe(context.Background(), contract, ""); err != nil {
		t.Fatalf("Probe: %v", err)
	}
}

func TestProbeHealthUnavailableServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	err = Probe(context.Background(), ContainerHealthContract{Listen: addr, Scheme: "http", WebBasePath: "/"}, "")
	if err == nil {
		t.Fatal("expected probe failure for unavailable server")
	}
}

func TestWriteAndReadContainerHealthContract(t *testing.T) {
	path := filepath.Join(t.TempDir(), "container-health.json")
	want := ContractFromServe("0.0.0.0:443", true, "/secret/", "/etc/veil/tls.crt", "example.com")
	if err := WriteContract(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got=%+v want=%+v", got, want)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed["token"]; ok || strings.Contains(string(raw), "secret-token") {
		t.Fatalf("contract persisted a credential: %s", raw)
	}
}

func newHealthzServer(t *testing.T, basePath, token string, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := strings.TrimRight(basePath, "/") + "/healthz"
		if basePath == "/" {
			wantPath = "/healthz"
		}
		if r.URL.Path != wantPath {
			http.NotFound(w, r)
			return
		}
		if token != "" && r.Header.Get("X-Veil-Token") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(server.Close)
	return server
}
