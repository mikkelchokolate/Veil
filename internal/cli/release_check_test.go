package cli

import (
	"os"
	"strings"
	"testing"
)

// TestDockerfileHealthcheckUsesContainerContract asserts the image healthcheck
// contract structurally: the HEALTHCHECK instruction must invoke `veil
// healthcheck`, and no ENV may pin VEIL_CONTAINER_HEALTH_PATH to the packaged
// root — a hard pin would defeat the VEIL_VAR_DIR-derived default for
// custom-root containers (issue #753). Comment text cannot satisfy any of it.
func TestDockerfileHealthcheckUsesContainerContract(t *testing.T) {
	body, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	instructions := dockerfileInstructions(t, string(body))
	sawHealthcheck := false
	for _, instruction := range instructions {
		if strings.Contains(instruction, `veil status --listen http://127.0.0.1:2096`) {
			t.Fatalf("Dockerfile healthcheck still hardcodes HTTP :2096: %q", instruction)
		}
		if strings.HasPrefix(instruction, "ENV ") && strings.Contains(instruction, "VEIL_CONTAINER_HEALTH_PATH=") {
			t.Fatalf("Dockerfile must not pin VEIL_CONTAINER_HEALTH_PATH — it must derive from VEIL_VAR_DIR: %q", instruction)
		}
		if strings.HasPrefix(instruction, "HEALTHCHECK ") {
			if !strings.Contains(instruction, "CMD veil healthcheck") {
				t.Fatalf("HEALTHCHECK must invoke `veil healthcheck`: %q", instruction)
			}
			sawHealthcheck = true
		}
	}
	if !sawHealthcheck {
		t.Fatal("Dockerfile has no HEALTHCHECK instruction")
	}
	// The entrypoint must derive the leaf from VEIL_VAR_DIR so `veil serve`
	// writes the contract where the probe will look for it.
	entrypoint, err := os.ReadFile("../../packaging/docker/entrypoint.sh")
	if err != nil {
		t.Fatal(err)
	}
	live := stripHashComments(t, string(entrypoint))
	if !strings.Contains(live, `VEIL_CONTAINER_HEALTH_PATH="${VEIL_CONTAINER_HEALTH_PATH:-${VEIL_VAR_DIR:-/var/lib/veil}/container-health.json}"`) {
		t.Fatalf("entrypoint must derive VEIL_CONTAINER_HEALTH_PATH from VEIL_VAR_DIR:\n%s", live)
	}
	if !strings.Contains(live, "export VEIL_STATE_PATH VEIL_APPLY_ROOT VEIL_KEY_PATH VEIL_CONTAINER_HEALTH_PATH") {
		t.Fatalf("entrypoint must export VEIL_CONTAINER_HEALTH_PATH for the serve process:\n%s", live)
	}
}

func TestDockerfileCreatesWritableVeilDirectoriesForNonRootUser(t *testing.T) {
	body, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	instructions := dockerfileInstructions(t, string(body))
	sawMkdir, sawChown, sawUser := false, false, false
	for _, instruction := range instructions {
		if strings.HasPrefix(instruction, "RUN ") {
			if strings.Contains(instruction, "mkdir -p /etc/veil /var/lib/veil") {
				sawMkdir = true
			}
			if strings.Contains(instruction, "chown -R veil:veil /etc/veil /var/lib/veil") {
				sawChown = true
			}
		}
		if instruction == "USER veil" {
			sawUser = true
		}
	}
	for name, ok := range map[string]bool{"mkdir -p /etc/veil /var/lib/veil": sawMkdir, "chown -R veil:veil /etc/veil /var/lib/veil": sawChown, "USER veil": sawUser} {
		if !ok {
			t.Fatalf("Dockerfile missing instruction %q for writable non-root runtime", name)
		}
	}
}

// TestMakefileDefinesReleaseCheck asserts the release-check RECIPE — not the
// .PHONY line or comments — contains the required gate commands (#798).
func TestMakefileDefinesReleaseCheck(t *testing.T) {
	body, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	recipe := strings.Join(makefileRecipe(t, string(body), "release-check"), "\n")
	for _, want := range []string{"go vet ./...", "go test ./... -count=1", "go test -tags e2e ./test/e2e/... -count=1", "make build", "sh -n scripts/install.sh", "sh -n scripts/install-main.sh", "bash -n scripts/install-privileged.sh scripts/uninstall.sh", "bash scripts/install-privileged.sh --help >/dev/null", "bash scripts/uninstall.sh --help >/dev/null", "bash scripts/install-main.sh --help >/dev/null", "git diff --check", "git status --short"} {
		if !strings.Contains(recipe, want) {
			t.Fatalf("Makefile release-check recipe missing %q:\n%s", want, recipe)
		}
	}
}

// TestCiWorkflowRunsE2ESuite pins a literal `e2e:` job key — a `browser-e2e:`
// sibling or a comment must not satisfy it (#866).
func TestCiWorkflowRunsE2ESuite(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := strings.ReplaceAll(string(body), "\r\n", "\n")
	job := stripHashComments(t, workflowJobBlock(t, workflow, "e2e"))
	if !strings.Contains(job, "scripts/ci/e2e.sh") {
		t.Fatalf("ci.yml e2e job does not invoke scripts/ci/e2e.sh:\n%s", job)
	}
	e2eScript, err := os.ReadFile("../../scripts/ci/e2e.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := stripHashComments(t, string(e2eScript))
	if !strings.Contains(script, "go test -tags e2e ./test/e2e/...") {
		t.Fatalf("scripts/ci/e2e.sh missing required e2e gate %q", "go test -tags e2e ./test/e2e/...")
	}
}

func TestMakefileDefinesE2ETarget(t *testing.T) {
	body, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	recipe := strings.Join(makefileRecipe(t, string(body), "e2e"), "\n")
	if !strings.Contains(recipe, "go test -tags e2e ./test/e2e/... -count=1") {
		t.Fatalf("Makefile e2e recipe missing required command:\n%s", recipe)
	}
}
