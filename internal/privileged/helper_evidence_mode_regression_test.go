package privileged

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
	"github.com/mikkelchokolate/Veil/internal/releaseverify"
)

func TestRestartHelperEvidenceIsReadableByNonRoot(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "veil")
	if err := os.WriteFile(binaryPath, []byte("panel-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	var showCalls int
	executor := NewProductionExecutor(ProductionConfig{
		BinaryPath: binaryPath,
		RunCommand: func(_ context.Context, command []string, _ time.Duration) (string, error) {
			if len(command) > 1 && command[0] == "systemctl" && command[1] == "show" {
				showCalls++
				return fmt.Sprintf("LoadState=loaded\nActiveState=active\nSubState=running\nMainPID=%d\nExecMainStartTimestampMonotonic=%d\n", showCalls, showCalls), nil
			}
			return "", nil
		},
	})
	ctx := ContextWithRestartPanelRequest(context.Background(), RestartPanelRequest{
		Fence: FenceToken{Owner: "panel", Generation: 1, OperationID: "restart-op", LeaseExpiresAt: time.Now().Add(time.Minute).Unix()},
	})
	if err := executor.RestartPanel(ctx); err != nil {
		t.Fatalf("restart Panel: %v", err)
	}
	manifestPath := filepath.Join(dir, ".veil-restart-evidence.json")
	assertHelperEvidenceReadable(t, manifestPath)

	// The committed receipt must carry the exact evidence the restart
	// transaction gathered, not merely be a readable JSON file.
	binaryDigest := sha256.Sum256([]byte("panel-binary"))
	var evidence map[string]any
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &evidence); err != nil {
		t.Fatalf("restart evidence is not JSON: %v", err)
	}
	want := map[string]any{
		"version":                  float64(1),
		"transactionId":            "restart-op",
		"expectedExecutableDigest": hex.EncodeToString(binaryDigest[:]),
		// The fake systemctl show returns MainPID/Monotonic equal to the call
		// number: generation 1 before restart, generation 2 after.
		"previousStartGeneration": float64(1),
		"newStartGeneration":      float64(2),
		"mainPid":                 float64(2),
		"serviceActive":           true,
		"activationManifest":      manifestPath,
		"commitPhase":             "committed",
	}
	for key, wantValue := range want {
		if got := evidence[key]; got != wantValue {
			t.Errorf("restart evidence %s = %#v, want %#v (body %s)", key, got, wantValue, body)
		}
	}
}

func TestUpdateHelperEvidenceIsReadableByNonRoot(t *testing.T) {
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
		ArtifactID: "veil-update", Version: "v0.6.0", Path: archivePath, ChecksumsPath: checksumsPath,
	}
	writePrivilegedTestUpdateEvidence(t, filepath.Dir(archivePath), &request)
	executor := NewProductionExecutor(ProductionConfig{
		BinaryPath:      currentPath,
		ReleaseVerifier: func(releaseverify.Evidence) error { return nil },
	})
	if _, err := executor.Update(context.Background(), request); err != nil {
		t.Fatalf("install staged update: %v", err)
	}
	manifestPath := filepath.Join(root, ".veil-update-evidence.json")
	assertHelperEvidenceReadable(t, manifestPath)

	// Assert the exact committed receipt fields — digests of the old and new
	// binaries, target version, and the committed phase — not just that some
	// readable JSON exists.
	newDigest := sha256.Sum256([]byte("new-binary"))
	oldDigest := sha256.Sum256([]byte("old-binary"))
	body, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var evidence map[string]any
	if err := json.Unmarshal(body, &evidence); err != nil {
		t.Fatalf("update evidence is not JSON: %v", err)
	}
	want := map[string]any{
		"version":              float64(1),
		"expectedBinaryDigest": hex.EncodeToString(newDigest[:]),
		"oldBinaryDigest":      hex.EncodeToString(oldDigest[:]),
		"targetVersion":        "v0.6.0",
		"activationManifest":   manifestPath,
		"commitPhase":          "committed",
	}
	for key, wantValue := range want {
		if got := evidence[key]; got != wantValue {
			t.Errorf("update evidence %s = %#v, want %#v (body %s)", key, got, wantValue, body)
		}
	}
	// transactionId is generated when the request carries no fence; it must
	// still be a durable 16-byte hex token, and installedPathInode must be
	// populated ("unknown" is the documented fallback off Linux).
	if tx, _ := evidence["transactionId"].(string); len(tx) != 32 || strings.Trim(tx, "0123456789abcdef") != "" {
		t.Errorf("update evidence transactionId = %#v, want 32-char hex", evidence["transactionId"])
	}
	if inode, _ := evidence["installedPathInode"].(string); inode == "" {
		t.Error("update evidence omits installedPathInode")
	}
}

func assertHelperEvidenceReadable(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat helper evidence: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("helper evidence is not a regular file: %s", info.Mode())
	}
	if info.Mode().Perm()&0o022 != 0 {
		t.Fatalf("helper evidence is group/other-writable: %o", info.Mode().Perm())
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o044 == 0 {
		t.Fatalf("helper evidence mode %o is unreadable by the Panel user", info.Mode().Perm())
	}
	if _, err := os.ReadFile(path); err != nil {
		t.Fatalf("helper evidence is unreadable: %v", err)
	}
}
