package privileged

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/releaseverify"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/storage"
)

func createBackupTestDatabase(t *testing.T, root string) {
	t.Helper()
	db, err := storage.Open(filepath.Join(root, "veil.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

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

func TestProductionExecutorFailsWhenProtocolConfigOwnershipCannotBeSet(t *testing.T) {
	oldEffectiveUID := effectiveUID
	oldLookupUser := lookupUser
	oldChownPath := chownPath
	defer func() {
		effectiveUID = oldEffectiveUID
		lookupUser = oldLookupUser
		chownPath = oldChownPath
	}()

	effectiveUID = func() int { return 0 }
	lookupUser = func(string) (*user.User, error) {
		return &user.User{Uid: "123", Gid: "456"}, nil
	}
	ownershipErr := errors.New("injected chown failure")
	chownPath = func(string, int, int) error { return ownershipErr }

	root := t.TempDir()
	source := filepath.Join(root, "staging", "mieru", "server_config.json")
	destination := filepath.Join(root, "generated", "mieru", "server_config.json")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(`{"portBindings":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	executor := NewProductionExecutor(ProductionConfig{
		PromotionBackupRoot: filepath.Join(root, "backups"),
	})
	_, err := executor.Promote(context.Background(), ResolvedPromotion{
		Artifacts: []ResolvedArtifact{{
			ID:          "mieru/server_config.json",
			Source:      source,
			Destination: destination,
		}},
	})
	if !errors.Is(err, ownershipErr) {
		t.Fatalf("promote error = %v, want injected ownership error", err)
	}
}

// TestProductionExecutorDoesNotBackupSymlinkTargetOnRemoval covers the
// exfiltration vector: if a managed artifact slated for removal is replaced by
// a symlink pointing outside the managed root, backupPromotionDestination must
// not read the symlink target into the promotion backup (which the caller could
// then retrieve via the returned backup ID). The helper must detect the symlink
// and skip the content backup instead of following it.
func TestProductionExecutorDoesNotBackupSymlinkTargetOnRemoval(t *testing.T) {
	root := t.TempDir()
	backupRoot := filepath.Join(root, "backups")
	destination := filepath.Join(root, "generated", "caddy", "legacy.Caddyfile")
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(root, "outside-secret")
	if err := os.WriteFile(secret, []byte("TOP-SECRET-OUTSIDE-ROOT"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, destination); err != nil {
		t.Fatal(err)
	}

	executor := NewProductionExecutor(ProductionConfig{PromotionBackupRoot: backupRoot})
	result, err := executor.Promote(context.Background(), ResolvedPromotion{
		RemoveArtifacts: []ResolvedArtifact{{
			ID:          "caddy/legacy.Caddyfile",
			Destination: destination,
		}},
	})
	if err != nil {
		t.Fatalf("promote removal: %v", err)
	}
	// The symlink itself is removed.
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("expected symlink to be removed, stat err=%v", statErr)
	}
	// No backup artifact may contain the outside secret.
	for _, backupPath := range result.BackupArtifacts {
		body, readErr := os.ReadFile(backupPath)
		if readErr != nil {
			continue
		}
		if string(body) == "TOP-SECRET-OUTSIDE-ROOT" {
			t.Fatalf("symlink target content was exfiltrated into backup %s", backupPath)
		}
	}
}

func TestProductionExecutorGrantsPanelReadAccessToCaddyConfig(t *testing.T) {
	oldEffectiveUID := effectiveUID
	oldLookupUser := lookupUser
	oldChownPath := chownPath
	oldChmodPath := chmodPath
	defer func() {
		effectiveUID = oldEffectiveUID
		lookupUser = oldLookupUser
		chownPath = oldChownPath
		chmodPath = oldChmodPath
	}()

	effectiveUID = func() int { return 0 }
	lookupUser = func(name string) (*user.User, error) {
		if name != "veil" {
			t.Fatalf("lookup user = %q, want veil", name)
		}
		return &user.User{Uid: "123", Gid: "456"}, nil
	}
	type chownCall struct {
		path     string
		uid, gid int
	}
	type chmodCall struct {
		path string
		mode os.FileMode
	}
	var chowns []chownCall
	var chmods []chmodCall
	chownPath = func(path string, uid, gid int) error {
		chowns = append(chowns, chownCall{path: path, uid: uid, gid: gid})
		return nil
	}
	chmodPath = func(path string, mode os.FileMode) error {
		chmods = append(chmods, chmodCall{path: path, mode: mode})
		return nil
	}

	root := t.TempDir()
	source := filepath.Join(root, "staging", "caddy", "config.json")
	destination := filepath.Join(root, "generated", "caddy", "config.json")
	if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(`{"apps":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	executor := NewProductionExecutor(ProductionConfig{PromotionBackupRoot: filepath.Join(root, "backups")})
	if _, err := executor.Promote(context.Background(), ResolvedPromotion{Artifacts: []ResolvedArtifact{{
		ID:          "caddy/config.json",
		Source:      source,
		Destination: destination,
	}}}); err != nil {
		t.Fatalf("promote: %v", err)
	}

	wantChowns := []chownCall{
		{path: filepath.Dir(destination), uid: 0, gid: 456},
		{path: destination, uid: 0, gid: 456},
	}
	wantChmods := []chmodCall{
		{path: filepath.Dir(destination), mode: 0o750},
		{path: destination, mode: 0o640},
	}
	if !reflect.DeepEqual(chowns, wantChowns) {
		t.Fatalf("chown calls = %+v, want %+v", chowns, wantChowns)
	}
	if !reflect.DeepEqual(chmods, wantChmods) {
		t.Fatalf("chmod calls = %+v, want %+v", chmods, wantChmods)
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
	ufw := &transactionalUFWModel{enabled: true, rules: map[string]string{"2096/tcp": "Veil panel"}}
	run := func(_ context.Context, command []string, _ time.Duration) (string, error) {
		commands = append(commands, append([]string(nil), command...))
		if len(command) > 0 && command[0] == "env" {
			return ufw.runner(context.Background(), command, 0)
		}
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

	wantNonFirewall := [][]string{
		{"systemctl", "restart", "veil.service"},
		{"systemctl", "show", "veil.service", "--property=LoadState", "--property=ActiveState", "--property=SubState", "--no-page"},
		{"journalctl", "-u", "veil.service", "--no-pager", "-n", "25", "-o", "short-iso"},
		{"systemctl", "restart", "veil.service"},
	}
	var nonFirewall [][]string
	for _, command := range commands {
		if len(command) == 0 || command[0] != "env" {
			nonFirewall = append(nonFirewall, command)
		}
	}
	if !reflect.DeepEqual(nonFirewall, wantNonFirewall) {
		t.Fatalf("non-firewall commands:\nwant=%v\ngot=%v", wantNonFirewall, nonFirewall)
	}
}

func TestProductionExecutorFirewallReloadsAfterApplyingRules(t *testing.T) {
	model := &transactionalUFWModel{enabled: true, rules: map[string]string{"2096/tcp": "Veil Panel"}}
	run := func(_ context.Context, command []string, _ time.Duration) (string, error) {
		return model.runner(context.Background(), command, 0)
	}
	executor := NewProductionExecutor(ProductionConfig{RunCommand: run})

	firewall, err := executor.Firewall(context.Background(), ResolvedFirewall{Rules: []FirewallRule{
		{Command: "ufw", Args: []string{"allow", "2096/tcp", "comment", "Veil Panel"}},
		{Command: "ufw", Args: []string{"allow", "23456/udp", "comment", "Veil Hysteria2"}},
	}})
	if err != nil {
		t.Fatalf("firewall: %v", err)
	}
	if !reflect.DeepEqual(firewall.AppliedRuleIDs, []string{"dynamic:2096/tcp", "dynamic:23456/udp"}) {
		t.Fatalf("unexpected applied ids: %+v", firewall)
	}
	foundReload := false
	for _, mutation := range model.mutations {
		foundReload = foundReload || mutation == "reload"
	}
	if !foundReload {
		t.Fatalf("active UFW was not reloaded: %v", model.mutations)
	}
}

func TestProductionExecutorReloadFallsBackToStartWhenInactive(t *testing.T) {
	var commands [][]string
	run := func(_ context.Context, command []string, _ time.Duration) (string, error) {
		commands = append(commands, append([]string(nil), command...))
		if len(command) >= 3 && command[0] == "systemctl" && command[1] == "is-active" {
			if command[len(command)-1] == "inactive-unit.service" {
				return "inactive\n", fmt.Errorf("exit status 3")
			}
			return "active\n", nil
		}
		return "", nil
	}
	executor := NewProductionExecutor(ProductionConfig{RunCommand: run})

	if err := executor.ServiceAction(context.Background(), ServiceActionRequest{Unit: "active-unit.service", Action: ServiceActionReload}); err != nil {
		t.Fatalf("reload active unit: %v", err)
	}
	if err := executor.ServiceAction(context.Background(), ServiceActionRequest{Unit: "inactive-unit.service", Action: ServiceActionReload}); err != nil {
		t.Fatalf("reload inactive unit: %v", err)
	}

	want := [][]string{
		{"systemctl", "is-active", "active-unit.service"},
		{"systemctl", "reload", "active-unit.service"},
		{"systemctl", "is-active", "inactive-unit.service"},
		{"systemctl", "start", "inactive-unit.service"},
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

	request := ResolvedUpdate{
		ArtifactID:    "veil-update",
		Version:       "v0.6.0",
		Path:          archivePath,
		ChecksumsPath: checksumsPath,
	}
	writePrivilegedTestUpdateEvidence(t, filepath.Dir(archivePath), &request)
	executor := NewProductionExecutor(ProductionConfig{
		BinaryPath:      currentPath,
		ReleaseVerifier: func(releaseverify.Evidence) error { return nil },
	})
	result, err := executor.Update(context.Background(), request)
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

	request := ResolvedUpdate{
		ArtifactID:    "veil-update",
		Version:       "v0.6.0",
		Path:          archivePath,
		ChecksumsPath: checksumsPath,
	}
	writePrivilegedTestUpdateEvidence(t, filepath.Dir(archivePath), &request)
	executor := NewProductionExecutor(ProductionConfig{
		BinaryPath:      currentPath,
		ReleaseVerifier: func(releaseverify.Evidence) error { return nil },
	})
	if _, err := executor.Update(context.Background(), request); err == nil {
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

func writePrivilegedTestUpdateEvidence(t *testing.T, directory string, request *ResolvedUpdate) {
	t.Helper()
	for name, target := range map[string]*string{
		"checksums.txt.bundle":        &request.ChecksumsBundlePath,
		"veil.provenance.json":        &request.ProvenancePath,
		"veil.provenance.json.bundle": &request.ProvenanceBundlePath,
	} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte("signed-test-evidence"), 0o600); err != nil {
			t.Fatal(err)
		}
		*target = path
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

func TestDefaultProductionConfig(t *testing.T) {
	root := t.TempDir()
	policy := Policy{StateRoot: root, BackupRoot: root}
	config := DefaultProductionConfig(policy, "v0.0.1")
	if config.PromotionBackupRoot != filepath.Join(root, "promotion-backups") {
		t.Fatalf("unexpected promotion backup root %q", config.PromotionBackupRoot)
	}
	if config.VeilVersion != "v0.0.1" {
		t.Fatalf("unexpected version %q", config.VeilVersion)
	}
}

func TestNewProductionExecutorFillsNilDefaults(t *testing.T) {
	config := ProductionConfig{}
	executor := NewProductionExecutor(config)
	if executor.Backup == nil || executor.Update == nil || executor.RotateKey == nil {
		t.Fatal("expected default workflows to be populated")
	}
}

func TestProductionExecutorPromotesRemoveArtifacts(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "generated", "remove.json")
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
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
		RemoveArtifacts: []ResolvedArtifact{{ID: "remove-me", Source: "", Destination: destination}},
	})
	if err != nil {
		t.Fatalf("promote remove: %v", err)
	}
	if !reflect.DeepEqual(result.RemovedArtifacts, []string{"remove-me"}) {
		t.Fatalf("unexpected result: %+v", result)
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("expected destination to be removed")
	}
}

func TestProductionExecutorPromotionNoOpForEmptyRequest(t *testing.T) {
	executor := NewProductionExecutor(ProductionConfig{})
	result, err := executor.Promote(context.Background(), ResolvedPromotion{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.BackupID != "" || len(result.WrittenArtifacts) != 0 || len(result.RemovedArtifacts) != 0 {
		t.Fatalf("expected empty result, got %+v", result)
	}
}

func TestBackupPromotionDestinationRequiresRoot(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(destination, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact := ResolvedArtifact{ID: "x", Source: "", Destination: destination}
	if _, err := backupPromotionDestination("", "id", artifact); err == nil {
		t.Fatal("expected empty backup root to fail")
	}
}

func TestRestorePromotedArtifactsHandlesMissingDestination(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "generated", "gone.json")
	backupDir := filepath.Join(root, "backups", "20260605T120000.000000000Z")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := promotionManifest{BackupID: "20260605T120000.000000000Z", Records: []promotionManifestRecord{
		{ArtifactID: "gone", Destination: destination, HadPrevious: false},
	}}
	body, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(backupDir, "manifest.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := restorePromotedArtifacts(root+"/backups", "20260605T120000.000000000Z")
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !reflect.DeepEqual(result.WrittenArtifacts, []string{"gone"}) {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestRestorePromotedArtifactsRejectsManifestMismatch(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "20260605T120000.000000000Z")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := promotionManifest{BackupID: "other"}
	body, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(backupDir, "manifest.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := restorePromotedArtifacts(root, "20260605T120000.000000000Z")
	if err == nil {
		t.Fatal("expected manifest mismatch error")
	}
}

func TestRunProductionCommand(t *testing.T) {
	output, err := runProductionCommand(context.Background(), []string{"echo", "hello world"}, time.Second)
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	if output != "hello world" {
		t.Fatalf("unexpected output %q", output)
	}
	_, err = runProductionCommand(context.Background(), []string{}, time.Second)
	if err == nil {
		t.Fatal("expected empty command to fail")
	}
	_, err = runProductionCommand(context.Background(), []string{"false"}, time.Second)
	if err == nil {
		t.Fatal("expected failing command to fail")
	}
}

func TestReadBoundedRegularFileRejectsNonRegularAndOversized(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedRegularFile(dir, 1024); err == nil {
		t.Fatal("expected directory read to fail")
	}
	big := filepath.Join(root, "big.bin")
	if err := os.WriteFile(big, bytes.Repeat([]byte("x"), 11), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedRegularFile(big, 10); err == nil {
		t.Fatal("expected oversized file to fail")
	}
}

func TestIsUFWDuplicateRule(t *testing.T) {
	if !isUFWDuplicateRule("Skipping adding existing rule") {
		t.Fatal("expected duplicate detection")
	}
	if !isUFWDuplicateRule("Rule already exists") {
		t.Fatal("expected duplicate detection")
	}
	if isUFWDuplicateRule("enabled") {
		t.Fatal("expected non-duplicate")
	}
}

func TestRunFirewallRules(t *testing.T) {
	t.Run("active ufw atomically reconciles complete managed set", func(t *testing.T) {
		model := &transactionalUFWModel{enabled: true, rules: map[string]string{"22/tcp": "OpenSSH"}}
		result, err := runFirewallRules(context.Background(), model.runner, transactionalFirewallRequest())
		if err != nil {
			t.Fatalf("firewall rules: %v", err)
		}
		if !reflect.DeepEqual(result.AppliedRuleIDs, []string{"management-ssh", "panel-https"}) {
			t.Fatalf("unexpected applied ids: %+v", result)
		}
	})
	t.Run("inactive ufw stages management access before enable", func(t *testing.T) {
		model := &transactionalUFWModel{rules: map[string]string{}}
		result, err := runFirewallRules(context.Background(), model.runner, transactionalFirewallRequest())
		if err != nil {
			t.Fatalf("firewall rules: %v", err)
		}
		if !reflect.DeepEqual(result.AppliedRuleIDs, []string{"management-ssh", "panel-https"}) || !model.enabled {
			t.Fatalf("unexpected result/state: %+v enabled=%v", result, model.enabled)
		}
		if len(model.mutations) == 0 || model.mutations[0] == "enable" {
			t.Fatalf("management access was not staged before enable: %v", model.mutations)
		}
	})
	t.Run("status failure", func(t *testing.T) {
		model := &transactionalUFWModel{rules: map[string]string{}, failAt: 1}
		_, err := runFirewallRules(context.Background(), model.runner, transactionalFirewallRequest())
		if err == nil {
			t.Fatal("expected status failure")
		}
	})
}

func TestRunProductionBackupAllActions(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	passphrasePath := filepath.Join(root, "backup.passphrase")
	backupRoot := filepath.Join(root, "backups")
	for _, dir := range []string{root, backupRoot} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	var keyArray [32]byte
	copy(keyArray[:], key)
	cipher, err := secrets.NewCipher(keyArray)
	if err != nil {
		t.Fatal(err)
	}
	stateBody, err := managementstate.NewStore(statePath, cipher).Marshal(model.ManagementSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateBody, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passphrasePath, []byte("a-very-long-passphrase-32"), 0o600); err != nil {
		t.Fatal(err)
	}
	createBackupTestDatabase(t, root)

	config := ProductionConfig{
		StatePath:            statePath,
		KeyPath:              keyPath,
		BackupPassphrasePath: passphrasePath,
		BackupRoot:           backupRoot,
		VeilVersion:          "v0.0.1",
		Now:                  func() time.Time { return time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC) },
	}

	created, err := runProductionBackup(context.Background(), config, ResolvedBackup{Action: BackupActionCreate, BackupRoot: backupRoot, StateRoot: root, StatePath: statePath, KeyPath: keyPath, BackupPassphrasePath: passphrasePath})
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	if created.ArchiveName == "" || !created.Verified {
		t.Fatalf("unexpected create result: %+v", created)
	}

	listed, err := runProductionBackup(context.Background(), config, ResolvedBackup{Action: BackupActionList, BackupRoot: backupRoot})
	if err != nil {
		t.Fatalf("list backups: %v", err)
	}
	if len(listed.Archives) != 1 {
		t.Fatalf("expected one archive, got %+v", listed.Archives)
	}

	verify, err := runProductionBackup(context.Background(), config, ResolvedBackup{Action: BackupActionVerify, ArchiveName: created.ArchiveName, ArchivePath: filepath.Join(backupRoot, created.ArchiveName), BackupPassphrasePath: passphrasePath})
	if err != nil {
		t.Fatalf("verify backup: %v", err)
	}
	if !verify.Verified {
		t.Fatal("expected backup to verify")
	}

	read, err := runProductionBackup(context.Background(), config, ResolvedBackup{Action: BackupActionRead, ArchiveName: created.ArchiveName, ArchivePath: filepath.Join(backupRoot, created.ArchiveName)})
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if len(read.Data) == 0 {
		t.Fatal("expected backup data")
	}

	restore, err := runProductionBackup(context.Background(), config, ResolvedBackup{Action: BackupActionRestore, ArchiveName: created.ArchiveName, ArchivePath: filepath.Join(backupRoot, created.ArchiveName), StatePath: statePath, KeyPath: keyPath, BackupPassphrasePath: passphrasePath})
	if err != nil {
		t.Fatalf("restore backup: %v", err)
	}
	if !restore.Restored {
		t.Fatal("expected restore to complete")
	}

	checkOnly, err := runProductionBackup(context.Background(), config, ResolvedBackup{Action: BackupActionRestore, ArchiveName: created.ArchiveName, ArchivePath: filepath.Join(backupRoot, created.ArchiveName), StatePath: statePath, KeyPath: keyPath, BackupPassphrasePath: passphrasePath, CheckOnly: true})
	if err != nil {
		t.Fatalf("restore check only: %v", err)
	}
	if checkOnly.Restored {
		t.Fatal("expected check-only to not restore")
	}

	pruned, err := runProductionBackup(context.Background(), config, ResolvedBackup{Action: BackupActionPrune, BackupRoot: backupRoot, Daily: 0, Weekly: 0, Monthly: 0})
	if err != nil {
		t.Fatalf("prune backups: %v", err)
	}
	if len(pruned.Kept) != 1 || len(pruned.Pruned) != 0 {
		t.Fatalf("unexpected prune result: kept=%v pruned=%v", pruned.Kept, pruned.Pruned)
	}
}

func TestRunProductionBackupConcurrentCreatesDoNotReplace(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	passphrasePath := filepath.Join(root, "backup.passphrase")
	backupRoot := filepath.Join(root, "backups")
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, key[:], 0o600); err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := managementstate.NewStore(statePath, cipher).Save(model.ManagementSnapshot{}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passphrasePath, []byte("a-very-long-passphrase-32"), 0o600); err != nil {
		t.Fatal(err)
	}
	createBackupTestDatabase(t, root)
	fixedNow := time.Date(2026, 6, 5, 12, 0, 0, 123456789, time.UTC)
	config := ProductionConfig{
		StatePath: statePath, KeyPath: keyPath, BackupPassphrasePath: passphrasePath,
		BackupRoot: backupRoot, VeilVersion: "v0.0.1", Now: func() time.Time { return fixedNow },
	}
	request := ResolvedBackup{
		Action: BackupActionCreate, BackupRoot: backupRoot, StateRoot: root,
		StatePath: statePath, KeyPath: keyPath, BackupPassphrasePath: passphrasePath,
	}

	start := make(chan struct{})
	results := make([]BackupResult, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			results[index], errs[index] = runProductionBackup(context.Background(), config, request)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent create %d: %v", i, err)
		}
	}
	if results[0].ArchiveName == results[1].ArchiveName {
		t.Fatalf("concurrent creates returned the same archive name %q", results[0].ArchiveName)
	}
	entries, err := os.ReadDir(backupRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("backup directory contains %d archives, want 2", len(entries))
	}
	listed, err := runProductionBackup(context.Background(), config, ResolvedBackup{Action: BackupActionList, BackupRoot: backupRoot})
	if err != nil {
		t.Fatalf("list concurrent archives: %v", err)
	}
	if len(listed.Archives) != 2 {
		t.Fatalf("retention/list parser sees %d archives, want 2", len(listed.Archives))
	}
	for _, result := range results {
		if _, err := runProductionBackup(context.Background(), config, ResolvedBackup{
			Action: BackupActionVerify, ArchiveName: result.ArchiveName,
			ArchivePath:          filepath.Join(backupRoot, result.ArchiveName),
			BackupPassphrasePath: passphrasePath,
		}); err != nil {
			t.Fatalf("verify surviving archive %q: %v", result.ArchiveName, err)
		}
	}

	explicit := request
	explicit.ArchiveName = "veil_backup_20260605_120000.tar.gz.enc"
	if _, err := runProductionBackup(context.Background(), config, explicit); err != nil {
		t.Fatalf("create explicit-name archive: %v", err)
	}
	explicitPath := filepath.Join(backupRoot, explicit.ArchiveName)
	beforeReplaceAttempt, err := os.ReadFile(explicitPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runProductionBackup(context.Background(), config, explicit); err == nil {
		t.Fatal("second create with the same explicit archive name replaced the first")
	}
	afterReplaceAttempt, err := os.ReadFile(explicitPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeReplaceAttempt, afterReplaceAttempt) {
		t.Fatal("existing archive changed after no-replace publication failure")
	}
}

func TestRunProductionBackupRejectsShortPassphrase(t *testing.T) {
	root := t.TempDir()
	passphrasePath := filepath.Join(root, "backup.passphrase")
	if err := os.WriteFile(passphrasePath, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := runProductionBackup(context.Background(), ProductionConfig{BackupPassphrasePath: passphrasePath}, ResolvedBackup{Action: BackupActionCreate})
	if err == nil {
		t.Fatal("expected short passphrase rejection")
	}
}

func TestRunProductionBackupUnsupportedAction(t *testing.T) {
	_, err := runProductionBackup(context.Background(), ProductionConfig{}, ResolvedBackup{Action: BackupAction("")})
	if err == nil {
		t.Fatal("expected unsupported action error")
	}
}

func TestReadBoundedRegularFileNotFound(t *testing.T) {
	_, err := readBoundedRegularFile(filepath.Join(t.TempDir(), "missing"), 1024)
	if err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestRunProductionUpdateRejectsMissingArchive(t *testing.T) {
	_, err := runProductionUpdate(ProductionConfig{}, ResolvedUpdate{Path: "/does/not/exist.tar.gz", ChecksumsPath: "/does/not/exist.txt"})
	if err == nil {
		t.Fatal("expected missing archive error")
	}
}

func TestPromoteResolvedArtifactsSourceReadError(t *testing.T) {
	_, err := promoteResolvedArtifacts(t.TempDir(), time.Now, ResolvedPromotion{
		Artifacts: []ResolvedArtifact{{ID: "missing", Source: "/does/not/exist", Destination: filepath.Join(t.TempDir(), "dst")}},
	})
	if err == nil {
		t.Fatal("expected source read error")
	}
}

// TestPromoteResolvedArtifactsRejectsSymlinkedSource covers the promote-time
// exfiltration vector: a staging source swapped for a symlink after policy
// resolution must not be read into the generated root.
func TestPromoteResolvedArtifactsRejectsSymlinkedSource(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "staging", "cfg.json")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, source); err != nil {
		t.Fatal(err)
	}
	_, err := promoteResolvedArtifacts(filepath.Join(root, "backups"), time.Now, ResolvedPromotion{
		Artifacts: []ResolvedArtifact{{ID: "cfg", Source: source, Destination: filepath.Join(root, "gen", "cfg.json")}},
	})
	if err == nil {
		t.Fatal("expected symlinked source to be rejected")
	}
}

func TestBackupPromotionDestinationReadError(t *testing.T) {
	artifact := ResolvedArtifact{ID: "x", Source: "", Destination: filepath.Join(t.TempDir(), "dir")}
	if err := os.MkdirAll(artifact.Destination, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := backupPromotionDestination(t.TempDir(), "id", artifact); err == nil {
		t.Fatal("expected destination read error")
	}
}

func TestRestorePromotedArtifactsRejectsCorruptManifest(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "20260605T120000.000000000Z")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "manifest.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := restorePromotedArtifacts(root, "20260605T120000.000000000Z")
	if err == nil {
		t.Fatal("expected corrupt manifest error")
	}
}

func TestRestorePromotedArtifactsBackupReadError(t *testing.T) {
	root := t.TempDir()
	backupDir := filepath.Join(root, "20260605T120000.000000000Z")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := promotionManifest{BackupID: "20260605T120000.000000000Z", Records: []promotionManifestRecord{
		{ArtifactID: "x", Destination: filepath.Join(root, "dest"), HadPrevious: true, BackupPath: filepath.Join(backupDir, "missing")},
	}}
	body, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(backupDir, "manifest.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := restorePromotedArtifacts(root, "20260605T120000.000000000Z")
	if err == nil {
		t.Fatal("expected backup read error")
	}
}

func TestRunProductionBackupCreateUsesDefaultArchiveName(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	passphrasePath := filepath.Join(root, "backup.passphrase")
	backupRoot := filepath.Join(root, "backups")
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	var keyArray [32]byte
	copy(keyArray[:], key)
	cipher, err := secrets.NewCipher(keyArray)
	if err != nil {
		t.Fatal(err)
	}
	stateBody, err := managementstate.NewStore(statePath, cipher).Marshal(model.ManagementSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, stateBody, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passphrasePath, []byte("a-very-long-passphrase-32"), 0o600); err != nil {
		t.Fatal(err)
	}
	createBackupTestDatabase(t, root)

	config := ProductionConfig{
		StatePath:            statePath,
		KeyPath:              keyPath,
		BackupPassphrasePath: passphrasePath,
		BackupRoot:           backupRoot,
		VeilVersion:          "v0.0.1",
		Now:                  func() time.Time { return time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC) },
	}
	result, err := runProductionBackup(context.Background(), config, ResolvedBackup{Action: BackupActionCreate, StatePath: statePath, KeyPath: keyPath, BackupPassphrasePath: passphrasePath, BackupRoot: backupRoot})
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	if result.ArchiveName == "" {
		t.Fatal("expected default archive name")
	}
}

func TestOpenRegularNoFollowRejectsMissingPath(t *testing.T) {
	_, err := openRegularNoFollow(filepath.Join(t.TempDir(), "missing"))
	if err == nil {
		t.Fatal("expected missing path error")
	}
}

func TestRunProductionUpdateRejectsMissingChecksums(t *testing.T) {
	root := t.TempDir()
	archivePath := filepath.Join(root, "archive.tar.gz")
	if err := os.WriteFile(archivePath, []byte("archive"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := runProductionUpdate(ProductionConfig{}, ResolvedUpdate{Path: archivePath, ChecksumsPath: filepath.Join(root, "missing.txt")})
	if err == nil {
		t.Fatal("expected missing checksums error")
	}
}

func TestRunProductionBackupVerifyRejectsMissingPassphrase(t *testing.T) {
	root := t.TempDir()
	archive := filepath.Join(root, "daily.enc")
	if err := os.WriteFile(archive, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := runProductionBackup(context.Background(), ProductionConfig{}, ResolvedBackup{Action: BackupActionVerify, ArchiveName: "daily.enc", ArchivePath: archive})
	if err == nil {
		t.Fatal("expected missing passphrase error")
	}
}

func TestNewProductionExecutorFirewallRejectsMissingCommand(t *testing.T) {
	executor := NewProductionExecutor(ProductionConfig{
		RunCommand: func(context.Context, []string, time.Duration) (string, error) { return "", nil },
	})
	_, err := executor.Firewall(context.Background(), ResolvedFirewall{RuleIDs: []string{"missing"}})
	if err == nil {
		t.Fatal("expected missing firewall command error")
	}
}

func TestRunProductionUpdateRejectsCorruptArchiveAfterChecksum(t *testing.T) {
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
	archive := []byte("not-a-valid-archive")
	hash := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(hash[:]), updateflow.AssetName())
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(checksumsPath, []byte(checksums), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := runProductionUpdate(ProductionConfig{BinaryPath: currentPath}, ResolvedUpdate{
		ArtifactID:    "veil-update",
		Version:       "v0.6.0",
		Path:          archivePath,
		ChecksumsPath: checksumsPath,
	})
	if err == nil {
		t.Fatal("expected corrupt archive install error")
	}
	body, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "old-binary" {
		t.Fatalf("binary changed after rejected update: %q", body)
	}
}

func TestRunProductionBackupListAndPruneReturnErrors(t *testing.T) {
	root := t.TempDir()
	notDir := filepath.Join(root, "file")
	if err := os.WriteFile(notDir, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := runProductionBackup(context.Background(), ProductionConfig{}, ResolvedBackup{Action: BackupActionList, BackupRoot: notDir})
	if err == nil {
		t.Fatal("expected list error for file path")
	}
	_, err = runProductionBackup(context.Background(), ProductionConfig{}, ResolvedBackup{Action: BackupActionPrune, BackupRoot: notDir})
	if err == nil {
		t.Fatal("expected prune error for file path")
	}
}

func TestRunProductionBackupCreateRejectsMissingPassphrase(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "state.json")
	keyPath := filepath.Join(root, "state.key")
	if err := os.WriteFile(statePath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := runProductionBackup(context.Background(), ProductionConfig{}, ResolvedBackup{Action: BackupActionCreate, StatePath: statePath, KeyPath: keyPath})
	if err == nil {
		t.Fatal("expected missing passphrase error")
	}
}
