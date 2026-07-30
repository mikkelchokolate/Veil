package cli

import (
	"os"
	"strings"
	"testing"
)

func TestCodeQLWorkflowUsesAdvancedConfiguration(t *testing.T) {
	workflowBody, err := os.ReadFile("../../.github/workflows/codeql.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := strings.ReplaceAll(string(workflowBody), "\r\n", "\n")

	for _, required := range []string{
		"merge_group:",
		"workflow_dispatch:",
		"actions: read",
		"packages: read",
		"runs-on: ubuntu-24.04",
		"fail-fast: false",
		"language: go",
		"build-mode: manual",
		"language: javascript-typescript",
		"language: actions",
		"build-mode: none",
		"go-version-file: go.mod",
		"build-mode: ${{ matrix.build-mode }}",
		"config-file: ./.github/codeql/codeql-config.yml",
		"dependency-caching: true",
		"go build ./...",
		`category: "/language:${{ matrix.language }}"`,
	} {
		if !strings.Contains(workflow, required) {
			t.Errorf("CodeQL Advanced workflow is missing %q", required)
		}
	}

	configBody, err := os.ReadFile("../../.github/codeql/codeql-config.yml")
	if err != nil {
		t.Fatal(err)
	}
	config := strings.ReplaceAll(string(configBody), "\r\n", "\n")
	if !strings.Contains(config, "- uses: security-and-quality") {
		t.Error("CodeQL config must enable the security-and-quality suite")
	}
	if strings.Contains(config, "disable-default-queries: true") {
		t.Error("CodeQL configuration must retain default queries")
	}
}
