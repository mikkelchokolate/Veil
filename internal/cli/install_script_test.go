package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func checkBash(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("skipping test: bash is not available")
	}
	cmd := exec.Command("bash", "-c", "echo")
	if err := cmd.Run(); err != nil {
		t.Skipf("skipping test: bash is not working: %v", err)
	}
}

func TestCurlInstallScriptDownloadsVerifiedReleaseBinary(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{
		`OFFICIAL_REPO="mikkelchokolate/Veil"`,
		`REPO="${OFFICIAL_REPO}"`,
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
	checkBash(t)
	cmd := exec.Command("bash", "../../scripts/install-privileged.sh", "--domain")
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
	checkBash(t)
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
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, unwanted := range []string{`PORT="443"`, `STACK=`, "default 443", "preferred shared TCP/UDP port", "Shared proxy port passed to veil install", "--port PORT", "--stack STACK", `--stack "${STACK}"`} {
		if strings.Contains(script, unwanted) {
			t.Fatalf("install.sh should not expose legacy stack/port option %q:\n%s", unwanted, script)
		}
	}
	if !strings.Contains(script, "Veil install only installs Panel; configure protocols as Panel Inbounds") {
		t.Fatalf("install.sh should guide users to configure protocols as Panel Inbounds:\n%s", script)
	}
}

func TestCurlInstallScriptNeverPipesIntoSudo(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	if strings.Contains(script, "| sudo sh") || strings.Contains(script, "| sudo bash") {
		t.Fatalf("unprivileged bootstrap must never pipe remote bytes into sudo:\n%s", script)
	}
	if !strings.Contains(script, "sudo env") {
		t.Fatalf("verified bootstrap must perform its own final sudo handoff:\n%s", script)
	}
}

func TestCurlInstallScriptRunsInteractiveInstallFromTTY(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{"run_veil_install()", "< /dev/tty", "run_veil_install"} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh should run interactive veil install from /dev/tty when launched through curl pipe; missing %q:\n%s", want, script)
		}
	}
}

func TestCurlInstallScriptDryRunDoesNotForceInteractivePrompt(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	if strings.Contains(script, `else args+=(--interactive); fi`) {
		t.Fatalf("install.sh should not pass --interactive when --dry-run is set:\n%s", script)
	}
	if !strings.Contains(script, `elif [[ -z "${DRY_RUN}" ]]; then args+=(--interactive); fi`) {
		t.Fatalf("install.sh should guard --interactive behind non-dry-run mode:\n%s", script)
	}
}

func TestCurlInstallScriptDoesNotForcePanelAccessInInteractiveMode(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	if strings.Contains(script, `PANEL_ACCESS="local"`) || strings.Contains(script, `args=(--profile "${PROFILE}" --panel-access "${PANEL_ACCESS}")`) {
		t.Fatalf("install.sh should let interactive veil install ask for panel access mode by default:\n%s", script)
	}
	for _, want := range []string{`PANEL_ACCESS=""`, `args=(--profile "${PROFILE}")`, `if [[ -n "${PANEL_ACCESS}" ]]; then args+=(--panel-access "${PANEL_ACCESS}"); fi`} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing panel access passthrough %q:\n%s", want, script)
		}
	}
}

func TestCurlInstallScriptResolvesRunBinaryAfterInstallDirFlag(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	verifyAt := strings.LastIndex(script, "verify_installer_bytes")
	runAt := strings.Index(script, `RUN_BIN="${INSTALL_DIR}/veil"`)
	if verifyAt < 0 || runAt < 0 || runAt < verifyAt {
		t.Fatalf("privileged installer should resolve RUN_BIN after option parsing and self-verification:\n%s", script)
	}
}

func TestCurlInstallScriptDryRunDoesNotExecTempBinaryBeforeCleanup(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{`if [[ -n "${DRY_RUN}" ]]; then`, `"${RUN_BIN}" install "${args[@]}"`, `return $?`} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh dry-run should run temp binary without exec so cleanup trap can run; missing %q:\n%s", want, script)
		}
	}
}

