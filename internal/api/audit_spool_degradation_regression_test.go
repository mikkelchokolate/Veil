package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/audit"
)

func TestAuditPrimaryFailureWithDurableSpoolIsVisibleAsDegraded(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "primary-directory")
	if err := os.Mkdir(primary, 0o700); err != nil {
		t.Fatal(err)
	}
	recorder := audit.NewRecorder(primary, audit.RecorderOptions{SpoolPath: filepath.Join(root, "critical.spool")})
	state := &managementState{audit: recorder}
	if err := state.recordRequestAudit(nil, audit.Record{Action: "backup.restore.start", Success: true}); err != nil {
		t.Fatalf("durably spooled critical audit failed request: %v", err)
	}
	if !state.isAuditDegraded() || recorder.Degraded() == nil {
		t.Fatal("successful spool hid primary audit degradation")
	}
}

func TestProductionAuditRecorderSpoolsKeyRotationAndRestore(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	auditPath := filepath.Join(filepath.Dir(statePath), "audit", "panel.jsonl")
	if err := os.MkdirAll(filepath.Dir(auditPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(auditPath, 0o700); err != nil {
		t.Fatal(err)
	}
	recorder := audit.NewRecorder(auditPath, audit.RecorderOptions{})
	state := &managementState{audit: recorder}
	if err := state.recordRequestAudit(nil, audit.Record{Action: "security.key.rotate", Success: true}); err != nil {
		t.Fatalf("production key rotation audit was dropped: %v", err)
	}
	if err := state.recordRequestAudit(nil, audit.Record{Action: "backup.restore", Success: true}); err != nil {
		t.Fatalf("production restore audit was dropped: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(auditPath), "critical.spool")); err != nil {
		t.Fatalf("production recorder did not enable the critical spool: %v", err)
	}
	if !state.isAuditDegraded() || recorder.Degraded() == nil {
		t.Fatal("successful spool hid primary audit degradation")
	}
}

func TestAuditSpoolReplayFailureRemainsVisible(t *testing.T) {
	root := t.TempDir()
	spool := filepath.Join(root, "critical.spool")
	if err := os.WriteFile(spool, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recorder := audit.NewRecorder(filepath.Join(root, "audit.jsonl"), audit.RecorderOptions{SpoolPath: spool})
	if recorder.Degraded() == nil {
		t.Fatal("startup swallowed audit spool replay failure")
	}
	// The corrupt line is quarantined to <spool>.corrupt: evidence must be
	// preserved even though the replay drains and degrades (#1033).
	quarantine, err := os.ReadFile(spool + ".corrupt")
	if err != nil {
		t.Fatalf("corrupt spool line was discarded: %v", err)
	}
	if !strings.Contains(string(quarantine), "not-json") {
		t.Fatalf("quarantine lost the corrupt evidence: %q", quarantine)
	}
}

// TestHealthReportsDurablySpoolingAudit (#981): when the primary audit log is
// unavailable but the critical spool accepted the event, /health must not
// label audit_spool durability_unverified — the spool is proven durable.
func TestHealthReportsDurablySpoolingAudit(t *testing.T) {
	root := t.TempDir()
	// A directory as the primary path makes every primary append fail.
	primary := filepath.Join(root, "primary-directory")
	if err := os.Mkdir(primary, 0o700); err != nil {
		t.Fatal(err)
	}
	recorder := audit.NewRecorder(primary, audit.RecorderOptions{SpoolPath: filepath.Join(root, "critical.spool")})
	state := &managementState{audit: recorder}
	// backup.create was missing from the critical allowlist (#981): it must
	// durably spool instead of being dropped.
	if err := state.recordRequestAudit(nil, audit.Record{Action: "backup.create", Success: true}); err != nil {
		t.Fatalf("critical append should durably spool: %v", err)
	}
	response, _ := HealthRoutes{State: state}.snapshot(context.Background())
	if got := response.Components["audit_primary"]; got.Status != "degraded" || got.Reason != "primary_unavailable" {
		t.Fatalf("audit_primary = %+v, want degraded/primary_unavailable", got)
	}
	if got := response.Components["audit_spool"]; got.Status != "ok" || got.Reason != "spool_active" {
		t.Fatalf("audit_spool = %+v, want ok/spool_active (spool proved durable)", got)
	}
}

// TestHealthMarksUnprovenSpoolUnverified (#981): when a critical append fails
// at both sinks, audit_spool must stay degraded/durability_unverified.
func TestHealthMarksUnprovenSpoolUnverified(t *testing.T) {
	root := t.TempDir()
	primary := filepath.Join(root, "primary-directory")
	if err := os.Mkdir(primary, 0o700); err != nil {
		t.Fatal(err)
	}
	spoolDir := filepath.Join(root, "spool-directory")
	if err := os.Mkdir(spoolDir, 0o700); err != nil {
		t.Fatal(err)
	}
	recorder := audit.NewRecorder(primary, audit.RecorderOptions{SpoolPath: spoolDir, BackpressurePolicy: "spool_critical"})
	state := &managementState{audit: recorder}
	if err := state.recordRequestAudit(nil, audit.Record{Action: "security.key.rotate", Success: true}); err == nil {
		t.Fatal("append with both sinks broken should fail")
	}
	response, _ := HealthRoutes{State: state}.snapshot(context.Background())
	if got := response.Components["audit_spool"]; got.Status != "degraded" || got.Reason != "durability_unverified" {
		t.Fatalf("audit_spool = %+v, want degraded/durability_unverified", got)
	}
}
