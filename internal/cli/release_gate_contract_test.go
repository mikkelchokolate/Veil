package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestReleaseGateRequiredJobsMatchSourceWorkflows(t *testing.T) {
	output, err := exec.Command("python3", "-c", `import json,runpy; print(json.dumps({k: sorted(v) for k,v in runpy.run_path('../../scripts/ci/release-ci-gate.py')['REQUIRED_JOBS'].items()}))`).CombinedOutput()
	if err != nil {
		t.Fatalf("load release gate REQUIRED_JOBS: %v: %s", err, output)
	}
	var required map[string][]string
	if err := json.Unmarshal(output, &required); err != nil || len(required) != 2 {
		t.Fatalf("invalid release gate REQUIRED_JOBS: %v: %s", err, output)
	}
	for _, filename := range []string{"ci.yml", "codeql.yml"} {
		body, err := os.ReadFile("../../.github/workflows/" + filename)
		if err != nil {
			t.Fatal(err)
		}
		var workflow struct {
			Concurrency map[string]string `yaml:"concurrency"`
			Jobs        map[string]struct {
				Name     string                            `yaml:"name"`
				Strategy struct{ Matrix map[string][]any } `yaml:"strategy"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(body, &workflow); err != nil {
			t.Fatalf("%s: unsupported workflow/matrix shape: %v", filename, err)
		}
		if workflow.Concurrency["cancel-in-progress"] != "${{ github.event_name == 'pull_request' }}" {
			t.Errorf("%s must preserve main CI proof and only cancel superseded PR runs", filename)
		}
		var names []string
		for id, job := range workflow.Jobs {
			name := job.Name
			if name == "" {
				name = id
			}
			matrix := job.Strategy.Matrix
			if len(matrix) == 0 {
				names = append(names, name)
				continue
			}
			if len(matrix) != 1 {
				t.Fatalf("%s/%s: unsupported multi-axis matrix; update the release job-name contract", filename, id)
			}
			for axis, values := range matrix {
				if len(values) == 0 {
					t.Fatalf("%s/%s: empty matrix", filename, id)
				}
				for _, value := range values {
					if axis == "include" {
						row, ok := value.(map[string]any)
						language, languageOK := row["language"].(string)
						if !ok || !languageOK || language == "" || len(row) != 2 || name != "analyze (${{ matrix.language }})" {
							t.Fatalf("%s/%s: unsupported include/name shape; update the release job-name contract", filename, id)
						}
						names = append(names, "analyze ("+language+")")
					} else {
						if job.Name != "" || (axis != "arch" && axis != "shard") {
							t.Fatalf("%s/%s: unsupported matrix/name shape; update the release job-name contract", filename, id)
						}
						names = append(names, fmt.Sprintf("%s (%v)", id, value))
					}
				}
			}
		}
		slices.Sort(names)
		if !slices.Equal(names, required[filename]) {
			t.Errorf("%s job names differ from release REQUIRED_JOBS: workflow=%v gate=%v", filename, names, required[filename])
		}
	}
}

func TestReleaseWorkflowRequiresVerifiedCIAndShippedPackageGates(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := strings.ReplaceAll(string(body), "\r\n", "\n")
	// needs: is parsed out of the YAML per job — a needs: line in the wrong
	// job, a quoted string, or a comment cannot satisfy the chain (#744).
	wantNeeds := map[string][]string{
		"release":        {"verified-ci"},
		"package-smoke":  {"verified-ci", "release"},
		"docker-publish": {"verified-ci", "package-smoke"},
		"publish":        {"verified-ci", "package-smoke", "release", "docker-publish"},
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

func TestReleaseWorkflowRestoresVerifiedFrontendDistBeforeCompile(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Jobs map[string]struct {
			Permissions map[string]string `yaml:"permissions"`
			Steps       []struct {
				Uses string            `yaml:"uses"`
				Run  string            `yaml:"run"`
				Env  map[string]string `yaml:"env"`
				With map[string]any    `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	for _, job := range []string{"package-smoke", "release"} {
		block := parsed.Jobs[job]
		if block.Permissions["actions"] != "read" {
			t.Errorf("%s must restore the verified CI artifact with read-only Actions access", job)
		}
		download, restore, compile := -1, -1, -1
		for i, step := range block.Steps {
			if strings.HasPrefix(step.Uses, "actions/download-artifact@") && step.With["name"] == "ci-frontend-dist-${{ github.sha }}" {
				download = i
				for key, want := range map[string]string{
					"path":         ".artifacts/ci/frontend-dist",
					"run-id":       "${{ needs.verified-ci.outputs.ci_run_id }}",
					"repository":   "${{ github.repository }}",
					"github-token": "${{ github.token }}",
				} {
					if step.With[key] != want {
						t.Errorf("%s frontend download %s=%v, want %q", job, key, step.With[key], want)
					}
				}
			}
			run := stripHashComments(t, step.Run)
			if strings.Contains(run, "bash scripts/ci/prepare-frontend-dist.sh") {
				restore = i
				if step.Env["CI_FRONTEND_DIST_ARTIFACT_DIR"] != ".artifacts/ci/frontend-dist" {
					t.Errorf("%s frontend restore must verify the downloaded artifact, not fall back to a new build", job)
				}
			}
			if compile < 0 && (strings.Contains(run, "go build") || strings.Contains(run, "bash scripts/package-smoke.sh")) {
				compile = i
			}
			if strings.HasPrefix(step.Uses, "actions/setup-node@") || strings.HasPrefix(step.Uses, "pnpm/action-setup@") || strings.Contains(run, "pnpm build") {
				t.Errorf("%s must reuse the tested frontend, not build it again", job)
			}
		}
		if download < 0 || restore <= download || compile <= restore {
			t.Errorf("%s must download the gated artifact, verify its SHA/content, then compile (download=%d restore=%d compile=%d)", job, download, restore, compile)
		}
	}
}
