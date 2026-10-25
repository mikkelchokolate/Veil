package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type publishChannelWorkflow struct {
	Jobs map[string]struct {
		Steps []struct {
			Name string `yaml:"name"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// publishChannelStepRun returns the run script of the single step called name.
func publishChannelStepRun(t *testing.T, wf publishChannelWorkflow, name string) string {
	t.Helper()
	var run string
	found := 0
	for _, job := range wf.Jobs {
		for _, step := range job.Steps {
			if step.Name == name {
				run = step.Run
				found++
			}
		}
	}
	if found != 1 {
		t.Fatalf("release.yml has %d steps named %q, want exactly 1", found, name)
	}
	return run
}

func loadReleaseWorkflow(t *testing.T) publishChannelWorkflow {
	t.Helper()
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	var wf publishChannelWorkflow
	if err := yaml.Unmarshal(body, &wf); err != nil {
		t.Fatal(err)
	}
	return wf
}

// TestReleaseLatestChannelAgreesAcrossJobs locks #1448 and keeps the #1148/
// #1414/#1422 contract in one place. Two channels must never disagree about
// what "latest" means: the GHCR `:latest` image tag (docker-publish) and the
// GitHub Latest release (publish). A prerelease claims neither, and a stable
// backport cut after a newer stable line must not move either — before this,
// tagging v0.7.4 after v0.8.0 published the release with --latest=false while
// still repointing ghcr.io/…:latest at the older line.
//
// The test executes both steps under bash with a fake gh, so it pins behaviour
// rather than text, and asserts the two decisions agree for every case.
func TestReleaseLatestChannelAgreesAcrossJobs(t *testing.T) {
	wf := loadReleaseWorkflow(t)
	publish := publishChannelStepRun(t, wf, "Publish GitHub release")
	docker := publishChannelStepRun(t, wf, "Detect the latest channel")
	for name, step := range map[string]string{"publish": publish, "docker-publish": docker} {
		if !strings.Contains(step, "scripts/ci/release-channel.sh") {
			t.Fatalf("%s must classify the tag with scripts/ci/release-channel.sh, or the two latest channels can disagree (#1448):\n%s", name, step)
		}
	}
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ReplaceAll(string(body), "\r\n", "\n"), "enable=${{ steps.latest.outputs.enabled }}") {
		t.Fatal("the image metadata's latest tag must be driven by the channel decision")
	}

	if runtime.GOOS == "windows" {
		t.Skip("executing the release steps needs bash")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash not found: %v", err)
	}

	for _, tc := range []struct {
		name, tag, published, want string
	}{
		{"first stable release", "v0.1.0", "", "latest"},
		{"newer patch", "v0.7.4", "v0.7.3", "latest"},
		{"newer minor", "v0.8.0", "v0.7.3", "latest"},
		{"numeric not lexical ordering", "v0.10.0", "v0.9.9", "latest"},
		{"re-run of the newest tag", "v0.8.0", "v0.8.0\nv0.7.3", "latest"},
		{"backport patch after newer minor", "v0.7.4", "v0.8.0", "backport"},
		{"backport lexically larger", "v0.9.9", "v0.10.0", "backport"},
		{"backport after newer major", "v1.9.0", "v2.0.0", "backport"},
		{"anchor is newest stable not latest flag", "v0.7.4", "v0.9.0-rc.1\nv0.7.3\nv0.8.0\nv0.7.2", "backport"},
		{"prerelease stays off latest", "v0.9.0-rc.1", "v0.8.0", "prerelease"},
		{"build metadata is not stable", "v1.2.3+build.5", "v0.8.0", "prerelease"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantLatest := tc.want == "latest"

			createArgs, out, err := runReleaseStep(t, bash, publish, tc.tag, tc.published, false)
			if err != nil {
				t.Fatalf("publish step failed: %v\n%s", err, out)
			}
			if gotLatest := strings.Contains("\n"+createArgs+"\n", "\n--latest\n"); gotLatest != wantLatest {
				t.Fatalf("publish step for %s uses --latest=%t, want %t:\n%s", tc.tag, gotLatest, wantLatest, createArgs)
			}
			if tc.want == "prerelease" && !strings.Contains("\n"+createArgs+"\n", "\n--prerelease\n") {
				t.Fatalf("prerelease tag %s must be published as a prerelease:\n%s", tc.tag, createArgs)
			}

			outputs, dockerOut, err := runReleaseStepWithOutput(t, bash, docker, tc.tag, tc.published, false)
			if err != nil {
				t.Fatalf("docker-publish step failed: %v\n%s", err, dockerOut)
			}
			if gotLatest := outputs["enabled"] == "true"; gotLatest != wantLatest {
				t.Fatalf("docker-publish would move :latest=%t for %s, want %t; step output=%v", gotLatest, tc.tag, wantLatest, outputs)
			}
		})
	}

	t.Run("unreadable release list fails both channels closed", func(t *testing.T) {
		if _, out, err := runReleaseStep(t, bash, publish, "v0.8.1", "v0.8.0", true); err == nil {
			t.Fatalf("publish step succeeded although the release list failed:\n%s", out)
		}
		outputs, out, err := runReleaseStepWithOutput(t, bash, docker, "v0.8.1", "v0.8.0", true)
		if err == nil {
			t.Fatalf("docker-publish step succeeded although the release list failed:\n%s", out)
		}
		if outputs["enabled"] == "true" {
			t.Fatal("docker-publish moved :latest although the channel decision failed")
		}
	})
}

// runReleaseStep executes a release.yml step under bash with a fake gh and the
// real scripts/ci/release-channel.sh in the working directory, and returns the
// argv of `gh release create` (empty when the step does not create a release).
func runReleaseStep(t *testing.T, bash, run, tag, published string, listFails bool) (string, []byte, error) {
	t.Helper()
	createArgs, _, out, err := runReleaseStepRaw(t, bash, run, tag, published, listFails)
	return createArgs, out, err
}

// runReleaseStepWithOutput is runReleaseStep plus the step's GITHUB_OUTPUT keys.
func runReleaseStepWithOutput(t *testing.T, bash, run, tag, published string, listFails bool) (map[string]string, []byte, error) {
	t.Helper()
	_, outputs, out, err := runReleaseStepRaw(t, bash, run, tag, published, listFails)
	return outputs, out, err
}

func runReleaseStepRaw(t *testing.T, bash, run, tag, published string, listFails bool) (createArgs string, outputs map[string]string, out []byte, err error) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	dist := filepath.Join(dir, "dist")
	scriptDir := filepath.Join(dir, "scripts", "ci")
	for _, path := range []string{bin, dist, scriptDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dist, "release-notes.md"), []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	channelScript, err := os.ReadFile(filepath.Join("..", "..", "scripts", "ci", "release-channel.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "release-channel.sh"), channelScript, 0o755); err != nil {
		t.Fatal(err)
	}

	argsFile := filepath.Join(dir, "gh-args")
	outputFile := filepath.Join(dir, "github-output")
	fakeGH := `#!/bin/sh
if [ "$1" = release ] && [ "$2" = list ]; then
  if [ -n "$FAKE_GH_LIST_FAIL" ]; then echo "HTTP 502: Bad Gateway" >&2; exit 1; fi
  json=""; prev=""
  for a in "$@"; do [ "$prev" = --json ] && json="$a"; prev="$a"; done
  case ",$json," in *,tagName,*) ;; *) echo "release list must ask for --json with tagName: $*" >&2; exit 2;; esac
  if [ -n "$FAKE_GH_PUBLISHED" ]; then printf '%s\n' "$FAKE_GH_PUBLISHED"; fi
  exit 0
fi
if [ "$1" = release ] && [ "$2" = create ]; then
  for a in "$@"; do printf '%s\n' "$a"; done > "` + argsFile + `"
  exit 0
fi
echo "unexpected gh call: $*" >&2
exit 2
`
	if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o755); err != nil {
		t.Fatal(err)
	}

	fail := ""
	if listFails {
		fail = "1"
	}
	cmd := exec.Command(bash, "-e", "-c", run)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_REF_NAME="+tag,
		"GITHUB_REPOSITORY=mikkelchokolate/Veil",
		"GITHUB_OUTPUT="+outputFile,
		"GH_TOKEN=fake",
		"FAKE_GH_PUBLISHED="+published,
		"FAKE_GH_LIST_FAIL="+fail,
	)
	out, err = cmd.CombinedOutput()

	raw, readErr := os.ReadFile(argsFile)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	outputs = map[string]string{}
	if output, readErr := os.ReadFile(outputFile); readErr == nil {
		for _, line := range strings.Split(string(output), "\n") {
			if key, value, ok := strings.Cut(line, "="); ok {
				outputs[key] = value
			}
		}
	}
	return string(raw), outputs, out, err
}
