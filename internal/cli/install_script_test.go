package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		"--no-same-owner",
		"/usr/local/bin",
		`"${RUN_BIN}" install`,
		"run_veil_install",
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
	for _, want := range []string{`RUN_BIN="${tmpdir}/veil"`, `if [[ -n "${DRY_RUN}" ]]; then`, `RUN_BIN="${INSTALL_DIR}/veil"`, `run_veil_install`} {
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

func TestPrivilegedInstallerForwardsLeIPCertFalse(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install-privileged.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{
		"--le-ip-cert=*)",
		"--no-le-ip-cert",
		"args+=(--le-ip-cert=false)",
		"args+=(--le-ip-cert=true)",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install-privileged.sh missing LE IP cert opt-out %q:\n%s", want, script)
		}
	}

	checkBash(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "../../scripts/install-privileged.sh", "--le-ip-cert=not-a-bool")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected invalid --le-ip-cert to fail, got:\n%s", out)
	}
	if ctx.Err() != nil {
		t.Fatalf("invalid --le-ip-cert hung instead of exiting: %v\n%s", ctx.Err(), out)
	}
	if !strings.Contains(string(out), "Invalid boolean value") {
		t.Fatalf("unexpected invalid boolean output:\n%s", out)
	}
}

func TestUninstallScriptFailsClosedWhenBinaryMissingButStateRemains(t *testing.T) {
	checkBash(t)
	root := t.TempDir()
	installDir := filepath.Join(root, "bin")
	etcDir := filepath.Join(root, "etc")
	varDir := filepath.Join(root, "var")
	systemdDir := filepath.Join(root, "systemd")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "veil.env"), []byte("VEIL_API_TOKEN=leftover\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		t.Fatal(err)
	}

	run := func(extra ...string) (string, error) {
		t.Helper()
		args := append([]string{
			"../../scripts/uninstall.sh",
			"--install-dir", installDir,
			"--etc-dir", etcDir,
			"--var-dir", varDir,
			"--systemd-dir", systemdDir,
		}, extra...)
		cmd := exec.Command("bash", args...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	out, err := run("--dry-run")
	if err != nil {
		t.Fatalf("leftover dry-run failed: %v\n%s", err, out)
	}
	if strings.Contains(out, "Nothing to uninstall") {
		t.Fatalf("leftover state must not be reported as empty:\n%s", out)
	}
	if !strings.Contains(out, etcDir) {
		t.Fatalf("leftover dry-run should mention %s:\n%s", etcDir, out)
	}

	out, err = run()
	if err == nil {
		t.Fatalf("leftover uninstall without --yes must fail closed, got:\n%s", out)
	}
	if strings.Contains(out, "Nothing to uninstall") {
		t.Fatalf("leftover uninstall without --yes must not claim success:\n%s", out)
	}

	emptyRoot := t.TempDir()
	cmd := exec.Command("bash", "../../scripts/uninstall.sh",
		"--install-dir", filepath.Join(emptyRoot, "bin"),
		"--etc-dir", filepath.Join(emptyRoot, "etc"),
		"--var-dir", filepath.Join(emptyRoot, "var"),
		"--systemd-dir", filepath.Join(emptyRoot, "systemd"),
		"--dry-run",
	)
	outBytes, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("empty uninstall dry-run failed: %v\n%s", err, outBytes)
	}
	if !strings.Contains(string(outBytes), "Nothing to uninstall") {
		t.Fatalf("empty host should still report nothing to uninstall:\n%s", outBytes)
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
		"sha256sum veil_linux_amd64.tar.gz veil_linux_arm64.tar.gz",
		"checksums.txt",
		"gh release create",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing %q:\n%s", want, workflow)
		}
	}
	if strings.Contains(workflow, "sha256sum ./veil_linux_") {
		t.Fatal("release checksums must use asset basenames, not ./-prefixed paths")
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
		// #668: the release gate runs the same shared Required scripts as
		// ci.yml — a hand-rolled subset is not the ship gate.
		"scripts/ci/frontend.sh",
		"scripts/ci/test.sh",
		"scripts/ci/lint.sh",
		"scripts/ci/privilege-boundary.sh",
		"scripts/ci/multi-process.sh",
		"scripts/ci/sigkill.sh",
		"scripts/ci/filesystem-faults.sh",
		"scripts/ci/install-acceptance.sh",
		"go vet ./...",
		"make build",
		"gofmt -l",
		"staticcheck",
		"govulncheck ./...",
		"shellcheck scripts/*.sh",
		"sh -n scripts/install.sh",
		"sh -n scripts/install-main.sh",
		"bash -n scripts/install-privileged.sh scripts/uninstall.sh",
		"bash scripts/install-privileged.sh --help >/dev/null",
		"bash scripts/uninstall.sh --help >/dev/null",
		"bash scripts/install-main.sh --help >/dev/null",
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
		"ci-frontend-dist-${{ github.sha }}",
		"actions/download-artifact@3e5f45b2cfb9172054b4087a40e8e0b5a5461e7c",
		"needs: frontend",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("ci.yml does not route to shared CI script %q:\n%s", want, workflow)
		}
	}

	// #670 + ruleset contract: the branch ruleset requires a literal
	// "package-smoke" check context. Matrix legs report as
	// "package-smoke (<arch>)", so ci.yml must keep an aggregator job named
	// package-smoke that depends on the matrix and fails if any arch fails.
	for _, want := range []string{
		"package-smoke-matrix:",
		"arch: [amd64, arm64]",
		"package-smoke:\n    needs: package-smoke-matrix",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("ci.yml lost the literal package-smoke check context %q:\n%s", want, workflow)
		}
	}

	testScript := read("../../scripts/ci/test.sh")
	for _, want := range []string{
		"go test ./sdk/go -race -count=1",
		"go list ./... | grep -v '/sdk/go$'",
		"${CI_SCRIPTS_DIR}/prepare-frontend-dist.sh",
		"${CI_SCRIPTS_DIR}/test-orchestrator.py",
		"${CI_SCRIPTS_DIR}/verify-test-shards.py",
		"${CI_SCRIPTS_DIR}/test-inventory.py",
		"coverage.out",
		"go vet ./...",
		"make build",
		"gofmt -l",
	} {
		if !strings.Contains(testScript, want) {
			t.Fatalf("scripts/ci/test.sh missing required gate %q", want)
		}
	}

	frontendScript := read("../../scripts/ci/frontend.sh")
	for _, want := range []string{"frontend_dist_artifact_dir", "source.sha", "git -C \"${CI_ROOT}\" rev-parse HEAD"} {
		if !strings.Contains(frontendScript, want) {
			t.Fatalf("scripts/ci/frontend.sh missing source-keyed artifact gate %q", want)
		}
	}
	prepareFrontendScript := read("../../scripts/ci/prepare-frontend-dist.sh")
	for _, want := range []string{"CI_FRONTEND_DIST_ARTIFACT_DIR", "git rev-parse HEAD", "source.sha", "frontend-install-build"} {
		if !strings.Contains(prepareFrontendScript, want) {
			t.Fatalf("prepare-frontend-dist.sh missing artifact gate %q", want)
		}
	}

	// The live API shard path is test-orchestrator.py (bounded LPT scheduler)
	// verified by verify-test-shards.py — the retired api-shards.sh was a dead
	// comment contract (issue #429).
	orchestrator := read("../../scripts/ci/test-orchestrator.py")
	for _, want := range []string{
		"--api-shards",
		"--serial-roots",
		"TestRollbackPreservesRuntimeIdentityAndProtocolConfigBytes",
		"-coverprofile=",
		"coverage-api-serial.out",
		"missing_profiles",
	} {
		if !strings.Contains(orchestrator, want) {
			t.Fatalf("scripts/ci/test-orchestrator.py missing required gate %q", want)
		}
	}

	lintScript := read("../../scripts/ci/lint.sh")
	for _, want := range []string{
		"staticcheck",
		"govulncheck ./...",
		"shellcheck scripts/*.sh",
		"sh -n scripts/install.sh",
		"sh -n scripts/install-main.sh",
		"bash -n scripts/install-privileged.sh scripts/uninstall.sh",
		"bash scripts/install-privileged.sh --help >/dev/null",
		"bash scripts/uninstall.sh --help >/dev/null",
		"bash scripts/install-main.sh --help >/dev/null",
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

	// The runnable commands must appear inside fenced code blocks — prose
	// mentions of "rollback restore" do not prove an operator-run example
	// exists.
	var fenced strings.Builder
	inFence := false
	for _, line := range strings.Split(readme, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			fenced.WriteString(line + "\n")
		}
	}
	examples := fenced.String()
	for _, want := range []string{
		"veil repair --backup-dir",
		"veil rollback list --backup-dir",
		"veil rollback restore",
		"veil rollback cleanup",
		"--audit-log",
	} {
		if !strings.Contains(examples, want) {
			t.Fatalf("README.md has no fenced example running %q:\n%s", want, examples)
		}
	}

	// The surrounding prose must still explain the audit format and the
	// destructive/preview split.
	for _, want := range []string{"JSONL", "dry-run", "writable"} {
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
		`count=$(awk -v asset="${asset}" '($2 == asset || $2 == "./" asset) { count++ } END { print count+0 }' checksums.txt)`,
		`if [[ "${count}" -ne 1 ]]; then`,
		`expected exactly one checksum for ${asset} in checksums.txt, got ${count}`,
		`awk -v asset="${asset}" '($2 == asset || $2 == "./" asset) { print }' checksums.txt | sha256sum -c -`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing checksum uniqueness guard %q:\n%s", want, script)
		}
	}
}

func TestBootstrapChecksumAcceptsCanonicalAndDotSlashAssetNames(t *testing.T) {
	body, err := os.ReadFile("../../scripts/install.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{
		`count="$(awk -v asset="$asset" '($2 == asset || $2 == "./" asset) { count++ } END { print count+0 }' checksums.txt)"`,
		`awk -v asset="$asset" '($2 == asset || $2 == "./" asset) { print }' checksums.txt | sha256sum -c - >/dev/null`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("bootstrap missing compatible checksum match %q:\n%s", want, script)
		}
	}
}
