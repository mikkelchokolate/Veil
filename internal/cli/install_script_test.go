package cli

import (
	"os"
	"os/exec"
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
		"exec \"${RUN_BIN}\" install",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing %q:\n%s", want, script)
		}
	}
}

func TestCurlInstallScriptRejectsMissingOptionValueBeforeSideEffects(t *testing.T) {
	cmd := exec.Command("bash", "../../scripts/install.sh", "--domain")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected install.sh --domain to fail")
	}
	got := string(out)
	if !strings.Contains(got, "Missing value for --domain") || strings.Contains(got, "Downloading Veil") {
		t.Fatalf("unexpected install.sh --domain output:\n%s", got)
	}
}

func TestCurlUninstallScriptRejectsMissingOptionValueBeforeSideEffects(t *testing.T) {
	cmd := exec.Command("bash", "../../scripts/uninstall.sh", "--install-dir")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected uninstall.sh --install-dir to fail")
	}
	got := string(out)
	if !strings.Contains(got, "Missing value for --install-dir") || strings.Contains(got, "Nothing to uninstall") {
		t.Fatalf("unexpected uninstall.sh --install-dir output:\n%s", got)
	}
}

func TestCurlInstallScriptHidesLegacyStackAndPortOptions(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, unwanted := range []string{`PORT="443"`, `STACK=`, "default 443", "preferred shared TCP/UDP port", "Shared proxy port passed to veil install", "--port PORT", "--stack STACK", `--stack "${STACK}"`} {
		if strings.Contains(script, unwanted) {
			t.Fatalf("install.sh should not expose legacy stack/port option %q:\n%s", unwanted, script)
		}
	}
	if !strings.Contains(script, "Veil install only installs Panel; configure protocols as Panel Inbounds") {
		t.Fatalf("install.sh should guide users to configure protocols as Panel Inbounds:\n%s", script)
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

func TestCurlInstallScriptRunsInteractiveInstallFromTTY(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{"run_veil_install()", "< /dev/tty", "run_veil_install"} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh should run interactive veil install from /dev/tty when launched through curl pipe; missing %q:\n%s", want, script)
		}
	}
}

func TestCurlInstallScriptDryRunDoesNotForceInteractivePrompt(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	if strings.Contains(script, `else args+=(--interactive); fi`) {
		t.Fatalf("install.sh should not pass --interactive when --dry-run is set:\n%s", script)
	}
	if !strings.Contains(script, `elif [[ -z "${DRY_RUN}" ]]; then args+=(--interactive); fi`) {
		t.Fatalf("install.sh should guard --interactive behind non-dry-run mode:\n%s", script)
	}
}

func TestCurlInstallScriptResolvesRunBinaryAfterInstallDirFlag(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	marker := "done\n\nRUN_BIN=\"${INSTALL_DIR}/veil\"\n\nrequire_cmd curl"
	if !strings.Contains(script, marker) {
		t.Fatalf("install.sh should resolve RUN_BIN after parsing --install-dir before idempotency path:\n%s", script)
	}
}

func TestCurlInstallScriptDryRunDoesNotExecTempBinaryBeforeCleanup(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{`if [[ -n "${DRY_RUN}" ]]; then`, `"${RUN_BIN}" install "${args[@]}"`, `return $?`} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh dry-run should run temp binary without exec so cleanup trap can run; missing %q:\n%s", want, script)
		}
	}
}

func TestCurlInstallScriptDryRunUsesTempBinaryWithoutInstalling(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{`RUN_BIN="${tmpdir}/veil"`, `if [[ -n "${DRY_RUN}" ]]; then`, `RUN_BIN="${INSTALL_DIR}/veil"`, `exec "${RUN_BIN}" install`} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh dry-run should execute downloaded temp binary without installing; missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, `if [[ "${EUID}" -ne 0 ]]; then`) {
		t.Fatalf("install.sh should not require root for --dry-run:\n%s", script)
	}
	if !strings.Contains(script, `if [[ "${EUID}" -ne 0 && -z "${DRY_RUN}" ]]; then`) {
		t.Fatalf("install.sh root check should be skipped for dry-run:\n%s", script)
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

func TestCurlUninstallScriptSupportsSudoAndCustomPaths(t *testing.T) {
	body, err := os.ReadFile("../../scripts/uninstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{
		"| sudo bash",
		`ETC_DIR="${VEIL_ETC_DIR:-/etc/veil}"`,
		`VAR_DIR="${VEIL_VAR_DIR:-/var/lib/veil}"`,
		`SYSTEMD_DIR="${VEIL_SYSTEMD_DIR:-/etc/systemd/system}"`,
		`--etc-dir "${ETC_DIR}"`,
		`--var-dir "${VAR_DIR}"`,
		`--systemd-dir "${SYSTEMD_DIR}"`,
		`--install-dir "${INSTALL_DIR}"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("uninstall.sh missing %q:\n%s", want, script)
		}
	}
}

func TestCurlUninstallScriptDryRunDoesNotRequireRoot(t *testing.T) {
	body, err := os.ReadFile("../../scripts/uninstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	if strings.Contains(script, `if [[ "${EUID}" -ne 0 ]]; then`) {
		t.Fatalf("uninstall.sh should not require root for --dry-run:\n%s", script)
	}
	if !strings.Contains(script, `if [[ "${EUID}" -ne 0 && -z "${DRY_RUN}" ]]; then`) {
		t.Fatalf("uninstall.sh root check should be skipped for dry-run:\n%s", script)
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
		"bash -n scripts/install.sh scripts/uninstall.sh",
		"bash scripts/install.sh --help >/dev/null",
		"bash scripts/uninstall.sh --help >/dev/null",
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
		"bash -n scripts/install.sh scripts/uninstall.sh",
		"bash scripts/install.sh --help >/dev/null",
		"bash scripts/uninstall.sh --help >/dev/null",
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

func TestCurlInstallScriptChecksumRequiresExactlyOneMatch(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(body)
	for _, want := range []string{
		`count=$(awk -v asset="${asset}" '$2 == asset { count++ } END { print count+0 }' checksums.txt)`,
		`if [[ "${count}" -ne 1 ]]; then`,
		`expected exactly one checksum for ${asset} in checksums.txt, got ${count}`,
		`awk -v asset="${asset}" '$2 == asset { print }' checksums.txt | sha256sum -c -`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing checksum uniqueness guard %q:\n%s", want, script)
		}
	}
}
