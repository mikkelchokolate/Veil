package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
)

// mockUpdateTransport routes HTTP requests to a handler without binding a real port.
type mockUpdateTransport struct {
	handler http.Handler
}

func (t *mockUpdateTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}

type mockUpdateHandler struct {
	assetBody       []byte
	checksums       string
	releaseStatus   int
	assetStatus     int
	checksumsStatus int
}

func (h *mockUpdateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/releases/latest"):
		w.Header().Set("Content-Type", "application/json")
		if h.releaseStatus != 0 {
			w.WriteHeader(h.releaseStatus)
			return
		}
		assetName := updateflow.AssetName()
		rel := map[string]interface{}{
			"tag_name": "v2.0.0",
			"assets": []map[string]string{
				{"name": assetName, "browser_download_url": "https://api.github.com/assets/" + assetName},
				{"name": "checksums.txt", "browser_download_url": "https://api.github.com/assets/checksums.txt"},
			},
		}
		_ = json.NewEncoder(w).Encode(rel)
	case strings.Contains(r.URL.Path, "/assets/") && strings.Contains(r.URL.Path, "checksums"):
		if h.checksumsStatus != 0 {
			w.WriteHeader(h.checksumsStatus)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(h.checksums))
	case strings.Contains(r.URL.Path, "/assets/"):
		if h.assetStatus != 0 {
			w.WriteHeader(h.assetStatus)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(h.assetBody)
	default:
		http.NotFound(w, r)
	}
}

func mockUpdateClient(t *testing.T, h *mockUpdateHandler) {
	t.Helper()
	old := updateHTTPClient
	updateHTTPClient = &http.Client{
		Timeout:   30 * time.Second,
		Transport: &mockUpdateTransport{handler: h},
	}
	t.Cleanup(func() { updateHTTPClient = old })
}

func TestFetchLatestReleaseReturnsRelease(t *testing.T) {
	assetBody := []byte("fake archive")
	hash := sha256.Sum256(assetBody)
	mockUpdateClient(t, &mockUpdateHandler{
		assetBody: assetBody,
		checksums: fmt.Sprintf("%s  %s\n", hex.EncodeToString(hash[:]), updateflow.AssetName()),
	})

	rel, err := fetchLatestRelease()
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if rel.TagName != "v2.0.0" {
		t.Fatalf("tag = %q, want v2.0.0", rel.TagName)
	}
	foundAsset := false
	for _, a := range rel.Assets {
		if a.Name == updateflow.AssetName() {
			foundAsset = true
		}
	}
	if !foundAsset {
		t.Fatalf("release missing asset %s", updateflow.AssetName())
	}
}

func TestFetchLatestReleaseReturnsErrorOnNonOK(t *testing.T) {
	mockUpdateClient(t, &mockUpdateHandler{releaseStatus: http.StatusServiceUnavailable})
	if _, err := fetchLatestRelease(); err == nil {
		t.Fatal("expected error for non-OK GitHub API response")
	}
}

