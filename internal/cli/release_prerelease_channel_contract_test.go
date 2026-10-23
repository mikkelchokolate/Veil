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

// releaseStepRun returns the run script of the single step called name.
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
			fakeGH := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > \"" + argsFile + "\"\n"
			if err := os.WriteFile(filepath.Join(bin, "gh"), []byte(fakeGH), 0o755); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(bash, "-e", "-c", publish)
			cmd.Dir = dir
			cmd.Env = append(os.Environ(),
				"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"GITHUB_REF_NAME="+tc.tag,
				"GITHUB_REPOSITORY=mikkelchokolate/Veil",
			)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("publish step failed: %v\n%s", err, out)
			}
			raw, err := os.ReadFile(argsFile)
			if err != nil {
				t.Fatalf("gh was not invoked: %v", err)
			}
			args := string(raw)
			if !strings.HasPrefix(args, "release\ncreate\n"+tc.tag+"\n") {
				t.Fatalf("unexpected gh invocation:\n%s", args)
			}
			lines := "\n" + args
			for _, want := range tc.want {
				if !strings.Contains(lines, "\n"+want+"\n") {
					t.Fatalf("gh args for %s missing %q:\n%s", tc.tag, want, args)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(lines, "\n"+absent) {
					t.Fatalf("gh args for %s must not contain %q:\n%s", tc.tag, strings.TrimSpace(absent), args)
				}
			}
		})
	}
}
