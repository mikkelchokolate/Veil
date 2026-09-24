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
	// needs: is parsed out of the YAML per job — a needs: line in the wrong
	// job, a quoted string, or a comment cannot satisfy the chain (#744).
	wantNeeds := map[string][]string{
		"release":        {"quality", "browser-e2e", "install-acceptance"},
		"package-smoke":  {"release"},
		"docker-publish": {"quality", "browser-e2e", "package-smoke"},
		"publish":        {"quality", "browser-e2e", "package-smoke", "release", "docker-publish"},
	}
	for job, wants := range wantNeeds {
		needs := workflowJobNeeds(t, workflow, job)
		set := map[string]bool{}
		for _, n := range needs {
			set[n] = true
		}
		for _, want := range wants {
			if !set[want] {
				t.Errorf("release.yml job %q does not need %q (needs=%v)", job, want, needs)
			}
		}
	}
	// #767: release package-smoke must validate the SHIPPED artifacts — the
	// job downloads the release job's veil-linux-<arch> artifact into
	// dist-shipped and points the smoke harness at that prebuilt dir. A
	// locally rebuilt package must not be what the smoke validates.
	smokeBlock := stripHashComments(t, workflowJobBlock(t, workflow, "package-smoke"))
	for _, want := range []string{
		"veil-linux-${{ matrix.arch }}",
		"dist-shipped",
		"VEIL_SMOKE_PREBUILT_DIR",
	} {
		if !strings.Contains(smokeBlock, want) {
			t.Errorf("release package-smoke job lost shipped-artifact binding %q", want)
		}
	}
	// #769: every release setup-go must consume the versions.sh pin through
	// steps.versions.outputs.go_version — go-version-file: go.mod would let
	// the ship toolchain drift from the PR gate pin (#455). CodeQL keeps
	// go-version-file intentionally; release.yml must not.
	stripped := stripHashComments(t, workflow)
	setupGoUses := strings.Count(stripped, "uses: actions/setup-go@")
	pinned := strings.Count(stripped, "go-version: ${{ steps.versions.outputs.go_version }}")
	if setupGoUses == 0 || pinned != setupGoUses {
		t.Errorf("release.yml must pin every actions/setup-go use to steps.versions.outputs.go_version (setup-go uses=%d, pinned=%d)", setupGoUses, pinned)
	}
	if strings.Contains(stripped, "go-version-file: go.mod") {
		t.Error("release.yml must not use go-version-file: go.mod — the ship toolchain follows scripts/ci/versions.sh")
	}
}

func TestReleaseWorkflowBuildsFrontendDistBeforeCompile(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := strings.ReplaceAll(string(body), "\r\n", "\n")
	// Each job's own steps must produce web/dist — a marker in a sibling job
	// or a comment cannot satisfy the per-job contract (#668).
	quality := stripHashComments(t, workflowJobBlock(t, workflow, "quality"))
	if !strings.Contains(quality, "scripts/ci/frontend.sh") {
		t.Error("quality job does not run the shared frontend job (scripts/ci/frontend.sh)")
	}
	for _, job := range []string{"browser-e2e", "package-smoke", "release"} {
		block := stripHashComments(t, workflowJobBlock(t, workflow, job))
		if !strings.Contains(block, "scripts/ci/prepare-frontend-dist.sh") {
			t.Errorf("%s job does not build web/dist via prepare-frontend-dist.sh", job)
		}
	}
	if !strings.Contains(quality, "scripts/ci/e2e.sh") {
		t.Error("quality job does not run the shared e2e leg (scripts/ci/e2e.sh)")
	}
	dist := strings.Index(quality, "scripts/ci/frontend.sh")
	vet := strings.Index(quality, "go vet ./...")
	if dist < 0 || vet < 0 || dist > vet {
		t.Fatalf("frontend dist must be built before go vet in the quality job so //go:embed all:dist succeeds")
	}
	// The e2e/browser-e2e jobs must invoke their scripts in their own blocks.
	e2eBlock := stripHashComments(t, workflowJobBlock(t, workflow, "browser-e2e"))
	if !strings.Contains(e2eBlock, "scripts/ci/browser-e2e.sh") {
		t.Error("browser-e2e job does not invoke scripts/ci/browser-e2e.sh")
	}
	// The veil system user is created by the shared e2e script (single source
	// of truth for PR CI and release) — assert the real command, not a
	// comment describing it.
	e2eBody, err := os.ReadFile("../../scripts/ci/e2e.sh")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stripHashComments(t, strings.ReplaceAll(string(e2eBody), "\r\n", "\n")), "useradd --system --user-group --no-create-home --shell /usr/sbin/nologin veil") {
		t.Error("scripts/ci/e2e.sh missing veil system user creation")
	}
}
