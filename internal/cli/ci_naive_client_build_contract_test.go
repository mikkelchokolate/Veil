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
	job, ok := parsed.Jobs["build-linux-x64"]
	if !ok || job.If != "github.ref == 'refs/heads/main'" {
		t.Fatalf("build job must be guarded to main, got %q", job.If)
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
		"gh release create ci-deps --prerelease --latest=false",
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
