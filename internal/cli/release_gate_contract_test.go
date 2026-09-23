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
	// #668: the quality leg runs the shared frontend job (frontend.sh is the
	// superset that also leaves web/dist); the other legs still call
	// prepare-frontend-dist.sh directly.
	markers := []string{"scripts/ci/prepare-frontend-dist.sh", "scripts/ci/frontend.sh"}
	count := 0
	dist := -1
	for _, marker := range markers {
		count += strings.Count(workflow, marker)
		if i := strings.Index(workflow, marker); i >= 0 && (dist < 0 || i < dist) {
			dist = i
		}
	}
	if count < 4 {
		t.Fatalf("release workflow must build web/dist in quality, browser-e2e, package-smoke, and package jobs; found %d", count)
	}
	vet := strings.Index(workflow, "go vet ./...")
	if dist < 0 || vet < 0 || dist > vet {
		t.Fatalf("frontend dist must be built before go vet so //go:embed all:dist succeeds")
	}
	for _, want := range []string{
		"scripts/ci/e2e.sh",
		"scripts/ci/browser-e2e.sh",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release workflow missing %q", want)
		}
	}
	// The veil system user is created by the shared e2e script (single source
	// of truth for PR CI and release) — the workflow no longer needs its own.
	e2eBody, err := os.ReadFile("../../scripts/ci/e2e.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ReplaceAll(string(e2eBody), "\r\n", "\n"), "useradd --system --user-group --no-create-home --shell /usr/sbin/nologin veil") {
		t.Error("scripts/ci/e2e.sh missing veil system user creation")
	}
}
