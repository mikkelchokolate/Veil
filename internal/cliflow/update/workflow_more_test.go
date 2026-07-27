package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newValidWorkflowDeps(t *testing.T) (WorkflowDependencies, []byte) {
	t.Helper()
	archive := createTestTarGz(t, "veil", []byte("new-binary"))
	hash := sha256.Sum256(archive)
	assetName := AssetName()
	checksums := []byte(fmt.Sprintf("%s  %s\n", hex.EncodeToString(hash[:]), assetName))
	release := &Release{TagName: "v1.2.4", Assets: append([]Asset{
		{Name: assetName, BrowserDownloadURL: "https://example.com/archive"},
		{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums"},
	}, testReleaseEvidenceAssets()...)}
	return WorkflowDependencies{
		FetchRelease:          func() (*Release, error) { return release, nil },
		VerifyReleaseEvidence: acceptTestReleaseEvidence,
		DownloadAsset: func(url string) ([]byte, error) {
			if strings.Contains(url, "checksums") {
				return checksums, nil
			}
			return archive, nil
		},
		Executable:               func() (string, error) { return filepath.Join(t.TempDir(), "veil"), nil },
		ReplaceBinaryFromArchive: ReplaceBinaryFromArchive,
		RestartUpdated:           nil,
	}, archive
}

func TestRunWorkflowRequiresFetchRelease(t *testing.T) {
	var out bytes.Buffer
	err := RunWorkflow(WorkflowOptions{}, &out, WorkflowDependencies{DownloadAsset: func(string) ([]byte, error) { return nil, nil }})
	if err == nil {
		t.Fatal("expected error when FetchRelease is nil")
	}
}

func TestRunWorkflowRequiresDownloadAsset(t *testing.T) {
	var out bytes.Buffer
	err := RunWorkflow(WorkflowOptions{}, &out, WorkflowDependencies{FetchRelease: func() (*Release, error) { return &Release{}, nil }})
	if err == nil {
		t.Fatal("expected error when DownloadAsset is nil")
	}
}

func TestRunWorkflowReturnsFetchError(t *testing.T) {
	var out bytes.Buffer
	deps := WorkflowDependencies{
		FetchRelease:  func() (*Release, error) { return nil, errors.New("api down") },
		DownloadAsset: func(string) ([]byte, error) { return nil, errors.New("should not download") },
	}
	err := RunWorkflow(WorkflowOptions{}, &out, deps)
	if err == nil {
		t.Fatal("expected fetch error")
	}
}

func TestRunWorkflowSkipsWhenCurrentIsNewer(t *testing.T) {
	var out bytes.Buffer
	deps, _ := newValidWorkflowDeps(t)
	err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.3.0"}, &out, deps)
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	if !strings.Contains(out.String(), "is newer than latest release") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunWorkflowSkipsWhenAlreadyLatest(t *testing.T) {
	var out bytes.Buffer
	deps, _ := newValidWorkflowDeps(t)
	err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.2.4"}, &out, deps)
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	if !strings.Contains(out.String(), "already at the latest version") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunWorkflowForcesReinstallWhenCurrentIsNewer(t *testing.T) {
	var out bytes.Buffer
	deps, _ := newValidWorkflowDeps(t)
	err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.3.0", Force: true, Yes: true}, &out, deps)
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	if !strings.Contains(out.String(), "Updated to v1.2.4") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunWorkflowUpdatesWhenOlder(t *testing.T) {
	var out bytes.Buffer
	deps, _ := newValidWorkflowDeps(t)
	err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.2.3", Yes: true}, &out, deps)
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	if !strings.Contains(out.String(), "Updated to v1.2.4") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunWorkflowDryRun(t *testing.T) {
	var out bytes.Buffer
	deps, _ := newValidWorkflowDeps(t)
	err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.2.3", DryRun: true}, &out, deps)
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	if !strings.Contains(out.String(), "Dry run") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunWorkflowReturnsDownloadError(t *testing.T) {
	var out bytes.Buffer
	deps, _ := newValidWorkflowDeps(t)
	deps.DownloadAsset = func(string) ([]byte, error) { return nil, errors.New("download failed") }
	err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.2.3"}, &out, deps)
	if err == nil {
		t.Fatal("expected download error")
	}
}

func TestRunWorkflowFallsBackToDefaultExecutable(t *testing.T) {
	var out bytes.Buffer
	deps, _ := newValidWorkflowDeps(t)
	deps.Executable = func() (string, error) { return "", errors.New("cannot find executable") }
	// The default-path fallback must run against an isolated path: a unit test
	// must never write to the production /usr/local/bin location.
	fallback := filepath.Join(t.TempDir(), "veil")
	deps.DefaultExecutablePath = fallback
	err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.2.3", Yes: true}, &out, deps)
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	if !strings.Contains(out.String(), fallback) {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunWorkflowReturnsReplaceError(t *testing.T) {
	var out bytes.Buffer
	deps, _ := newValidWorkflowDeps(t)
	deps.ReplaceBinaryFromArchive = func(string, []byte, bool) (string, error) { return "", errors.New("replace failed") }
	err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.2.3", Yes: true}, &out, deps)
	if err == nil {
		t.Fatal("expected replace error")
	}
}

func TestRunWorkflowRequiresRestartFlowWhenRestartRequested(t *testing.T) {
	var out bytes.Buffer
	deps, _ := newValidWorkflowDeps(t)
	deps.Executable = func() (string, error) {
		path := filepath.Join(t.TempDir(), "veil")
		_ = os.WriteFile(path, []byte("old"), 0o755)
		return path, nil
	}
	err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.2.3", Yes: true, Restart: true}, &out, deps)
	if err == nil {
		t.Fatal("expected error when RestartUpdated is nil")
	}
}

func TestRunWorkflowRestartsWhenRequested(t *testing.T) {
	var out bytes.Buffer
	deps, _ := newValidWorkflowDeps(t)
	deps.Executable = func() (string, error) {
		path := filepath.Join(t.TempDir(), "veil")
		_ = os.WriteFile(path, []byte("old"), 0o755)
		return path, nil
	}
	deps.RestartUpdated = func(currentPath, backupPath string, opts WorkflowOptions) error {
		if opts.Restart != true {
			return errors.New("restart flag not propagated")
		}
		return nil
	}
	err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.2.3", Yes: true, Restart: true}, &out, deps)
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	if !strings.Contains(out.String(), "Updated to v1.2.4") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunWorkflowStagedFlagTriggersRestartFlow(t *testing.T) {
	var out bytes.Buffer
	deps, _ := newValidWorkflowDeps(t)
	deps.Executable = func() (string, error) {
		path := filepath.Join(t.TempDir(), "veil")
		_ = os.WriteFile(path, []byte("old"), 0o755)
		return path, nil
	}
	called := false
	deps.RestartUpdated = func(currentPath, backupPath string, opts WorkflowOptions) error {
		called = true
		if opts.Staged != true {
			return errors.New("staged flag not propagated")
		}
		return nil
	}
	err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.2.3", Yes: true, Staged: true}, &out, deps)
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	if !called {
		t.Fatal("RestartUpdated was not called")
	}
}

func TestRunWorkflowPrintsRestartHintWhenNotRestarting(t *testing.T) {
	var out bytes.Buffer
	deps, _ := newValidWorkflowDeps(t)
	err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.2.3", Yes: true}, &out, deps)
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	if !strings.Contains(out.String(), "systemctl restart") {
		t.Fatalf("output = %q", out.String())
	}
}
