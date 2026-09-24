package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestReleaseNotesScriptExtractsExactTaggedSection exercises the real
// scripts/release-notes.py (#926): the workflow test only proves the publish
// job wires --notes-file to it — the script itself must extract exactly the
// tagged section and fail closed on absent, malformed, or duplicate tags.
func TestReleaseNotesScriptExtractsExactTaggedSection(t *testing.T) {
	root := t.TempDir()
	changelog := filepath.Join(root, "CHANGELOG.md")
	body := "# Changelog\n\n## Unreleased\n\n- pending\n\n## [v1.2.4] - 2026-09-20\n\n### Fixed\n- exact section body\n\n## [v1.2.3] - 2026-09-01\n\n- other release\n"
	if err := os.WriteFile(changelog, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "release-notes.md")
	run := func(tag, changelogPath string) (string, error) {
		cmd := exec.Command("python3", "../../scripts/release-notes.py",
			"--changelog", changelogPath, "--tag", tag, "--output", output)
		raw, err := cmd.CombinedOutput()
		return string(raw), err
	}
	raw, err := run("v1.2.4", changelog)
	if err != nil {
		t.Fatalf("release-notes.py failed for tagged section: %v\n%s", err, raw)
	}
	notes, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(notes), "exact section body") ||
		strings.Contains(string(notes), "other release") ||
		strings.Contains(string(notes), "pending") {
		t.Fatalf("release-notes.py did not extract exactly the tagged section:\n%s", notes)
	}
	if raw, err := run("v9.9.9", changelog); err == nil {
		t.Fatalf("release-notes.py must fail for a tag absent from CHANGELOG:\n%s", raw)
	}
	if raw, err := run("not-a-version", changelog); err == nil {
		t.Fatalf("release-notes.py must reject a non-version tag:\n%s", raw)
	}
	// Two sections for the same tag is ambiguous — must fail, not pick one.
	dup := filepath.Join(root, "CHANGELOG-dup.md")
	if err := os.WriteFile(dup, []byte(body+"\n## [v1.2.4] - 2026-10-01\n\n- duplicate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if raw, err := run("v1.2.4", dup); err == nil {
		t.Fatalf("release-notes.py must reject duplicate tag sections:\n%s", raw)
	}
}
