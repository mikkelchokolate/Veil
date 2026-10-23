package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

var stableTagTest = regexp.MustCompile(`\[\[ "\$\{GITHUB_REF_NAME\}" =~ (\S+) \]\]`)

// Issue #1414: install.sh, install-privileged.sh and veil/panel update all
// resolve /releases/latest, so the GitHub release for a prerelease tag must
// be a prerelease that never becomes Latest. The publish step must classify
// tags with the exact stable-semver test docker-publish uses for :latest
// (#1148), and the gh invocation it builds must carry the right flags. The
// step is executed with a fake gh so the test pins behaviour, not text.
func TestReleasePublishKeepsPrereleasesOffLatest(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	var wf publishChannelWorkflow
	if err := yaml.Unmarshal(body, &wf); err != nil {
		t.Fatal(err)
	}
	publish := publishChannelStepRun(t, wf, "Publish GitHub release")
	docker := publishChannelStepRun(t, wf, "Detect stable semver release tag")

	publishTest := stableTagTest.FindStringSubmatch(publish)
	dockerTest := stableTagTest.FindStringSubmatch(docker)
	if publishTest == nil || dockerTest == nil {
		t.Fatalf("stable-semver test missing: publish=%v docker=%v", publishTest, dockerTest)
	}
	if publishTest[1] != dockerTest[1] {
		t.Fatalf("publish classifies tags with %q but docker-publish uses %q; keep one stable-semver contract", publishTest[1], dockerTest[1])
	}

	if runtime.GOOS == "windows" {
		t.Skip("executing the publish step needs bash")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash not found: %v", err)
	}
	for _, tc := range []struct {
		tag        string
		want       []string
		wantAbsent []string
	}{
		{"v1.2.3", []string{"--latest"}, []string{"--prerelease", "--latest=false"}},
		{"v10.20.30", []string{"--latest"}, []string{"--prerelease", "--latest=false"}},
		{"v1.2.3-rc.1", []string{"--prerelease", "--latest=false"}, []string{"--latest\n"}},
		{"v0.8.0-beta", []string{"--prerelease", "--latest=false"}, []string{"--latest\n"}},
		{"v1.2.3+build.5", []string{"--prerelease", "--latest=false"}, []string{"--latest\n"}},
	} {
		t.Run(tc.tag, func(t *testing.T) {
			args, out, err := runPublishStep(t, bash, publish, tc.tag, "", false)
			if err != nil {
				t.Fatalf("publish step failed: %v\n%s", err, out)
			}
			assertPublishArgs(t, tc.tag, args, tc.want, tc.wantAbsent)
		})
	}
}

