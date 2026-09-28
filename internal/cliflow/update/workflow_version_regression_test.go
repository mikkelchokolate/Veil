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

// TestRunWorkflowRefusesNonReleaseBuildWithoutForce is the CLI half of
// #1104: a main-<sha> install-main build (or any non-release stamp) does not
// order against release tags — Compare sorts it before every tag, so the old
// code happily "updated" a newer source build to the latest release and could
// strand the panel on an older DB schema. Without --force it must refuse.
func TestRunWorkflowRefusesNonReleaseBuildWithoutForce(t *testing.T) {
	for _, current := range []string{"main-abcdef1", "dev"} {
		var out bytes.Buffer
		downloaded := false
		deps, _ := newValidWorkflowDeps(t)
		origDownload := deps.DownloadAsset
		deps.DownloadAsset = func(url string) ([]byte, error) {
			downloaded = true
			return origDownload(url)
		}
		if err := RunWorkflow(WorkflowOptions{CurrentVersion: current}, &out, deps); err != nil {
			t.Fatalf("RunWorkflow(%q): %v", current, err)
		}
		got := out.String()
		if downloaded || strings.Contains(got, "Updating") {
			t.Fatalf("non-release build %q proceeded to update without --force:\n%s", current, got)
		}
		if !strings.Contains(got, "not a release build") || !strings.Contains(got, "--force") {
			t.Fatalf("non-release build %q missing refusal guidance:\n%s", current, got)
		}
	}
}

// TestRunWorkflowForcesNonReleaseBuild covers the --force escape hatch: the
// operator may intentionally replace a main-<sha> build with a release.
func TestRunWorkflowForcesNonReleaseBuild(t *testing.T) {
	var out bytes.Buffer
	downloaded := false
	deps, _ := newValidWorkflowDeps(t)
	origDownload := deps.DownloadAsset
	deps.DownloadAsset = func(url string) ([]byte, error) {
		downloaded = true
		return origDownload(url)
	}
	deps.ReplaceBinaryFromArchive = func(currentPath string, archive []byte, yes bool) (string, error) {
		return currentPath + ".backup", nil
	}
	if err := RunWorkflow(WorkflowOptions{CurrentVersion: "main-abcdef1", Force: true, Yes: true}, &out, deps); err != nil {
		t.Fatalf("RunWorkflow forced: %v", err)
	}
	if !downloaded {
		t.Fatal("forced non-release update did not download the release")
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
