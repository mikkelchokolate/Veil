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

func TestReleaseWorkflowBuildsFrontendDistBeforeCompile(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := strings.ReplaceAll(string(body), "\r\n", "\n")
	marker := "scripts/ci/prepare-frontend-dist.sh"
	if strings.Count(workflow, marker) < 4 {
		t.Fatalf("release workflow must build web/dist in quality, browser-e2e, package-smoke, and package jobs; found %d", strings.Count(workflow, marker))
	}
	vet := strings.Index(workflow, "go vet ./...")
	dist := strings.Index(workflow, marker)
	if dist < 0 || vet < 0 || dist > vet {
		t.Fatalf("frontend dist must be built before go vet so //go:embed all:dist succeeds")
	}
}
