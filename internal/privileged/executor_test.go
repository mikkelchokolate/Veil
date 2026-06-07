package privileged

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
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

func TestProductionExecutorRestoresPromotionByOpaqueBackupID(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "staging", "edge.Caddyfile")
	destination := filepath.Join(root, "generated", "edge.Caddyfile")
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
	promoted, err := executor.Promote(context.Background(), ResolvedPromotion{
		Artifacts: []ResolvedArtifact{{ID: "caddy/edge.Caddyfile", Source: source, Destination: destination}},
	})
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if _, err := executor.Promote(context.Background(), ResolvedPromotion{RestoreBackupID: promoted.BackupID}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	body, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "old" {
		t.Fatalf("restored destination=%q", body)
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

func TestProductionExecutorServiceStatusSkipsTemplatesAndToleratesFailures(t *testing.T) {
	var shown []string
	run := func(_ context.Context, command []string, _ time.Duration) (string, error) {
		if len(command) > 2 && command[0] == "systemctl" && command[1] == "show" {
			unit := command[2]
			shown = append(shown, unit)
			if unit == "veil-mieru.service" {
				return "Failed to get properties: Unit veil-mieru.service not loaded", fmt.Errorf("exit status 1")
			}
			return "LoadState=loaded\nActiveState=active\nSubState=running\n", nil
		}
		return "", nil
	}
	executor := NewProductionExecutor(ProductionConfig{RunCommand: run})

	result, err := executor.ServiceStatus(context.Background(), ServiceStatusRequest{
		Units: []string{"veil.service", "veil-hysteria2@.service", "veil-mieru.service"},
	})
	if err != nil {
		t.Fatalf("a single unit failure must not abort the batch: %v", err)
	}
	if len(result.Services) != 3 {
		t.Fatalf("want 3 services, got %d: %+v", len(result.Services), result.Services)
	}
	byUnit := map[string]ServiceStatus{}
	for _, s := range result.Services {
		byUnit[s.Unit] = s
	}
	// Template units are reported inactive and never queried via systemctl show.
	if got := byUnit["veil-hysteria2@.service"]; got.ActiveState != "inactive" || got.Error != "" {
		t.Fatalf("template unit should be inactive without error: %+v", got)
	}
	for _, u := range shown {
		if u == "veil-hysteria2@.service" {
			t.Fatal("template unit must not be queried with systemctl show")
		}
	}
	// Healthy unit parsed normally.
	if got := byUnit["veil.service"]; got.ActiveState != "active" {
		t.Fatalf("veil.service should be active: %+v", got)
	}
	// A failing unit records its error but does not break the batch.
	if got := byUnit["veil-mieru.service"]; got.ActiveState != "unknown" || got.Error == "" {
		t.Fatalf("failed unit should carry an error and unknown state: %+v", got)
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

func TestProductionExecutorReadsManagedBackupArchive(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "daily.enc")
	if err := os.WriteFile(archive, []byte("VEILBACK-data"), 0o600); err != nil {
		t.Fatal(err)
	}
	executor := NewProductionExecutor(ProductionConfig{})
	result, err := executor.Backup(context.Background(), ResolvedBackup{
		Action:      BackupActionRead,
		ArchiveName: "daily.enc",
		ArchivePath: archive,
	})
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(result.Data) != "VEILBACK-data" {
		t.Fatalf("backup data = %q", result.Data)
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

func TestProductionExecutorVerifiesAndInstallsStagedUpdate(t *testing.T) {
	root := t.TempDir()
	currentPath := filepath.Join(root, "veil")
	archivePath := filepath.Join(root, "updates", "veil-update.tar.gz")
	checksumsPath := filepath.Join(root, "updates", "checksums.txt")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(currentPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := privilegedTestArchive(t, []byte("new-binary"))
	hash := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(hash[:]), updateflow.AssetName())
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checksumsPath, []byte(checksums), 0o600); err != nil {
		t.Fatal(err)
	}

	executor := NewProductionExecutor(ProductionConfig{BinaryPath: currentPath})
	result, err := executor.Update(context.Background(), ResolvedUpdate{
		ArtifactID:    "veil-update",
		Version:       "v0.6.0",
		Path:          archivePath,
		ChecksumsPath: checksumsPath,
	})
	if err != nil {
		t.Fatalf("install staged update: %v", err)
	}
	body, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new-binary" || !result.Installed || result.Version != "v0.6.0" {
		t.Fatalf("result=%+v binary=%q", result, body)
	}
}

func TestProductionExecutorRejectsStagedUpdateChecksumMismatch(t *testing.T) {
	root := t.TempDir()
	currentPath := filepath.Join(root, "veil")
	archivePath := filepath.Join(root, "updates", "veil-update.tar.gz")
	checksumsPath := filepath.Join(root, "updates", "checksums.txt")
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(currentPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archivePath, privilegedTestArchive(t, []byte("new-binary")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checksumsPath, []byte(fmt.Sprintf("%064s  %s\n", "0", updateflow.AssetName())), 0o600); err != nil {
		t.Fatal(err)
	}

	executor := NewProductionExecutor(ProductionConfig{BinaryPath: currentPath})
	if _, err := executor.Update(context.Background(), ResolvedUpdate{
		ArtifactID:    "veil-update",
		Version:       "v0.6.0",
		Path:          archivePath,
		ChecksumsPath: checksumsPath,
	}); err == nil {
		t.Fatal("expected checksum mismatch")
	}
	body, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "old-binary" {
		t.Fatalf("binary changed after rejected update: %q", body)
	}
}

func privilegedTestArchive(t *testing.T, binary []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gzipWriter := gzip.NewWriter(&buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "veil", Mode: 0o755, Size: int64(len(binary))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
