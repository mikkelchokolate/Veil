package update

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunWorkflowUpdatesReleaseCandidateToStable(t *testing.T) {
	var out bytes.Buffer
	deps, _ := newValidWorkflowDeps(t)
	downloaded := false
	origDownload := deps.DownloadAsset
	deps.DownloadAsset = func(url string) ([]byte, error) {
		downloaded = true
		return origDownload(url)
	}
	deps.ReplaceBinaryFromArchive = func(currentPath string, archive []byte, yes bool) (string, error) {
		return currentPath + ".backup", nil
	}

	err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.2.4-rc.1", Yes: true}, &out, deps)
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "already at the latest version") || strings.Contains(got, "newer than latest release") {
		t.Fatalf("release candidate was treated as current or newer than stable:\n%s", got)
	}
	if !downloaded {
		t.Fatal("RC-to-stable update was skipped")
	}
	if !strings.Contains(got, "Updating v1.2.4-rc.1 → v1.2.4") {
		t.Fatalf("output = %q", got)
	}
}

func TestRunWorkflowTreatsReleaseDisplayVersionAsLatest(t *testing.T) {
	var out bytes.Buffer
	display := "v1.2.3 (" + strings.Repeat("a", 40) + ")"
	if err := RunWorkflow(WorkflowOptions{CurrentVersion: display}, &out, WorkflowDependencies{
		FetchRelease: func() (*Release, error) { return &Release{TagName: "v1.2.3"}, nil },
		DownloadAsset: func(url string) ([]byte, error) {
			t.Fatalf("already-latest release display version must not download assets")
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "already at the latest version") {
		t.Fatalf("output = %s", got)
	}
	if strings.Contains(got, "Updating") {
		t.Fatalf("treated current release as outdated:\n%s", got)
	}
}
