package cli

import (
	"os"
	"strings"
	"testing"
)

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
	for _, want := range []string{"release-check:", "go vet ./...", "go test ./... -count=1", "git diff --check", "git status --short"} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile missing %q:\n%s", want, makefile)
		}
	}
}
