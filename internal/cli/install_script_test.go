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

func TestCurlInstallScriptHidesLegacyStackAndPortOptions(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, unwanted := range []string{`PORT="443"`, "default 443", "preferred shared TCP/UDP port", "Shared proxy port passed to veil install", "--port PORT", "--stack STACK", `--stack "${STACK}"`} {
		if strings.Contains(script, unwanted) {
			t.Fatalf("install.sh should not expose legacy stack/port option %q:\n%s", unwanted, script)
		}
	}
	if !strings.Contains(script, "configure protocols from the Panel") {
		t.Fatalf("install.sh should guide users to configure protocols from the Panel:\n%s", script)
	}
}

func TestCurlInstallScriptUsageShowsSudoForSystemdInstall(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	if !strings.Contains(script, "| sudo bash") {
		t.Fatalf("install.sh usage should show sudo for systemd install:\n%s", script)
	}
	if strings.Contains(script, "| bash\n") || strings.Contains(script, "| bash -s --") {
		t.Fatalf("install.sh usage should not show non-root bash install examples:\n%s", script)
	}
}

func TestCurlInstallScriptRejectsLegacyStackBeforeSideEffects(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	msg := "Veil install only installs Panel; configure protocols as Panel Inbounds."
	if strings.Count(script, msg) != 1 {
		t.Fatalf("install.sh should centralize legacy stack rejection once before side effects, count=%d:\n%s", strings.Count(script, msg), script)
	}
	msgIndex := strings.Index(script, msg)
	for _, sideEffect := range []string{"# Idempotency:", "Downloading Veil", "curl -fsSL \"${download_url}\"", "install -m 0755"} {
		if idx := strings.Index(script, sideEffect); idx >= 0 && msgIndex > idx {
			t.Fatalf("legacy stack rejection should appear before %q:\n%s", sideEffect, script)
		}
	}
}

func TestCurlInstallScriptRequiresRootForPanelServiceInstall(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	if strings.Contains(script, `&& "${INSTALL_DIR}" == "/usr/local/bin"`) {
		t.Fatalf("install.sh should require root for systemd Panel install even with custom install-dir:\n%s", script)
	}
	for _, want := range []string{"Veil installer must run as root", "systemd"} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing root/systemd guidance %q:\n%s", want, script)
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

func TestReleaseWorkflowEnforcesQualityGatesBeforePublish(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(body)
	for _, want := range []string{
		"quality:",
		"go test ./... -count=1",
		"go vet ./...",
		"make build",
		"git diff --check",
		"needs: [quality, release]",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing required release gate %q:\n%s", want, workflow)
		}
	}
}

func TestCiWorkflowEnforcesProductionGates(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(body)
	for _, want := range []string{
		"go test ./... -count=1",
		"go vet ./...",
		"make build",
		"git diff --check",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("ci.yml missing required gate %q:\n%s", want, workflow)
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

func TestCurlInstallScriptSkipsWhenBinaryExists(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	found := false
	for _, want := range []string{
		"-f \"${INSTALL_DIR}/veil\"",
		"already installed",
		"already up to date",
		"skip",
	} {
		if strings.Contains(script, want) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("install.sh missing idempotency check for existing binary:\n%s", script)
	}
}

func TestCurlInstallScriptForceReinstalls(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{
		"--force",
		"FORCE",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing --force flag for forced re-install; found none of [--force, FORCE]:\n%s", script)
		}
	}
}

func TestCurlInstallScriptChecksumFailsWhenMissing(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	found := false
	for _, want := range []string{
		"wc -l",
		"grep -c",
		"No checksum",
		"no matching checksum",
		"checksum not found",
	} {
		if strings.Contains(script, want) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("install.sh missing checksum match-count guard (wc -l / grep -c / error pattern); checksum verification is fragile:\n%s", script)
	}
}