func TestCurlInstallScriptDryRunUsesTempBinaryWithoutInstalling(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
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
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
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
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
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
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
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
	workflow := strings.ReplaceAll(string(body), "\r\n", "\n")
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
	workflow := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{
		"quality:",
		"go test ./... -race -count=1",
		"go vet ./...",
		"make build",
		"gofmt -l",
		"staticcheck",
		"govulncheck ./...",
		"shellcheck scripts/*.sh",
		"sh -n scripts/install.sh",
		"bash -n scripts/install-privileged.sh scripts/uninstall.sh",
		"bash scripts/install-privileged.sh --help >/dev/null",
		"bash scripts/uninstall.sh --help >/dev/null",
		"git diff --check",
		"needs: [quality, release, docker-publish]",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing required release gate %q:\n%s", want, workflow)
		}
	}
}

func TestCiWorkflowEnforcesProductionGates(t *testing.T) {
	// The gate commands live in the shared CI scripts (single source of truth
	// for local VMs and GitHub Actions). ci.yml must route each job to its
	// script, and the scripts must contain the gates.
	read := func(path string) string {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(body), "\r\n", "\n")
	}
	workflow := read("../../.github/workflows/ci.yml")
	for _, want := range []string{
		"scripts/ci/frontend.sh",
		"scripts/ci/test.sh",
		"scripts/ci/lint.sh",
		"scripts/ci/privilege-boundary.sh",
		"scripts/ci/e2e.sh",
		"scripts/ci/browser-e2e.sh",
		"scripts/ci/package-smoke.sh",
		"scripts/ci/image-build.sh",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("ci.yml does not route to shared CI script %q:\n%s", want, workflow)
		}
	}

	testScript := read("../../scripts/ci/test.sh")
	for _, want := range []string{
		"go test ./sdk/go -race -count=1",
		"go list ./... | grep -v '/sdk/go$'",
		"go test ${packages} -race -count=1 -coverprofile=coverage.out",
		"go vet ./...",
		"make build",
		"gofmt -l",
	} {
		if !strings.Contains(testScript, want) {
			t.Fatalf("scripts/ci/test.sh missing required gate %q", want)
		}
	}

	lintScript := read("../../scripts/ci/lint.sh")
	for _, want := range []string{
		"staticcheck",
		"govulncheck ./...",
		"shellcheck scripts/*.sh",
		"sh -n scripts/install.sh",
		"bash -n scripts/install-privileged.sh scripts/uninstall.sh",
		"bash scripts/install-privileged.sh --help >/dev/null",
		"bash scripts/uninstall.sh --help >/dev/null",
	} {
		if !strings.Contains(lintScript, want) {
			t.Fatalf("scripts/ci/lint.sh missing required gate %q", want)
		}
	}

	fastScript := read("../../scripts/ci/fast.sh")
	if !strings.Contains(fastScript, "git diff --check") {
		t.Fatalf("scripts/ci/fast.sh missing required gate %q", "git diff --check")
	}
}

func TestReadmeDocumentsBackupRollbackAuditWorkflow(t *testing.T) {
	body, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := strings.ReplaceAll(string(body), "\r\n", "\n")
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
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
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
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{
		"--force",
		"FORCE",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing --force flag for forced re-install; found none of [--force, FORCE]:\n%s", script)
		}
	}
}

func TestCurlInstallScriptUpgradesExistingOlderBinary(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{
		"installed_veil_version()",
		"resolve_target_version()",
		"Installed Veil ${current_version} is older than target ${target_version}; upgrading.",
		"Installed Veil ${current_version} differs from target ${target_version}; installing requested release.",
		"already up to date",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing existing-version upgrade logic %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "Use --force to re-install\n  args=(--profile") {
		t.Fatalf("install.sh should not unconditionally skip download when a binary already exists:\n%s", script)
	}
}

func TestCurlInstallScriptChecksumRequiresExactlyOneMatch(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
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
