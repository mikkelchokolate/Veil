package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"gopkg.in/yaml.v3"
)

var (
	goModEditRequire = regexp.MustCompile(`go mod edit\b[^\n]*?-require[= ]"?([^"\s@]+)@([^"\s]+)"?`)
	// golang.org/x/mod/semver canonical form (v1.2.3, optional
	// -prerelease/+build), or a shell expansion resolved from a pin.
	canonicalModuleVersion = regexp.MustCompile(`^(v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?|\$\{?[A-Za-z_][A-Za-z0-9_]*\}?)$`)
)

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
				for _, m := range goModEditRequire.FindAllStringSubmatch(step.Run, -1) {
					if !canonicalModuleVersion.MatchString(m[2]) {
						t.Errorf("%s step %q: go mod edit -require %s@%s is not a canonical version; use `go get %s@%s`",
							filepath.Base(path), step.Name, m[1], m[2], m[1], m[2])
					}
				}
			}
		}
	}
}

func TestGoModEditRequirePatternCatchesQueries(t *testing.T) {
	for script, wantBad := range map[string]bool{
		`go mod edit -require "github.com/caddyserver/caddy/v2@latest"`:      true,
		`go mod edit -require github.com/x/y@master`:                         true,
		`go mod edit -require "github.com/caddyserver/caddy/v2@v2.11.7"`:     false,
		`go mod edit -require "github.com/caddyserver/caddy/v2@${CI_CADDY}"`: false,
		`go mod edit -require=github.com/x/y@v1.2.3-rc.1`:                    false,
	} {
		m := goModEditRequire.FindStringSubmatch(script)
		if m == nil {
			t.Fatalf("pattern missed %q", script)
		}
		if bad := !canonicalModuleVersion.MatchString(m[2]); bad != wantBad {
			t.Errorf("%q: flagged=%v, want %v (version %q)", script, bad, wantBad, m[2])
		}
	}
}