func TestDownloadAssetReturnsBody(t *testing.T) {
	want := []byte("downloaded asset body")
	mockUpdateClient(t, &mockUpdateHandler{assetBody: want})

	got, err := downloadAsset("https://api.github.com/assets/" + updateflow.AssetName())
	if err != nil {
		t.Fatalf("downloadAsset: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDownloadAssetReturnsErrorOnNonOK(t *testing.T) {
	mockUpdateClient(t, &mockUpdateHandler{assetStatus: http.StatusNotFound})
	if _, err := downloadAsset("https://api.github.com/assets/" + updateflow.AssetName()); err == nil {
		t.Fatal("expected error for non-OK asset response")
	}
}

func TestUpdateCommandDryRunFetchesReleaseAndVerifiesAsset(t *testing.T) {
	assetBody := []byte("fake archive for command")
	hash := sha256.Sum256(assetBody)
	mockUpdateClient(t, &mockUpdateHandler{
		assetBody: assetBody,
		checksums: fmt.Sprintf("%s  %s\n", hex.EncodeToString(hash[:]), updateflow.AssetName()),
	})

	cmd := NewRootCommand("v1.0.0")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"update", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("update --dry-run: %v\n%s", err, out.String())
	}

	got := out.String()
	for _, want := range []string{
		"Latest release: v2.0.0",
		"Updating v1.0.0 \u2192 v2.0.0",
		"Checksum verified.",
		"Dry run: would extract",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestRestartUpdatedVeilSucceedsWhenHealthy(t *testing.T) {
	oldRestart := runSystemctlRestart
	oldHealth := updateHealthChecker
	runSystemctlRestart = func(string) error { return nil }
	updateHealthChecker = func(string, string, time.Duration) error { return nil }
	t.Cleanup(func() {
		runSystemctlRestart = oldRestart
		updateHealthChecker = oldHealth
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	dir := t.TempDir()
	current := filepath.Join(dir, "veil")
	backup := current + ".backup"
	_ = os.WriteFile(current, []byte("new"), 0o755)
	_ = os.WriteFile(backup, []byte("old"), 0o755)

	if err := restartUpdatedVeil(cmd, current, backup, updateflow.WorkflowOptions{Restart: true}); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !strings.Contains(out.String(), "Service healthy") {
		t.Fatalf("missing healthy message:\n%s", out.String())
	}
}

func TestRestartUpdatedVeilFailsHealthCheckWithoutStaged(t *testing.T) {
	oldRestart := runSystemctlRestart
	oldHealth := updateHealthChecker
	runSystemctlRestart = func(string) error { return nil }
	updateHealthChecker = func(string, string, time.Duration) error { return fmt.Errorf("unhealthy") }
	t.Cleanup(func() {
		runSystemctlRestart = oldRestart
		updateHealthChecker = oldHealth
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	dir := t.TempDir()
	current := filepath.Join(dir, "veil")
	backup := current + ".backup"
	_ = os.WriteFile(current, []byte("new"), 0o755)
	_ = os.WriteFile(backup, []byte("old"), 0o755)

	err := restartUpdatedVeil(cmd, current, backup, updateflow.WorkflowOptions{Restart: true})
	if err == nil || !strings.Contains(err.Error(), "health check failed after restart") {
		t.Fatalf("expected health check error, got %v", err)
	}
}

func TestRestartUpdatedVeilStagedHealthCheckRollsBack(t *testing.T) {
	oldRestart := runSystemctlRestart
	oldHealth := updateHealthChecker
	runSystemctlRestart = func(string) error { return nil }
	updateHealthChecker = func(string, string, time.Duration) error { return fmt.Errorf("unhealthy") }
	t.Cleanup(func() {
		runSystemctlRestart = oldRestart
		updateHealthChecker = oldHealth
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	dir := t.TempDir()
	current := filepath.Join(dir, "veil")
	backup := current + ".backup"
	_ = os.WriteFile(current, []byte("new"), 0o755)
	_ = os.WriteFile(backup, []byte("old"), 0o755)

	err := restartUpdatedVeil(cmd, current, backup, updateflow.WorkflowOptions{Staged: true})
	if err == nil || !strings.Contains(err.Error(), "health check failed, rolled back") {
		t.Fatalf("expected staged rollback error, got %v", err)
	}
	body, _ := os.ReadFile(current)
	if string(body) != "old" {
		t.Fatalf("expected old binary restored, got %q", body)
	}
	if !strings.Contains(out.String(), "Rolled back to previous binary") {
		t.Fatalf("missing rollback message:\n%s", out.String())
	}
}

func TestRestartUpdatedVeilStagedHealthCheckRollbackFailure(t *testing.T) {
	oldRestart := runSystemctlRestart
	oldHealth := updateHealthChecker
	runSystemctlRestart = func(string) error { return nil }
	updateHealthChecker = func(string, string, time.Duration) error { return fmt.Errorf("unhealthy") }
	t.Cleanup(func() {
		runSystemctlRestart = oldRestart
		updateHealthChecker = oldHealth
	})

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	dir := t.TempDir()
	current := filepath.Join(dir, "veil")
	backup := current + ".backup"
	// No backup file, so rollback fails.
	_ = os.WriteFile(current, []byte("new"), 0o755)

	err := restartUpdatedVeil(cmd, current, backup, updateflow.WorkflowOptions{Staged: true})
	if err == nil || !strings.Contains(err.Error(), "health check failed and rollback also failed") {
		t.Fatalf("expected rollback failure error, got %v", err)
	}
}

func TestRestartUpdatedVeilFailsRestartWithoutStaged(t *testing.T) {
	oldRestart := runSystemctlRestart
	runSystemctlRestart = func(string) error { return fmt.Errorf("systemctl failed") }
	t.Cleanup(func() { runSystemctlRestart = oldRestart })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	dir := t.TempDir()
	current := filepath.Join(dir, "veil")
	backup := current + ".backup"
	_ = os.WriteFile(current, []byte("new"), 0o755)
	_ = os.WriteFile(backup, []byte("old"), 0o755)

	err := restartUpdatedVeil(cmd, current, backup, updateflow.WorkflowOptions{Restart: true})
	if err == nil || !strings.Contains(err.Error(), "restart failed (binary updated") {
		t.Fatalf("expected non-staged restart error, got %v", err)
	}
}

func TestRestartUpdatedVeilStagedRestartRollbackFailure(t *testing.T) {
	oldRestart := runSystemctlRestart
	runSystemctlRestart = func(string) error { return fmt.Errorf("systemctl failed") }
	t.Cleanup(func() { runSystemctlRestart = oldRestart })

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	dir := t.TempDir()
	current := filepath.Join(dir, "veil")
	backup := current + ".backup"
	_ = os.WriteFile(current, []byte("new"), 0o755)
	// Missing backup causes rollback to fail.

	err := restartUpdatedVeil(cmd, current, backup, updateflow.WorkflowOptions{Staged: true})
	if err == nil || !strings.Contains(err.Error(), "restart failed and rollback also failed") {
		t.Fatalf("expected rollback failure error, got %v", err)
	}
}
