package cli

import (
	"os"
	"strings"
	"testing"
)

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
