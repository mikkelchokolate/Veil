package cli

import (
	"bytes"
	"strings"
	"testing"

	updateflow "github.com/veil-panel/veil/internal/cliflow/update"
)

func TestRunUpdateWorkflowSkipsAlreadyLatestWithoutForce(t *testing.T) {
	oldReleaseFetcher := updateReleaseFetcher
	oldAssetDownloader := updateAssetDownloader
	updateReleaseFetcher = func() (*updateflow.Release, error) {
		return &updateflow.Release{TagName: "v1.2.3"}, nil
	}
	updateAssetDownloader = func(url string) ([]byte, error) {
		t.Fatalf("already-latest update must not download assets")
		return nil, nil
	}
	t.Cleanup(func() {
		updateReleaseFetcher = oldReleaseFetcher
		updateAssetDownloader = oldAssetDownloader
	})

	cmd := NewRootCommand("v1.2.3")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	if err := runUpdateWorkflow(cmd, updateWorkflowOptions{CurrentVersion: "v1.2.3"}); err != nil {
		t.Fatalf("runUpdateWorkflow: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Latest release: v1.2.3", "Veil is already at the latest version (v1.2.3).", "Use --force to reinstall anyway."} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}
