package cli

import (
	"os"
	"strings"
	"testing"
)

func TestDockerWorkflowsInjectAndVerifyBuildVersion(t *testing.T) {
	contracts := map[string][]string{
		"../../.github/workflows/ci.yml": {
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
