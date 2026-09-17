package cli

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readInstallerScript(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "scripts", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(body), "\r\n", "\n")
}

func extractRegion(t *testing.T, script, begin, end string) string {
	t.Helper()
	start := strings.Index(script, begin)
	stop := strings.Index(script, end)
	if start < 0 || stop < 0 || stop <= start {
		t.Fatalf("script missing region %s ... %s", begin, end)
	}
	return script[start : stop+len(end)]
}

func writeExec(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func runBashScript(t *testing.T, dir string, env []string, script string, args ...string) (string, error) {
	t.Helper()
	checkBash(t)
	path := filepath.Join(dir, "harness.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{path}, args...)...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func stubNodeBody(version string) string {
	return "version='" + version + `'
if [ "${STUB_NODE_FAIL:-}" = "1" ]; then
  echo "node: /lib/libc.so.6: version GLIBC_2.28 not found" >&2
  exit 127
fi
case "$1" in
  -v|--version) printf 'v%s\n' "$version"; exit 0 ;;
  -p) printf '%s\n' "${version%%.*}"; exit 0 ;;
esac
exit 1
`
}

func nodeHelperEnv(t *testing.T, stubDir string) []string {
	t.Helper()
	path := stubDir + string(os.PathListSeparator) + "/bin" + string(os.PathListSeparator) + "/usr/bin"
	if runtime.GOOS == "windows" {
		path = stubDir + string(os.PathListSeparator) + os.Getenv("PATH")
	}
	return []string{
		"PATH=" + path,
		"HOME=" + filepath.Dir(stubDir),
		"TMPDIR=" + filepath.Dir(stubDir),
	}
}

func TestMainInstallerRejectsUnsupportedNodeVersions(t *testing.T) {
	script := readInstallerScript(t, "install-main.sh")
	helpers := extractRegion(t, script, "# BEGIN_NODE_HELPERS", "# END_NODE_HELPERS")
	cases := []struct {
		version string
		ok      bool
	}{
		{"18.20.0", false},
		{"20.0.0", false},
		{"20.18.3", false},
		{"20.19.0", true},
		{"21.7.3", false},
		{"22.0.0", false},
		{"22.11.0", false},
		{"22.12.0", true},
		{"23.1.0", false},
		{"24.0.0", true},
		{"26.8.2", true},
	}
	for _, test := range cases {
		t.Run(test.version, func(t *testing.T) {
			dir := t.TempDir()
			stubDir := filepath.Join(dir, "bin")
			if err := os.Mkdir(stubDir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeExec(t, stubDir, "node", stubNodeBody(test.version))
			harness := "#!/bin/sh\nset -eu\n" + helpers + `
if node_ok; then
  echo OK
else
  echo NO
fi
`
			out, err := runBashScript(t, dir, nodeHelperEnv(t, stubDir), harness)
			if err != nil {
				t.Fatalf("node_ok harness failed: %v\n%s", err, out)
			}
			got := strings.TrimSpace(out)
			want := "NO"
			if test.ok {
				want = "OK"
			}
			if got != want {
				t.Fatalf("node %s: got %q want %q\n%s", test.version, got, want, out)
			}
		})
	}
}

func TestMainInstallerTreatsUnusableSystemNodeAsMissing(t *testing.T) {
	script := readInstallerScript(t, "install-main.sh")
	helpers := extractRegion(t, script, "# BEGIN_NODE_HELPERS", "# END_NODE_HELPERS")
	dir := t.TempDir()
	stubDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(stubDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec(t, stubDir, "node", stubNodeBody("26.8.2"))
	harness := "#!/bin/sh\nset -eu\n" + helpers + `
if node_ok; then echo OK; else echo NO; fi
`
	env := append(nodeHelperEnv(t, stubDir), "STUB_NODE_FAIL=1")
	out, err := runBashScript(t, dir, env, harness)
	if err != nil {
		t.Fatalf("unusable node harness failed: %v\n%s", err, out)
	}
	if strings.TrimSpace(out) != "NO" {
		t.Fatalf("unusable system node must fail node_ok so bootstrap still runs, got %q", out)
	}
}

func TestMainInstallerReportsBootstrappedNodeFailure(t *testing.T) {
	script := readInstallerScript(t, "install-main.sh")
	helpers := extractRegion(t, script, "# BEGIN_NODE_HELPERS", "# END_NODE_HELPERS")
	if !strings.Contains(helpers, "ldd") || !strings.Contains(helpers, "PATH=") || !strings.Contains(helpers, "node -v") {
		t.Fatal("diagnose_node must report node -v, ldd, and PATH")
	}
	if strings.Contains(script, `node_ok || { echo "Bootstrapped Node.js is not usable"`) {
		t.Fatal("bootstrapped node failure must not be a one-line death")
	}
	for _, want := range []string{
		`node_ok "$node_bin"`,
		"diagnose_node",
		"Bootstrapped Node.js is not usable",
		"hash -r",
		"$work\"/node-v*/bin/node",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("install-main.sh missing bootstrapped node path/diagnostic %q", want)
		}
	}

	dir := t.TempDir()
	stubDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(stubDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nodeBin := writeExec(t, stubDir, "node", stubNodeBody("26.8.2"))
	harness := "#!/bin/sh\nset -eu\n" + helpers + `
diagnose_node "$1" || true
`
	out, err := runBashScript(t, dir, append(nodeHelperEnv(t, stubDir), "STUB_NODE_FAIL=1"), harness, nodeBin)
	if err != nil {
		t.Fatalf("diagnose_node harness failed: %v\n%s", err, out)
	}
	for _, want := range []string{"PATH=", "node -v", "GLIBC_2.28", nodeBin} {
		if !strings.Contains(out, want) {
			t.Fatalf("diagnostics missing %q\n%s", want, out)
		}
	}
}

func TestMainInstallerPrefersExtractedNodeOverOldPATH(t *testing.T) {
	script := readInstallerScript(t, "install-main.sh")
	helpers := extractRegion(t, script, "# BEGIN_NODE_HELPERS", "# END_NODE_HELPERS")
	dir := t.TempDir()
	oldDir := filepath.Join(dir, "old")
	newDir := filepath.Join(dir, "node-v26.8.2-linux-x64", "bin")
	for _, path := range []string{oldDir, newDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExec(t, oldDir, "node", stubNodeBody("20.18.3"))
	newBin := writeExec(t, newDir, "node", stubNodeBody("26.8.2"))
	harness := "#!/bin/sh\nset -eu\n" + helpers + `
node_bindir="$(dirname "$1")"
PATH="$node_bindir:$PATH"
export PATH
hash -r 2>/dev/null || true
if node_ok "$1"; then echo OK; else echo NO; fi
command -v node
`
	path := newDir + string(os.PathListSeparator) + oldDir + string(os.PathListSeparator) + "/bin" + string(os.PathListSeparator) + "/usr/bin"
	env := []string{"PATH=" + path, "HOME=" + dir}
	out, err := runBashScript(t, dir, env, harness, newBin)
	if err != nil {
		t.Fatalf("extracted node PATH harness failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OK") {
		t.Fatalf("extracted Node must win over unsupported PATH node:\n%s", out)
	}
	if !strings.Contains(out, filepath.ToSlash(newDir)) && !strings.Contains(out, newDir) {
		t.Fatalf("command -v node did not resolve to extracted bin:\n%s", out)
	}
}

func TestMainInstallerCorepackShimsStayInWorkDir(t *testing.T) {
	script := readInstallerScript(t, "install-main.sh")
	helpers := extractRegion(t, script, "# BEGIN_PNPM_BOOTSTRAP", "# END_PNPM_BOOTSTRAP")
	if !strings.Contains(helpers, `--install-directory "$work/bin"`) {
		t.Fatal("corepack enable must install shims under the work directory")
	}
	dir := t.TempDir()
	stubDir := filepath.Join(dir, "stub")
	readonly := filepath.Join(dir, "system")
	work := filepath.Join(dir, "work")
	for _, path := range []string{stubDir, readonly, work} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(readonly, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExec(t, stubDir, "corepack", `
install_dir=""
if [ "$1" = "enable" ]; then
  shift
  while [ $# -gt 0 ]; do
    case "$1" in
      --install-directory)
        install_dir="$2"
        shift 2
        ;;
      *)
        shift
        ;;
    esac
  done
  if [ -z "$install_dir" ]; then
    echo "EACCES: cannot create global Corepack shim in /usr/bin" >&2
    exit 1
  fi
  mkdir -p "$install_dir"
  printf '#!/bin/sh\nif [ "$1" = "--version" ]; then echo 12.4.1; else echo pnpm-from-corepack; fi\n' > "$install_dir/pnpm"
  chmod 0755 "$install_dir/pnpm"
  exit 0
fi
if [ "$1" = "prepare" ]; then
  exit 0
fi
exit 1
`)
	harness := "#!/bin/sh\nset -eu\nwork=\"$1\"\nCI_PNPM_VERSION=12.4.1\n" + helpers + `
ensure_pnpm
command -v pnpm
pnpm
`
	env := []string{
		"PATH=" + stubDir + string(os.PathListSeparator) + readonly + string(os.PathListSeparator) + "/bin" + string(os.PathListSeparator) + "/usr/bin",
		"HOME=" + dir,
	}
	out, err := runBashScript(t, dir, env, harness, work)
	if err != nil {
		t.Fatalf("ensure_pnpm failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "pnpm-from-corepack") {
		t.Fatalf("expected corepack-provided pnpm shim, got:\n%s", out)
	}
	if !strings.Contains(out, filepath.Join(work, "bin")) && !strings.Contains(out, filepath.ToSlash(filepath.Join(work, "bin"))) {
		t.Fatalf("pnpm shim must live under work/bin:\n%s", out)
	}
	entries, err := os.ReadDir(readonly)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "keep" {
		t.Fatalf("corepack wrote outside work dir: %v", entries)
	}
}

// Audit #301: a leftover pnpm shim whose interpreter is gone satisfies
// `command -v` but cannot execute. ensure_pnpm must detect the broken shim,
// bootstrap the pinned pnpm into the installer's private directory, and put it
// first on PATH without touching the user's shim.
func TestMainInstallerRepairsBrokenPnpmShim(t *testing.T) {
	script := readInstallerScript(t, "install-main.sh")
	helpers := extractRegion(t, script, "# BEGIN_PNPM_BOOTSTRAP", "# END_PNPM_BOOTSTRAP")
	dir := t.TempDir()
	stubDir := filepath.Join(dir, "stub")
	work := filepath.Join(dir, "work")
	if err := os.MkdirAll(stubDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	// A stale executable shim: present on PATH, unrunnable.
	brokenShim := filepath.Join(stubDir, "pnpm")
	if err := os.WriteFile(brokenShim, []byte("#!/nonexistent-old-node/bin/node\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec(t, stubDir, "corepack", `
if [ "$1" = "enable" ]; then
  shift
  while [ $# -gt 0 ]; do
    case "$1" in
      --install-directory) install_dir="$2"; shift 2 ;;
      *) shift ;;
    esac
  done
  mkdir -p "$install_dir"
  printf '#!/bin/sh\nif [ "$1" = "--version" ]; then echo 12.4.1; fi\n' > "$install_dir/pnpm"
  chmod 0755 "$install_dir/pnpm"
  exit 0
fi
if [ "$1" = "prepare" ]; then exit 0; fi
exit 1
`)
	harness := "#!/bin/sh\nset -eu\nwork=\"$1\"\nCI_PNPM_VERSION=12.4.1\n" + helpers + `
ensure_pnpm
command -v pnpm
pnpm --version
`
	env := []string{
		"PATH=" + stubDir + string(os.PathListSeparator) + "/bin" + string(os.PathListSeparator) + "/usr/bin",
		"HOME=" + dir,
	}
	out, err := runBashScript(t, dir, env, harness, work)
	if err != nil {
		t.Fatalf("ensure_pnpm must recover from a broken shim: %v\n%s", err, out)
	}
	if !strings.Contains(out, "12.4.1") {
		t.Fatalf("expected pinned pnpm to run, got:\n%s", out)
	}
	if !strings.Contains(out, filepath.Join(work, "bin")) && !strings.Contains(out, filepath.ToSlash(filepath.Join(work, "bin"))) {
		t.Fatalf("pinned pnpm must resolve under work/bin, got:\n%s", out)
	}
}

// Audit #294: the pinned Node build needs OS runtime libraries (libatomic and
// the C/C++ runtime). ensure_runtime_libs must map missing SONAMEs to distro
// packages and install them before the toolchain is launched.
func TestMainInstallerProvisionsMissingNodeRuntimeLibs(t *testing.T) {
	script := readInstallerScript(t, "install-main.sh")
	helpers := extractRegion(t, script, "# BEGIN_DEPS_HELPERS", "# END_DEPS_HELPERS")
	dir := t.TempDir()
	stubDir := filepath.Join(dir, "stub")
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(stubDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec(t, stubDir, "ldd", `
echo '	libatomic.so.1 => not found'
echo '	libstdc++.so.6 => not found'
echo '	libc.so.6 => /lib/x86_64-linux-gnu/libc.so.6'
`)
	writeExec(t, stubDir, "apt-get", `
echo "apt-get $*" >> "$HOME/apt.log"
exit 0
`)
	// Non-root CI runners invoke installs through sudo, which resets PATH and
	// would bypass the apt-get stub; a pass-through sudo keeps the PATH order.
	writeExec(t, stubDir, "sudo", `exec "$@"`)
	writeExec(t, binDir, "node", "#!/bin/sh\nexit 0\n")
	harness := "#!/bin/sh\nset -eu\n" + helpers + `
ensure_runtime_libs "$1"
`
	env := []string{
		"PATH=" + stubDir + string(os.PathListSeparator) + "/bin" + string(os.PathListSeparator) + "/usr/bin",
		"HOME=" + dir,
	}
	nodeBin := filepath.Join(binDir, "node")
	out, err := runBashScript(t, dir, env, harness, nodeBin)
	if err != nil {
		t.Fatalf("ensure_runtime_libs failed: %v\n%s", err, out)
	}
	aptLog, err := os.ReadFile(filepath.Join(dir, "apt.log"))
	if err != nil {
		t.Fatalf("apt-get was not invoked:\n%s", out)
	}
	log := string(aptLog)
	if !strings.Contains(log, "install -y libatomic1 libstdc++6") {
		t.Fatalf("expected narrowly scoped libatomic1+libstdc++6 install, got:\n%s", log)
	}
}

func TestMainInstallerReportsUnresolvableRuntimeLibs(t *testing.T) {
	script := readInstallerScript(t, "install-main.sh")
	helpers := extractRegion(t, script, "# BEGIN_DEPS_HELPERS", "# END_DEPS_HELPERS")
	dir := t.TempDir()
	stubDir := filepath.Join(dir, "stub")
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(stubDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec(t, stubDir, "ldd", `echo '	libatomic.so.1 => not found'`)
	writeExec(t, binDir, "node", "#!/bin/sh\nexit 0\n")
	// Simulate a host with no supported package manager: PATH contains the real
	// toolchain dirs, so override the detector instead of relying on lookup.
	harness := "#!/bin/sh\n" + helpers + `
detect_pkg_manager() { return 1; }
if ensure_runtime_libs "$1"; then echo OK; else echo FAILED; fi
`
	env := []string{
		"PATH=" + stubDir + string(os.PathListSeparator) + "/bin" + string(os.PathListSeparator) + "/usr/bin",
		"HOME=" + dir,
	}
	out, err := runBashScript(t, dir, env, harness, filepath.Join(binDir, "node"))
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "FAILED") || !strings.Contains(out, "libatomic.so.1") {
		t.Fatalf("expected actionable missing-library report, got:\n%s", out)
	}
}

// Audit #300: the installer accepts aarch64/arm64, so both toolchains must
// have verified pins for that architecture instead of amd64-only bailouts.
func TestMainInstallerBootstrapCoversARM64(t *testing.T) {
	script := readInstallerScript(t, "install-main.sh")
	versions := readInstallerScript(t, filepath.Join("ci", "versions.sh"))
	if !strings.Contains(versions, "CI_GO_TARBALL_SHA256_ARM64=") ||
		!strings.Contains(versions, "CI_NODE_TARBALL_SHA256_ARM64=") {
		t.Fatal("versions.sh must pin arm64 checksums for both toolchains")
	}
	for _, needle := range []string{
		`arm64) go_sha256="${CI_GO_TARBALL_SHA256_ARM64`,
		`arm64) node_sha256="${CI_NODE_TARBALL_SHA256_ARM64`,
		"go${CI_GO_VERSION}.linux-${go_arch}.tar.gz",
		"node-v${CI_NODE_VERSION}-linux-${node_arch}",
	} {
		if !strings.Contains(script, needle) {
			t.Fatalf("install-main.sh missing arm64 bootstrap selector %q", needle)
		}
	}
	if !strings.Contains(script, `aarch64|arm64) go_arch="arm64"; node_arch="arm64"`) {
		t.Fatal("install-main.sh must map aarch64/arm64 to the arm64 artifacts")
	}
}

func TestMainInstallerRootHandoffClearsInheritedSudoUID(t *testing.T) {
	script := readInstallerScript(t, "install-main.sh")
	rootAt := strings.Index(script, `if [ "$(id -u)" -eq 0 ]; then`)
	sudoAt := strings.Index(script, "sudo env ")
	if rootAt < 0 || sudoAt < 0 || rootAt > sudoAt {
		t.Fatal("install-main.sh must keep a direct-root handoff before sudo")
	}
	rootBlock := script[rootAt:sudoAt]
	if !strings.Contains(rootBlock, "SUDO_UID=") || !strings.Contains(rootBlock, "SUDO_USER=") {
		t.Fatal("root handoff must clear inherited SUDO_UID/SUDO_USER before verified copy")
	}
	if strings.Contains(rootBlock, "sudo env") {
		t.Fatal("direct-root handoff must not invoke sudo")
	}
}

func TestReleaseBootstrapAllowsRootWithoutSudo(t *testing.T) {
	script := readInstallerScript(t, "install.sh")
	if strings.Contains(script, "python3 sudo") {
		t.Fatal("release bootstrap must not require sudo when already root")
	}
	if !strings.Contains(script, `if [ "$(id -u)" -eq 0 ]; then`) {
		t.Fatal("release bootstrap must have a direct-root verified handoff")
	}
	rootAt := strings.Index(script, `if [ "$(id -u)" -eq 0 ]; then`)
	sudoAt := strings.Index(script, "sudo env ")
	if rootAt < 0 || sudoAt < 0 || rootAt > sudoAt {
		t.Fatal("release bootstrap root handoff must precede the sudo handoff")
	}
	rootBlock := script[rootAt:sudoAt]
	if !strings.Contains(rootBlock, "SUDO_UID=") {
		t.Fatal("root release handoff must clear inherited SUDO_UID")
	}
	if strings.Contains(rootBlock, "sudo env") {
		t.Fatal("direct-root release handoff must not invoke sudo")
	}
}

func TestReleaseBootstrapSelectsVEILVersionAndEqualsFlag(t *testing.T) {
	script := readInstallerScript(t, "install.sh")
	region := extractRegion(t, script, "# BEGIN_RELEASE_TAG_SELECT", "# END_RELEASE_TAG_SELECT")
	if !strings.Contains(region, `${VEIL_VERSION:-latest}`) {
		t.Fatal("release bootstrap must default the tag from VEIL_VERSION")
	}
	cases := []struct {
		name string
		env  []string
		args []string
		want string
		fail bool
	}{
		{name: "default-latest", want: "latest"},
		{name: "env", env: []string{"VEIL_VERSION=v0.7.0"}, want: "v0.7.0"},
		{name: "equals-flag", args: []string{"--version=v0.8.1"}, want: "v0.8.1"},
		{name: "flag-overrides-env", env: []string{"VEIL_VERSION=v0.7.0"}, args: []string{"--version", "v0.9.0"}, want: "v0.9.0"},
		{name: "equals-overrides-env", env: []string{"VEIL_VERSION=v0.7.0"}, args: []string{"--version=v0.8.1", "--yes"}, want: "v0.8.1"},
		{name: "missing-value", args: []string{"--version"}, fail: true},
		{name: "empty-equals", args: []string{"--version="}, fail: true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			harness := "#!/bin/sh\nset -eu\n" + region + "\nprintf '%s\\n' \"$requested_tag\"\n"
			env := append([]string{"PATH=/bin:/usr/bin", "HOME=" + dir}, test.env...)
			out, err := runBashScript(t, dir, env, harness, test.args...)
			if test.fail {
				if err == nil {
					t.Fatalf("expected failure, got %q", out)
				}
				if !strings.Contains(out, "Missing value for --version") {
					t.Fatalf("missing-value error not reported:\n%s", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("selector failed: %v\n%s", err, out)
			}
			if strings.TrimSpace(out) != test.want {
				t.Fatalf("got %q want %q\n%s", strings.TrimSpace(out), test.want, out)
			}
		})
	}
}

func TestPrivilegedInstallerAcceptsEqualsVersionFlag(t *testing.T) {
	checkBash(t)
	installer := filepath.Join("..", "..", "scripts", "install-privileged.sh")
	body, err := os.ReadFile(installer)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(body))
	root := t.TempDir()
	fakeBinary := filepath.Join(root, "veil")
	if err := os.WriteFile(fakeBinary, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("bash", installer, "--unsafe-development", "--dry-run", "--local-bin", fakeBinary, "--yes", "--version=v0.7.0")
	command.Env = append(os.Environ(), "VEIL_INSTALLER_SHA256="+digest)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("privileged installer rejected --version=TAG: %v\n%s", err, output)
	}
}
