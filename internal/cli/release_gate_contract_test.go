package cli

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseWorkflowRequiresBrowserAndPackageGates(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{
		"browser-e2e:",
		"package-smoke:",
		"needs: [quality, browser-e2e, package-smoke]",
		"needs: [quality, browser-e2e, package-smoke, release, docker-publish]",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow missing production gate %q", want)
		}
	}
}
