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
		"../../.github/workflows/release.yml": {
			`--build-arg "VERSION=${GITHUB_REF_NAME}"`,
			`VERSION=${{ github.ref_name }}`,
			`docker run --rm veil:release-check version`,
			`grep -F "${GITHUB_REF_NAME}"`,
		},
	}

	for path, wants := range contracts {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		workflow := strings.ReplaceAll(string(body), "\r\n", "\n")
		for _, want := range wants {
			if !strings.Contains(workflow, want) {
				t.Errorf("%s missing deterministic Docker version contract %q", path, want)
			}
		}
	}
}
