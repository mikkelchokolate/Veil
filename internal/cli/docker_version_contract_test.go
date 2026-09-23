package cli

import (
	"os"
	"strings"
	"testing"
)

func TestDockerWorkflowsInjectAndVerifyBuildVersion(t *testing.T) {
	// The version-injection contract lives in the shared CI script (single
	// source of truth for local VMs and GitHub Actions); the workflow only
	// has to route to it.
	contracts := map[string][]string{
		"../../.github/workflows/ci.yml": {
			"scripts/ci/image-build.sh",
		},
		"../../scripts/ci/image-build.sh": {
			`--build-arg "VERSION=${version}"`,
			`docker run --rm veil:ci version`,
			`grep -F "${version}"`,
		},
	}

	for path, wants := range contracts {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := stripHashComments(t, strings.ReplaceAll(string(body), "\r\n", "\n"))
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("%s missing deterministic Docker version contract %q", path, want)
			}
		}
	}

	// The verify gate must execute BOTH pushed architectures — a manifest
	// entry alone is not evidence the arm64 blob runs — and probe the
	// embedded SPA on each, matching the PR image-build contract (#681).
	// Asserted inside the docker-publish job block: a marker in a sibling
	// job or a comment is not routing evidence.
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := strings.ReplaceAll(string(body), "\r\n", "\n")
	dockerJob := stripHashComments(t, workflowJobBlock(t, workflow, "docker-publish"))
	for _, want := range []string{
		`VERSION=${{ github.ref_name }}`,
		`GO_GODEBUG=${{ steps.versions.outputs.go_godebug }}`,
		`GO_GOPROXY=${{ steps.versions.outputs.go_goproxy }}`,
		`for platform in linux/amd64 linux/arm64`,
		`docker pull --platform "${platform}" "${image}"`,
		`docker run --rm --platform "${platform}" "${image}" version`,
		`grep -F "${GITHUB_REF_NAME}"`,
		`image-spa-probe.sh "${image}" "${platform}"`,
	} {
		if !strings.Contains(dockerJob, want) {
			t.Errorf("docker-publish job missing deterministic Docker version contract %q", want)
		}
	}
}
