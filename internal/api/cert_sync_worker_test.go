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

// reconcilePending must not treat an unknown target set as "no targets": when
// syncOnce failed before the live target list existed (fence acquire, glob
// error, missing backend), pending marks must survive until their deadline
// instead of being wiped by a transient error (#1168 review).
func TestCertSyncReconcilePendingKeepsMarksOnFailedSync(t *testing.T) {
	state := newFencedPanelState(t, &recordingPrivilegedClient{})
	worker := newCertSyncWorker(state)
	worker.SignalPending([]string{"edge.example.com"})
	if !worker.PendingDomain("edge.example.com") {
		t.Fatal("pending mark was not recorded")
	}

	// targets == nil == "sync never reached the point where the live target
	// set was known" — pending must not be touched.
	worker.reconcilePending(map[string]certSyncOutcome{}, nil)
	if !worker.PendingDomain("edge.example.com") {
		t.Fatal("nil targets wiped the pending mark on a failed sync")
	}

	// An empty-but-known target set means the domain really left the live
	// config — dropping is correct there.
	worker.reconcilePending(map[string]certSyncOutcome{}, []hysteria2CertSyncTarget{})
	if worker.PendingDomain("edge.example.com") {
		t.Fatal("pending mark survived a known-empty live target set")
	}
}

// A still-served domain whose ACME material is absent burns an attempt each
// pass until the budget or the hard deadline is spent — retries never run
// unbounded (#1168).
func TestCertSyncReconcilePendingCountsAttempts(t *testing.T) {
	state := newFencedPanelState(t, &recordingPrivilegedClient{})
	worker := newCertSyncWorker(state)
	worker.maxAttempts = 2
	worker.SignalPending([]string{"edge.example.com"})

	targets := []hysteria2CertSyncTarget{{Domain: "edge.example.com", Unit: "veil-hysteria2@edge.service"}}
	outcomes := map[string]certSyncOutcome{"edge.example.com": certSyncMissing}

	worker.reconcilePending(outcomes, targets)
	if !worker.PendingDomain("edge.example.com") {
		t.Fatal("pending dropped after a single missing outcome")
	}
	worker.reconcilePending(outcomes, targets)
	// attempts (2) reached maxAttempts on this pass → dropped.
	worker.reconcilePending(outcomes, targets)
	if worker.PendingDomain("edge.example.com") {
		t.Fatal("pending survived past the attempt budget")
	}
}

// A found outcome converges the pending mark immediately.
func TestCertSyncReconcilePendingClearsOnFound(t *testing.T) {
	state := newFencedPanelState(t, &recordingPrivilegedClient{})
	worker := newCertSyncWorker(state)
	worker.SignalPending([]string{"edge.example.com"})
	worker.reconcilePending(
		map[string]certSyncOutcome{"edge.example.com": certSyncFound},
		[]hysteria2CertSyncTarget{{Domain: "edge.example.com", Unit: "veil-hysteria2@edge.service"}},
	)
	if worker.PendingDomain("edge.example.com") {
		t.Fatal("pending mark survived issued material")
	}
}
