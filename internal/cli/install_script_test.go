package cli

import (
	"os"
	"strings"
	"testing"
)

func TestCurlInstallScriptDownloadsVerifiedReleaseBinary(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{
		`REPO="${VEIL_REPO:-mikkelchokolate/Veil}"`,
		"releases/latest/download",
		"checksums.txt",
		"sha256sum -c",
		"tar -xzf",
		"/usr/local/bin",
		"exec \"${INSTALL_DIR}/veil\" install",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing %q:\n%s", want, script)
		}
	}
}

func TestCurlInstallScriptDoesNotDefaultSharedProxyPort(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, unwanted := range []string{`PORT="443"`, "default 443", "preferred shared TCP/UDP port"} {
		if strings.Contains(script, unwanted) {
			t.Fatalf("install.sh should require/prompt for shared proxy port, found %q:\n%s", unwanted, script)
		}
	}
	for _, want := range []string{"--port PORT", "Shared proxy port passed to veil install; omit it to use the interactive prompt"} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing port guidance %q:\n%s", want, script)
		}
	}
}

func TestReleaseWorkflowBuildsChecksummedLinuxArchives(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(body)
	for _, want := range []string{
		"on:",
		"tags:",
		"v*",
		"go build",
		"linux/amd64",
		"linux/arm64",
		"sha256sum",
		"checksums.txt",
		"gh release create",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing %q:\n%s", want, workflow)
		}
	}
}

func TestReadmeDocumentsBackupRollbackAuditWorkflow(t *testing.T) {
	body, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := string(body)
	for _, want := range []string{
		"repair --backup-dir",
		"rollback list --backup-dir",
		"rollback restore",
		"rollback cleanup",
		"--audit-log",
		"audit",
		"JSONL",
		"dry-run",
		"writable",
	} {
		if !strings.Contains(readme, want) {
			t.Fatalf("README.md missing %q:\n%s", want, readme)
		}
	}
}
