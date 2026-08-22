package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseWorkflowBindsEveryArtifactAndNotesToExactCommit(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(body)
	for _, required := range []string{
		`git rev-parse "${GITHUB_REF_NAME}^{commit}"`,
		`GITHUB_SHA`,
		`--source-commit "${GITHUB_SHA}"`,
		`-X main.commit=${GITHUB_SHA}`,
		`CHANGELOG.md`,
		`release-notes.md`,
		`--notes-file`,
		`dependency-manifest`,
		`go-version`,
		`node-version`,
		`pnpm-version`,
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("release workflow lacks exact-source requirement %q", required)
		}
	}
	if strings.Contains(workflow, `--notes "Automated Veil release`) {
		t.Error("release still publishes generic notes unrelated to the tagged changelog section")
	}
}

func TestReleaseProvenanceIncludesToolchainAndDependencyManifest(t *testing.T) {
	root := t.TempDir()
	dist := filepath.Join(root, "dist")
	if err := os.MkdirAll(dist, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "veil_linux_amd64.tar.gz"), []byte("exact artifact"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dist, "checksums.txt"), []byte("checksum manifest"), 0o600); err != nil {
		t.Fatal(err)
	}
	dependencyManifest := filepath.Join(root, "go.sum")
	if err := os.WriteFile(dependencyManifest, []byte("module dependency lock\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dist, "veil.provenance.json")
	commit := strings.Repeat("a", 40)
	command := exec.Command("python3", "../../scripts/release-provenance.py",
		"--dist", dist,
		"--repository", "owner/repo",
		"--tag", "v1.2.3",
		"--commit", commit,
		"--go-version", "go1.25.1",
		"--node-version", "24.7.0",
		"--pnpm-version", "10.15.1",
		"--dependency-manifest", dependencyManifest,
		"--output", output,
	)
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("generate provenance: %v\n%s", err, raw)
	}
	raw, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var statement map[string]any
	if err := json.Unmarshal(raw, &statement); err != nil {
		t.Fatal(err)
	}
	predicate := statement["predicate"].(map[string]any)
	definition := predicate["buildDefinition"].(map[string]any)
	external := definition["externalParameters"].(map[string]any)
	if external["sourceCommit"] != commit || external["sourceTag"] != "v1.2.3" {
		t.Fatalf("source binding missing: %#v", external)
	}
	toolchain, _ := external["toolchain"].(map[string]any)
	if toolchain["go"] != "go1.25.1" || toolchain["node"] != "24.7.0" || toolchain["pnpm"] != "10.15.1" {
		t.Fatalf("toolchain provenance missing: %#v", toolchain)
	}
	dependencies, _ := definition["resolvedDependencies"].([]any)
	foundManifest := false
	for _, item := range dependencies {
		dependency, _ := item.(map[string]any)
		if strings.Contains(asString(dependency["uri"]), "go.sum") {
			digest, _ := dependency["digest"].(map[string]any)
			foundManifest = asString(digest["sha256"]) != ""
		}
	}
	if !foundManifest {
		t.Fatalf("dependency-manifest digest absent: %#v", dependencies)
	}
}

func asString(value any) string {
	result, _ := value.(string)
	return result
}
