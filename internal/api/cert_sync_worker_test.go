package api

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// writeHy2Config drops a rendered hysteria2 config referencing a
// Caddy-managed certificate for domain.
func writeHy2Config(t *testing.T, liveRoot, inbound, domain string) {
	t.Helper()
	dir := filepath.Join(liveRoot, "hysteria2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "tls:\n  cert: /etc/veil/certs/" + domain + ".crt\n  key: /etc/veil/certs/" + domain + ".key\n"
	if err := os.WriteFile(filepath.Join(dir, inbound+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCertSyncWorkerRestartsOnlyOnChange (#1103): the periodic sync must
// re-copy renewed certificates AND restart only the instances whose cert
// material actually changed — restarting on every poll would drop live
// sessions hourly for no reason.
func TestCertSyncWorkerRestartsOnlyOnChange(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newFencedPanelState(t, client)
	root := t.TempDir()
	state.mu.Lock()
	state.liveRoot = filepath.Join(root, "live")
	state.mu.Unlock()
	writeHy2Config(t, state.liveRoot, "main", "vpn.example.com")

	worker := newCertSyncWorker(state)

	// Unchanged material: copy happens, no restart.
	client.syncCaddyCertResult = &privileged.SyncCaddyCertResult{Found: true, Changed: false}
	if err := worker.SyncOnce(context.Background()); err != nil {
		t.Fatalf("unchanged sync: %v", err)
	}
	if len(client.syncRequests()) != 1 {
		t.Fatalf("sync requests=%d, want 1", len(client.syncRequests()))
	}
	if got := client.syncRequests()[0].Domain; got != "vpn.example.com" {
		t.Fatalf("synced domain %q, want vpn.example.com", got)
	}
	if len(client.serviceActions) != 0 {
		t.Fatalf("unchanged cert triggered restart: %+v", client.serviceActions)
	}

	// Renewed material: restart the unit derived from the config filename.
	client.syncCaddyCertResult = &privileged.SyncCaddyCertResult{Found: true, Changed: true}
	if err := worker.SyncOnce(context.Background()); err != nil {
		t.Fatalf("changed sync: %v", err)
	}
	if len(client.serviceActions) != 1 {
		t.Fatalf("restarts=%d, want 1", len(client.serviceActions))
	}
	action := client.serviceActions[0]
	if action.Unit != "veil-hysteria2@main.service" || action.Action != privileged.ServiceActionRestart {
		t.Fatalf("restart action %+v, want restart veil-hysteria2@main.service", action)
	}

	// Certificate not yet issued: steady state, no restart, no error.
	client.syncCaddyCertResult = &privileged.SyncCaddyCertResult{Found: false}
	if err := worker.SyncOnce(context.Background()); err != nil {
		t.Fatalf("not-found sync: %v", err)
	}
	if len(client.serviceActions) != 1 {
		t.Fatalf("not-found triggered restart: %+v", client.serviceActions)
	}
}

// TestCertSyncWorkerLifecycle proves Start/Stop do not hang and a signaled
// pass actually runs the sync.
func TestCertSyncWorkerLifecycle(t *testing.T) {
	client := &recordingPrivilegedClient{}
	state := newFencedPanelState(t, client)
	root := t.TempDir()
	state.mu.Lock()
	state.liveRoot = filepath.Join(root, "live")
	state.mu.Unlock()
	writeHy2Config(t, state.liveRoot, "edge", "edge.example.com")

	worker := newCertSyncWorker(state)
	worker.interval = time.Hour
	worker.Start()
	worker.Signal()
	deadline := time.Now().Add(5 * time.Second)
	for len(client.syncRequests()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(client.syncRequests()) == 0 {
		t.Fatal("signaled sync pass never ran")
	}
	worker.Stop()
	select {
	case <-worker.done:
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not stop")
	}
}

// TestCertSyncTargetsSkipsNonCertConfigs: inbound configs that serve a
// panel-issued cert (tls.cert not under <etc>/certs/) produce no targets.
func TestCertSyncTargetsSkipsNonCertConfigs(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "live")
	dir := filepath.Join(live, "hysteria2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Panel-issued certificate path — no Caddy sync needed.
	if err := os.WriteFile(filepath.Join(dir, "panel-cert.yaml"),
		[]byte("tls:\n  cert: /etc/veil/tls/panel.crt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Non-hysteria2 directory and non-yaml file are ignored.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	targets := hysteria2CertSyncTargets([]string{
		filepath.Join(dir, "panel-cert.yaml"),
		filepath.Join(dir, "notes.txt"),
	})
	if len(targets) != 0 {
		t.Fatalf("unexpected targets: %+v", targets)
	}
}
