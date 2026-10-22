package cli

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Issue #1373: the source-built naiveproxy mirror must be manual-only and
// main-only, verify the upstream tag→commit binding, land on the ci-deps
// prerelease (never latest, never a version release) and never overwrite an
// asset whose checksum may already be pinned.
func TestNaiveClientBuildWorkflowContract(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/naive-client-build.yml")
	if err != nil {
		t.Fatal(err)
	}
	wf := string(body)

	var parsed struct {
		On   map[string]any `yaml:"on"`
		Jobs map[string]struct {
			If string `yaml:"if"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.On) != 1 {
		t.Fatalf("workflow must be triggered only by workflow_dispatch, got %v", parsed.On)
	}
	if _, ok := parsed.On["workflow_dispatch"]; !ok {
		t.Fatalf("workflow must be triggered only by workflow_dispatch, got %v", parsed.On)
	}
	for _, name := range []string{"build-linux-x64", "publish"} {
		job, ok := parsed.Jobs[name]
		if !ok || job.If != "github.ref == 'refs/heads/main'" {
			t.Fatalf("%s job must be guarded to main, got %q", name, job.If)
		}
	}
	// The build must be preserved as an artifact before (and independently
	// of) publishing, and the workflow must never create the release/tag.
	upload := strings.Index(wf, "Upload build as artifact (before any publish)")
	publish := strings.Index(wf, "Publish to existing ci-deps prerelease")
	if upload < 0 || publish < 0 || upload > publish {
		t.Fatal("artifact upload must precede the publish step")
	}
	for _, line := range strings.Split(wf, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "gh release create") || strings.HasPrefix(trimmed, "gh api") || strings.Contains(trimmed, "git push") {
			t.Fatalf("workflow must not create the ci-deps release/tag itself: %q", trimmed)
		}
	}

	for _, required := range []string{
		`[[ "${TAG}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+-[0-9]+$ ]]`,
		`[[ "${COMMIT}" =~ ^[0-9a-f]{40}$ ]]`,
		"repository: klzgrad/naiveproxy",
		"ref: ${{ inputs.commit }}",
		"persist-credentials: false",
		`test "$(git rev-parse HEAD)" = "${COMMIT}"`,
		`test "$(git ls-remote https://github.com/klzgrad/naiveproxy "refs/tags/${TAG}" | cut -f1)" = "${COMMIT}"`,
		"./get-clang.sh",
		"./build.sh",
		"../tests/basic.sh out/Release/naive",
		"needs: build-linux-x64",
		`sha256sum --check "${BUNDLE}.tar.xz.sha256"`,
		"if-no-files-found: error",
		`test "$(gh release view ci-deps --json isPrerelease -q .isPrerelease)" = true`,
		"refusing to overwrite",
		"actions/attest-build-provenance@",
		"provenance.txt",
	} {
		if !strings.Contains(wf, required) {
			t.Fatalf("naive-client-build workflow is missing %q", required)
		}
	}
	for _, forbidden := range []string{"--clobber", "persist-credentials: true"} {
		if strings.Contains(wf, forbidden) {
			t.Fatalf("naive-client-build workflow must not contain %q", forbidden)
		}
	}
}
