package cli

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/mikkelchokolate/Veil/internal/protocols/mieru"
)

// Issue #1415: the weekly upstream-compat run probed `mita -version ||
// mita --version`. mita has neither flag, so under `set -euo pipefail` the
// step aborted and no compat test ran for weeks. The probe must use the same
// subcommand as the product runtime catalog and keep the non-empty-output
// bar from #690.
func TestUpstreamCompatMitaProbeMatchesRuntimeCatalog(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/upstream-compat.yml")
	if err != nil {
		t.Fatal(err)
	}
	var wf struct {
		Jobs map[string]struct {
			Steps []struct {
				Name string `yaml:"name"`
				Run  string `yaml:"run"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &wf); err != nil {
		t.Fatal(err)
	}
	var run string
	for _, job := range wf.Jobs {
		for _, step := range job.Steps {
			if strings.Contains(step.Name, "(mita)") {
				run = step.Run
			}
		}
	}
	if run == "" {
		t.Fatal("upstream-compat.yml has no mita install step")
	}

	rt := mieru.Plugin{}.RuntimeInstall("amd64")
	want := rt.Binary + " " + strings.Join(rt.VersionArgs, " ")
	var code []string
	for _, line := range strings.Split(run, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			code = append(code, trimmed)
		}
	}
	script := strings.Join(code, "\n")
	if !strings.Contains(script, `"$(`+want+` `) && !strings.Contains(script, `"$(`+want+`)"`) {
		t.Fatalf("mita probe must run %q (runtime catalog VersionArgs); step:\n%s", want, script)
	}
	if regexp.MustCompile(`\bmita\s+--?version\b`).MatchString(script) {
		t.Fatalf("mita has no -version/--version flag; step:\n%s", script)
	}
	if !strings.Contains(script, `[ -n "${mita_version}" ]`) {
		t.Fatalf("mita probe lost the non-empty-output check (#690); step:\n%s", script)
	}
}
