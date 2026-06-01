package cli

import (
	"os"
	"strings"
	"testing"
)

func TestDockerfileHealthcheckUsesExplicitHTTPForDefaultServe(t *testing.T) {
	body, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(body)
	if !strings.Contains(dockerfile, `veil status --listen http://127.0.0.1:2096 --json`) {
		t.Fatalf("Dockerfile healthcheck should use explicit HTTP for default non-TLS serve:\n%s", dockerfile)
	}
}

func TestDockerfileCreatesWritableVeilDirectoriesForNonRootUser(t *testing.T) {
	body, err := os.ReadFile("../../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	dockerfile := string(body)
	for _, want := range []string{"mkdir -p /etc/veil /var/lib/veil", "chown -R veil:veil /etc/veil /var/lib/veil", "USER veil"} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile missing %q for writable non-root runtime:\n%s", want, dockerfile)
		}
	}
}

func TestMakefileDefinesReleaseCheck(t *testing.T) {
	body, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(body)
	for _, want := range []string{"release-check:", "go vet ./...", "go test ./... -count=1", "make build", "bash -n scripts/install.sh scripts/uninstall.sh", "bash scripts/install.sh --help >/dev/null", "bash scripts/uninstall.sh --help >/dev/null", "git diff --check", "git status --short"} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile missing %q:\n%s", want, makefile)
		}
	}
}

func TestCiWorkflowRunsE2ESuite(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{"e2e:", "go test -tags e2e ./test/e2e/..."} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("ci.yml missing required e2e gate %q:\n%s", want, workflow)
		}
	}
}

func TestMakefileDefinesE2ETarget(t *testing.T) {
	body, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(body)
	for _, want := range []string{"e2e:", "go test -tags e2e ./test/e2e/... -count=1"} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile missing e2e target %q:\n%s", want, makefile)
		}
	}
}
