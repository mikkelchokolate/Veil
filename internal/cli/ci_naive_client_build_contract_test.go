package cli

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// quotedString matches a double-quoted shell string (no escaped quotes in
// this workflow).
var quotedString = regexp.MustCompile(`"[^"]*"`)

// blankPlainQuotedStrings empties multi-word double-quoted strings without a
// command substitution (message text such as the ::error:: hint), then drops
// the remaining quotes so quoted command words (gh "release" "create") are
// still scanned as a command.
func blankPlainQuotedStrings(line string) string {
	line = quotedString.ReplaceAllStringFunc(line, func(q string) string {
		if strings.Contains(q, "$(") || strings.Contains(q, "`") || !strings.ContainsAny(q, " \t") {
			return q
		}
		return `""`
	})
	return strings.ReplaceAll(line, `"`, "")
}

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
			If          string            `yaml:"if"`
			Permissions map[string]string `yaml:"permissions"`
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
	wantPerms := map[string]map[string]string{
		"build-linux-x64": {"contents": "read", "id-token": "write", "attestations": "write"},
		"publish":         {"contents": "write", "attestations": "read"},
	}
	for name, want := range wantPerms {
		got := parsed.Jobs[name].Permissions
		if len(got) != len(want) {
			t.Fatalf("%s permissions = %v, want %v", name, got, want)
		}
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("%s permissions = %v, want %v", name, got, want)
			}
		}
	}
	// The build must be preserved as an artifact before (and independently
	// of) publishing, and the workflow must never create the release/tag.
	upload := strings.Index(wf, "Upload build as artifact (before any publish)")
	if attest := strings.Index(wf, "Attest build provenance"); attest < 0 || upload > attest {
		t.Fatal("artifact upload must precede attestation so an attest failure cannot lose the build")
	}
	publish := strings.Index(wf, "Publish to existing ci-deps prerelease")
	if upload < 0 || publish < 0 || upload > publish {
		t.Fatal("artifact upload must precede the publish step")
	}
	for _, line := range strings.Split(wf, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Only the text of plain quoted strings (e.g. the ::error:: hint telling
		// the owner how to create ci-deps) is exempt; anything chained after an
		// echo, or inside a "$(...)" substitution, is still scanned.
		trimmed = blankPlainQuotedStrings(trimmed)
		if strings.Contains(trimmed, "release create") || strings.Contains(trimmed, "gh api") || strings.Contains(trimmed, "git push") || strings.Contains(trimmed, "git tag") {
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
		`gh attestation verify "${BUNDLE}.tar.xz" -R "${GITHUB_REPOSITORY}" \
            --signer-workflow "${GITHUB_REPOSITORY}/.github/workflows/naive-client-build.yml" \
            --source-ref refs/heads/main \
            --deny-self-hosted-runners`,
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
