package privileged

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// #1276: the helper's CaddyLoad must reach the admin API over the Caddy
// admin Unix socket (owner-only, veil-proxy + root) instead of the legacy
// loopback TCP port any local uid could dial.
func TestProductionCaddyLoadDialsAdminUnixSocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "veilcaddy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "admin.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	var loaded []byte
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "127.0.0.1" {
			http.Error(w, "host not allowed: "+r.Host, http.StatusForbidden)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/load":
			loaded, _ = io.ReadAll(r.Body)
		case r.Method == http.MethodGet && r.URL.Path == "/config/":
			_, _ = w.Write(loaded)
		default:
			http.NotFound(w, r)
		}
	})}
	go func() { _ = srv.Serve(listener) }()
	t.Cleanup(func() { _ = srv.Close() })

	executor := NewProductionExecutor(ProductionConfig{
		PromotionBackupRoot: t.TempDir(),
		CaddyAdminEndpoint:  "unix/" + socket + "|0200",
	})
	config := []byte(`{"admin":{"listen":"unix/` + socket + `|0200"},"apps":{}}`)
	if err := executor.CaddyLoad(context.Background(), CaddyLoadRequest{Config: config}); err != nil {
		t.Fatalf("CaddyLoad over unix socket: %v", err)
	}
	if string(loaded) != string(config) {
		t.Fatalf("admin received %q, want %q", loaded, config)
	}
}
