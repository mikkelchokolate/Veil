package privileged

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestProductionExecutorPromotesResolvedArtifactsWithSafetyCopy(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "staging", "mieru.json")
	destination := filepath.Join(root, "generated", "mieru.json")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	executor := NewProductionExecutor(ProductionConfig{
		PromotionBackupRoot: filepath.Join(root, "backups"),
		Now:                 func() time.Time { return time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC) },
	})
	result, err := executor.Promote(context.Background(), ResolvedPromotion{
		Artifacts: []ResolvedArtifact{{ID: "mieru", Source: source, Destination: destination}},
	})
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	body, _ := os.ReadFile(destination)
	if string(body) != "new" {
		t.Fatalf("destination = %q", body)
	}
	if result.BackupID == "" || !reflect.DeepEqual(result.WrittenArtifacts, []string{"mieru"}) {
		t.Fatalf("unexpected promote result: %+v", result)
	}
}

func TestProductionExecutorUsesOnlyFixedCommandMappings(t *testing.T) {
	var commands [][]string
	run := func(_ context.Context, command []string, _ time.Duration) (string, error) {
		commands = append(commands, append([]string(nil), command...))
		if len(command) > 1 && command[0] == "systemctl" && command[1] == "show" {
			return "LoadState=loaded\nActiveState=active\nSubState=running\n", nil
		}
		if len(command) > 0 && command[0] == "journalctl" {
			return "line one\nline two\n", nil
		}
		return "", nil
	}
	executor := NewProductionExecutor(ProductionConfig{
		RunCommand: run,
		FirewallCommands: map[string][]string{
			"allow-panel": {"ufw", "allow", "2096/tcp", "comment", "Veil panel"},
		},
	})

	if err := executor.ServiceAction(context.Background(), ServiceActionRequest{Unit: "veil.service", Action: ServiceActionRestart}); err != nil {
		t.Fatalf("service action: %v", err)
	}
	status, err := executor.ServiceStatus(context.Background(), ServiceStatusRequest{Units: []string{"veil.service"}})
	if err != nil || len(status.Services) != 1 || status.Services[0].ActiveState != "active" {
		t.Fatalf("service status: %+v err=%v", status, err)
	}
	journal, err := executor.Journal(context.Background(), ResolvedJournal{Unit: "veil.service", Lines: 25})
	if err != nil || !reflect.DeepEqual(journal.Lines, []string{"line one", "line two"}) {
		t.Fatalf("journal: %+v err=%v", journal, err)
	}
	firewall, err := executor.Firewall(context.Background(), ResolvedFirewall{RuleIDs: []string{"allow-panel"}})
	if err != nil || !reflect.DeepEqual(firewall.AppliedRuleIDs, []string{"allow-panel"}) {
		t.Fatalf("firewall: %+v err=%v", firewall, err)
	}
	if err := executor.RestartPanel(context.Background()); err != nil {
		t.Fatalf("restart Panel: %v", err)
	}

	want := [][]string{
		{"systemctl", "restart", "veil.service"},
		{"systemctl", "show", "veil.service", "--property=LoadState", "--property=ActiveState", "--property=SubState", "--no-page"},
		{"journalctl", "-u", "veil.service", "--no-pager", "-n", "25", "-o", "short-iso"},
		{"ufw", "allow", "2096/tcp", "comment", "Veil panel"},
		{"systemctl", "restart", "veil.service"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands:\nwant=%v\ngot=%v", want, commands)
	}
}

func TestProductionExecutorMapsBackupUpdateAndRotationHooks(t *testing.T) {
	var gotBackup ResolvedBackup
	var gotUpdate ResolvedUpdate
	rotated := false
	executor := NewProductionExecutor(ProductionConfig{
		BackupWorkflow: func(_ context.Context, request ResolvedBackup) (BackupResult, error) {
			gotBackup = request
			return BackupResult{ArchiveName: request.ArchiveName}, nil
		},
		UpdateWorkflow: func(_ context.Context, request ResolvedUpdate) (UpdateResult, error) {
			gotUpdate = request
			return UpdateResult{ArtifactID: request.ArtifactID, Staged: true}, nil
		},
		RotateKeyWorkflow: func(context.Context) error {
			rotated = true
			return nil
		},
	})
	if _, err := executor.Backup(context.Background(), ResolvedBackup{Action: BackupActionVerify, ArchiveName: "daily.enc"}); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if _, err := executor.Update(context.Background(), ResolvedUpdate{ArtifactID: "veil-linux-amd64", Path: "/managed/update"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := executor.RotateKey(context.Background(), RotateKeyRequest{}); err != nil {
		t.Fatalf("rotate key: %v", err)
	}
	if gotBackup.ArchiveName != "daily.enc" || gotUpdate.ArtifactID != "veil-linux-amd64" || !rotated {
		t.Fatalf("workflow mapping failed: backup=%+v update=%+v rotated=%t", gotBackup, gotUpdate, rotated)
	}
}

func TestProductionExecutorPropagatesWorkflowErrors(t *testing.T) {
	expected := errors.New("denied")
	executor := NewProductionExecutor(ProductionConfig{
		RunCommand: func(context.Context, []string, time.Duration) (string, error) {
			return "", expected
		},
	})
	if err := executor.ServiceAction(context.Background(), ServiceActionRequest{Unit: "veil.service", Action: ServiceActionRestart}); !errors.Is(err, expected) {
		t.Fatalf("want %v, got %v", expected, err)
	}
}