// Follow-up to the #1422 review: a stable tag is only promoted to Latest when
// it is not older than the release that is Latest right now. A backport cut
// after a newer stable line (v0.7.4 after v0.8.0) used to get an explicit
// --latest and repoint /releases/latest — and so install.sh and veil/panel
// update — at the older line. The publish step must compare against the
// current Latest with version ordering (not string ordering) and fall back to
// --latest=false, and must fail closed (publish nothing) when it cannot learn
// the current Latest.
func TestReleasePublishNeverDemotesLatestToABackport(t *testing.T) {
	publish := loadPublishStep(t)
	if runtime.GOOS == "windows" {
		t.Skip("executing the publish step needs bash")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash not found: %v", err)
	}
	promote := []string{"--latest"}
	keep := []string{"--latest=false"}
	for _, tc := range []struct {
		name, tag, latest string
		want              []string
		wantAbsent        []string
	}{
		{"first stable release", "v0.1.0", "", promote, []string{"--latest=false", "--prerelease"}},
		{"newer patch", "v0.7.4", "v0.7.3", promote, []string{"--latest=false", "--prerelease"}},
		{"newer minor", "v0.8.0", "v0.7.3", promote, []string{"--latest=false", "--prerelease"}},
		{"numeric not lexical ordering", "v0.10.0", "v0.9.9", promote, []string{"--latest=false", "--prerelease"}},
		{"backport patch after newer minor", "v0.7.4", "v0.8.0", keep, []string{"--latest\n", "--prerelease"}},
		{"backport lexically larger", "v0.9.9", "v0.10.0", keep, []string{"--latest\n", "--prerelease"}},
		{"backport after newer major", "v1.9.0", "v2.0.0", keep, []string{"--latest\n", "--prerelease"}},
		// A Latest that is not stable semver (e.g. an RC promoted before
		// #1414) is no ordering anchor: the stable tag reclaims Latest.
		{"non-stable latest is ignored", "v0.8.0", "v0.9.0-rc.1", promote, []string{"--latest=false", "--prerelease"}},
		{"prerelease stays off latest", "v0.9.0-rc.1", "v0.8.0", []string{"--prerelease", "--latest=false"}, []string{"--latest\n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, out, err := runPublishStep(t, bash, publish, tc.tag, tc.latest, false)
			if err != nil {
				t.Fatalf("publish step failed: %v\n%s", err, out)
			}
			assertPublishArgs(t, tc.tag, args, tc.want, tc.wantAbsent)
		})
	}

	t.Run("latest lookup failure publishes nothing", func(t *testing.T) {
		args, out, err := runPublishStep(t, bash, publish, "v0.8.1", "v0.8.0", true)
		if err == nil {
			t.Fatalf("publish step succeeded although the Latest lookup failed:\n%s", out)
		}
		if args != "" {
			t.Fatalf("gh release create ran although the Latest lookup failed:\n%s", args)
		}
	})
}

func loadPublishStep(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	var wf publishChannelWorkflow
	if err := yaml.Unmarshal(body, &wf); err != nil {
		t.Fatal(err)
	}
	return publishChannelStepRun(t, wf, "Publish GitHub release")
}

// runPublishStep executes the publish step under bash with a fake gh. The fake
// answers `gh release list` with latest (as the tag that isLatest), or fails
// it when listFails is set, and records the argv of `gh release create`.
func runPublishStep(t *testing.T, bash, publish, tag, latest string, listFails bool) (createArgs string, out []byte, err error) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(filepath.Join(dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dist", "release-notes.md"), []byte("notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(dir, "gh-args")
	fakeGH := `#!/bin/sh
if [ "$1" = release ] && [ "$2" = list ]; then
  if [ -n "$FAKE_GH_LIST_FAIL" ]; then echo "HTTP 502: Bad Gateway" >&2; exit 1; fi
  if [ -n "$FAKE_GH_LATEST" ]; then printf '%s\n' "$FAKE_GH_LATEST"; fi
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
	cmd := exec.Command(bash, "-e", "-c", publish)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GITHUB_REF_NAME="+tag,
		"GITHUB_REPOSITORY=mikkelchokolate/Veil",
		"FAKE_GH_LATEST="+latest,
		"FAKE_GH_LIST_FAIL="+fail,
	)
	out, err = cmd.CombinedOutput()
	raw, readErr := os.ReadFile(argsFile)
	if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatal(readErr)
	}
	return string(raw), out, err
}

func assertPublishArgs(t *testing.T, tag, args string, want, wantAbsent []string) {
	t.Helper()
	if !strings.HasPrefix(args, "release\ncreate\n"+tag+"\n") {
		t.Fatalf("unexpected gh invocation:\n%s", args)
	}
	lines := "\n" + args
	for _, w := range want {
		if !strings.Contains(lines, "\n"+w+"\n") {
			t.Fatalf("gh args for %s missing %q:\n%s", tag, w, args)
		}
	}
	for _, absent := range wantAbsent {
		if strings.Contains(lines, "\n"+absent) {
			t.Fatalf("gh args for %s must not contain %q:\n%s", tag, strings.TrimSpace(absent), args)
		}
	}
}
