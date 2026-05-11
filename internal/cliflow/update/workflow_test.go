package update

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunUpdateWorkflowSkipsAlreadyLatestWithoutForce(t *testing.T) {
	var out bytes.Buffer

	if err := RunWorkflow(WorkflowOptions{CurrentVersion: "v1.2.3"}, &out, WorkflowDependencies{
		FetchRelease: func() (*Release, error) { return &Release{TagName: "v1.2.3"}, nil },
		DownloadAsset: func(url string) ([]byte, error) {
			t.Fatalf("already-latest update must not download assets")
			return nil, nil
		},
	}); err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	got := out.String()
	for _, want := range []string{"Latest release: v1.2.3", "Veil is already at the latest version (v1.2.3).", "Use --force to reinstall anyway."} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}
