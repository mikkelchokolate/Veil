package cli

import (
	"os"
	"strings"
	"testing"
)

// Issue #1373: the source-built naiveproxy mirror must be manual-only, land on
// the ci-deps prerelease (never latest, never a version release) and never
// overwrite an asset whose checksum may already be pinned.
func TestNaiveClientBuildWorkflowContract(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/naive-client-build.yml")
	if err != nil {
		t.Fatal(err)
	}
	wf := string(body)
	for _, required := range []string{
		"workflow_dispatch:",
		"repository: klzgrad/naiveproxy",
		"ref: ${{ inputs.commit }}",
		"./get-clang.sh",
		"./build.sh",
		"../tests/basic.sh out/Release/naive",
		"gh release create ci-deps --prerelease --latest=false",
		"refusing to overwrite",
		"actions/attest-build-provenance@",
		"provenance.txt",
	} {
		if !strings.Contains(wf, required) {
			t.Fatalf("naive-client-build workflow is missing %q", required)
		}
	}
	for _, forbidden := range []string{"--clobber", "push:", "pull_request", "schedule:", "release:\n"} {
		if strings.Contains(wf, forbidden) {
			t.Fatalf("naive-client-build workflow must not contain %q", forbidden)
		}
	}
}
