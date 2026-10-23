package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var (
	// goModEditCommand spans one `go mod edit` invocation up to the next
	// command separator; line continuations are joined first.
	goModEditCommand = regexp.MustCompile(`go mod edit\b[^\n;&|]*`)
	goModEditRequire = regexp.MustCompile(`-require[= ]['"]?([^'"\s@]+)@([^'"\s]+)`)
	// golang.org/x/mod/semver canonical form (v1.2.3, optional
	// -prerelease/+build), or a shell expansion resolved from a pin.
	canonicalModuleVersion = regexp.MustCompile(`^(v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?|\$\{?[A-Za-z_][A-Za-z0-9_]*\}?)$`)
)

// goModEditRequires returns every module@version passed to -require in any
// `go mod edit` command of script, including repeated flags and commands
// split across `\` continuations.
func goModEditRequires(script string) [][2]string {
	script = strings.ReplaceAll(script, "\\\n", " ")
	var out [][2]string
	for _, command := range goModEditCommand.FindAllString(script, -1) {
		for _, m := range goModEditRequire.FindAllStringSubmatch(command, -1) {
			out = append(out, [2]string{m[1], m[2]})
		}
	}
	return out
}

// `go mod edit -require` writes the version verbatim, and go.mod rejects
// anything that isn't a canonical version, so a query like @latest or a
// branch name fails at the next go command ("version \"latest\" invalid").
// That broke the upstream-compat caddy build on every run. Workflows must
// resolve queries with `go get` and keep `-require` for concrete pins.
func TestWorkflowGoModEditRequireUsesCanonicalVersions(t *testing.T) {
	paths, err := filepath.Glob("../../.github/workflows/*.yml")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no workflows found")
	}
	for _, path := range paths {
		body, err := os.ReadFile(path)
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
			t.Fatalf("%s: %v", path, err)
		}
		for _, job := range wf.Jobs {
			for _, step := range job.Steps {
				for _, m := range goModEditRequires(step.Run) {
					if !canonicalModuleVersion.MatchString(m[1]) {
						t.Errorf("%s step %q: go mod edit -require %s@%s is not a canonical version; use `go get %s@%s`",
							filepath.Base(path), step.Name, m[0], m[1], m[0], m[1])
					}
				}
			}
		}
	}
}

// The CI shell library and the VM image build the same caddy with
// `go mod edit -require`; scan them as plain scripts so a query version
// can't slip in there either.
func TestCIScriptsGoModEditRequireUsesCanonicalVersions(t *testing.T) {
	paths, err := filepath.Glob("../../scripts/ci/*.sh")
	if err != nil {
		t.Fatal(err)
	}
	paths = append(paths, "../../ci/vm/Containerfile")
	found := 0
	for _, path := range paths {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range goModEditRequires(string(body)) {
			found++
			if !canonicalModuleVersion.MatchString(m[1]) {
				t.Errorf("%s: go mod edit -require %s@%s is not a canonical version; use `go get %s@%s`",
					path, m[0], m[1], m[0], m[1])
			}
		}
	}
	if found == 0 {
		t.Fatal("no go mod edit -require found in scripts/ci or ci/vm/Containerfile; update this test if the caddy build moved")
	}
}

func TestGoModEditRequirePatternCatchesQueries(t *testing.T) {
	for _, tc := range []struct {
		script string
		want   [][2]string
	}{
		{`go mod edit -require "github.com/caddyserver/caddy/v2@latest"`, [][2]string{{"github.com/caddyserver/caddy/v2", "latest"}}},
		{`go mod edit -require github.com/x/y@master`, [][2]string{{"github.com/x/y", "master"}}},
		{`go mod edit -require "github.com/caddyserver/caddy/v2@v2.11.7"`, [][2]string{{"github.com/caddyserver/caddy/v2", "v2.11.7"}}},
		{`go mod edit -require "github.com/caddyserver/caddy/v2@${CI_CADDY}"`, [][2]string{{"github.com/caddyserver/caddy/v2", "${CI_CADDY}"}}},
		{`go mod edit -require=github.com/x/y@v1.2.3-rc.1`, [][2]string{{"github.com/x/y", "v1.2.3-rc.1"}}},
		{`go mod edit -require a/b@v1.0.0 -require c/d@latest`, [][2]string{{"a/b", "v1.0.0"}, {"c/d", "latest"}}},
		{"go mod edit \\\n  -require \"a/b@latest\"", [][2]string{{"a/b", "latest"}}},
		{`go mod edit -require='a/b@latest'`, [][2]string{{"a/b", "latest"}}},
		{`go mod edit -replace a/b=c/d@master && go get a/b@latest`, nil},
	} {
		got := goModEditRequires(tc.script)
		if len(got) != len(tc.want) {
			t.Errorf("%q: got %v, want %v", tc.script, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%q: got %v, want %v", tc.script, got, tc.want)
			}
		}
	}
	for version, canonical := range map[string]bool{
		"latest": false, "master": false, "v2": false,
		"v2.11.7": true, "v1.2.3-rc.1": true, "v1.2.3+build.5": true, "${CI_CADDY}": true, "$PIN": true,
	} {
		if got := canonicalModuleVersion.MatchString(version); got != canonical {
			t.Errorf("canonicalModuleVersion(%q)=%v, want %v", version, got, canonical)
		}
	}
}
