package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// releaseWorkflowStep is the subset of a GitHub Actions step the supply-chain
// gates inspect. Parsing YAML (instead of grepping raw text) means a comment
// or dead key cannot satisfy the test — the gates only pass when a real
// `uses:`/`run:`/`with:` field carries the value.
type releaseWorkflowStep struct {
	Name string         `yaml:"name"`
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	With map[string]any `yaml:"with"`
}

func releaseWorkflowSteps(t *testing.T) (map[string]string, []releaseWorkflowStep) {
	t.Helper()
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Permissions map[string]string `yaml:"permissions"`
		Jobs        map[string]struct {
			Steps []releaseWorkflowStep `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(body, &workflow); err != nil {
		t.Fatalf("release.yml must parse as workflow YAML: %v", err)
	}
	var steps []releaseWorkflowStep
	for _, job := range workflow.Jobs {
		steps = append(steps, job.Steps...)
	}
	if len(steps) == 0 {
		t.Fatal("release.yml has no job steps")
	}
	return workflow.Permissions, steps
}

func stepHasUse(steps []releaseWorkflowStep, prefix string) bool {
	for _, step := range steps {
		if strings.HasPrefix(step.Uses, prefix) {
			return true
		}
	}
	return false
}

func stepRunContains(steps []releaseWorkflowStep, want string) bool {
	for _, step := range steps {
		if strings.Contains(step.Run, want) {
			return true
		}
	}
	return false
}

// TestReleaseWorkflowBuildsSignedPackagesAndSBOM locks in the supply-chain
// release gates by walking the parsed workflow steps: native deb/rpm/apk
// packages built by nfpm, an SPDX SBOM, keyless cosign signatures, and build
// provenance attestation — each proven by an actual `uses:`/`run:`/`with:`
// field, never by a comment or step name alone.
func TestReleaseWorkflowBuildsSignedPackagesAndSBOM(t *testing.T) {
	permissions, steps := releaseWorkflowSteps(t)

	for _, perm := range []string{"id-token", "attestations"} {
		if got := permissions[perm]; got != "write" {
			t.Fatalf("release workflow permissions[%q] = %q, want \"write\" (keyless signing/attestation)", perm, got)
		}
	}

	// Native packages: an actual nfpm invocation against the packaged config.
	// The binary is invoked as `"$(go env GOPATH)/bin/nfpm" package ..., so
	// match the invocation arguments rather than a single `nfpm package` token.
	var nfpmStep bool
	for _, step := range steps {
		if strings.Contains(step.Run, "nfpm") && strings.Contains(step.Run, "package --config packaging/nfpm.yaml") {
			nfpmStep = true
		}
	}
	if !nfpmStep {
		t.Fatal("release workflow has no `run:` step invoking `nfpm package --config packaging/nfpm.yaml`")
	}

	// SBOM: a real sbom-action step plus a run step that stages the SPDX file
	// into dist/ for signing and upload.
	if !stepHasUse(steps, "anchore/sbom-action@") {
		t.Fatal("release workflow has no `uses: anchore/sbom-action@…` step")
	}
	if !stepRunContains(steps, "dist/veil.sbom.spdx.json") {
		t.Fatal("release workflow has no `run:` step staging dist/veil.sbom.spdx.json")
	}

	// Keyless signatures: the installer action must be pinned, and a run step
	// must actually invoke cosign sign-blob.
	if !stepHasUse(steps, "sigstore/cosign-installer@") {
		t.Fatal("release workflow has no `uses: sigstore/cosign-installer@…` step")
	}
	if !stepRunContains(steps, "cosign sign-blob") {
		t.Fatal("release workflow has no `run:` step invoking `cosign sign-blob`")
	}

	// Provenance: an attest-build-provenance step whose subject-path lists the
	// real release artifacts — including every native package glob.
	var attested bool
	for _, step := range steps {
		if !strings.HasPrefix(step.Uses, "actions/attest-build-provenance@") {
			continue
		}
		attested = true
		var subjects string
		for _, key := range []string{"subject-path", "subject-paths"} {
			if v, ok := step.With[key].(string); ok {
				subjects += "\n" + v
			}
		}
		for _, want := range []string{
			"dist/checksums.txt",
			"dist/veil.sbom.spdx.json",
			"dist/veil.provenance.json",
			"dist/veil_linux_*.tar.gz",
			"dist/*.deb",
			"dist/*.rpm",
			"dist/*.apk",
		} {
			if !strings.Contains(subjects, want) {
				t.Fatalf("attest-build-provenance subject paths missing %q:\n%s", want, subjects)
			}
		}
	}
	if !attested {
		t.Fatal("release workflow has no `uses: actions/attest-build-provenance@…` step")
	}

	// Container image: the build-push step must emit max-mode provenance.
	var maxProvenance bool
	for _, step := range steps {
		if strings.HasPrefix(step.Uses, "docker/build-push-action@") && step.With["provenance"] == "mode=max" {
			maxProvenance = true
		}
	}
	if !maxProvenance {
		t.Fatal("release workflow docker/build-push-action step missing `with: provenance: mode=max`")
	}
}

func TestGitHubActionsArePinnedAndSecurityScanned(t *testing.T) {
	actionUseLine := regexp.MustCompile(`(?m)^\s*uses:\s+[^\s#]+`)
	pinnedAction := regexp.MustCompile(`@[0-9a-f]{40}(?:\s|$|#)`)
	for _, workflowPath := range []string{
		"../../.github/workflows/ci.yml",
		"../../.github/workflows/release.yml",
		"../../.github/workflows/codeql.yml",
	} {
		body, err := os.ReadFile(workflowPath)
		if err != nil {
			t.Fatal(err)
		}
		workflow := strings.ReplaceAll(string(body), "\r\n", "\n")
		for _, line := range actionUseLine.FindAllString(workflow, -1) {
			if !pinnedAction.MatchString(line) {
				t.Fatalf("%s contains unpinned GitHub Action reference %q", workflowPath, line)
			}
		}
	}

	codeql, err := os.ReadFile("../../.github/workflows/codeql.yml")
	if err != nil {
		t.Fatal(err)
	}
	codeqlConfig := strings.ReplaceAll(string(codeql), "\r\n", "\n")
	codeqlPinnedAction := regexp.MustCompile(`github/codeql-action/(init|analyze)@[0-9a-f]{40}`)
	codeqlActions := map[string]bool{}
	for _, match := range codeqlPinnedAction.FindAllStringSubmatch(codeqlConfig, -1) {
		codeqlActions[match[1]] = true
	}
	for _, action := range []string{"init", "analyze"} {
		if !codeqlActions[action] {
			t.Fatalf("CodeQL workflow must pin %s action by commit SHA", action)
		}
	}

	ci, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	ciWorkflow := strings.ReplaceAll(string(ci), "\r\n", "\n")
	for _, want := range []string{"image-build:", "scripts/ci/image-build.sh"} {
		if !strings.Contains(ciWorkflow, want) {
			t.Fatalf("ci.yml missing Docker build verification %q", want)
		}
	}
	imageBuild, err := os.ReadFile("../../scripts/ci/image-build.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"docker build", "veil:ci"} {
		if !strings.Contains(string(imageBuild), want) {
			t.Fatalf("scripts/ci/image-build.sh missing Docker build verification %q", want)
		}
	}

	dependabot, err := os.ReadFile("../../.github/dependabot.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"gomod", "docker", "github-actions", "npm", "directory: /web", "directory: /test/browser"} {
		if !strings.Contains(string(dependabot), want) {
			t.Fatalf("dependabot.yml missing ecosystem %q", want)
		}
	}
	dependabotConfig := strings.ReplaceAll(string(dependabot), "\r\n", "\n")
	for _, want := range []string{"go-modules:", "container-images:", "github-actions-updates:", "web-dependencies:", "browser-test-dependencies:"} {
		if !strings.Contains(dependabotConfig, want) {
			t.Fatalf("dependabot.yml missing grouped updates policy %q", want)
		}
	}
	if got := strings.Count(dependabotConfig, "open-pull-requests-limit: 1"); got != 5 {
		t.Fatalf("dependabot.yml should cap each ecosystem at one grouped PR, got %d limits", got)
	}

	makefile, err := os.ReadFile("../../Makefile")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"verify-openapi", "verify-release"} {
		if !strings.Contains(string(makefile), want) {
			t.Fatalf("Makefile missing %q target", want)
		}
	}
}

// TestNfpmConfigShipsBinaryAndUnits verifies the package definition delivers
// the Panel binary and the managed systemd units. The config is parsed as
// YAML — a `# dst:` comment or a stray key in an overrides block cannot
// satisfy a content/script assertion (issue #774).
func TestNfpmConfigShipsBinaryAndUnits(t *testing.T) {
	body, err := os.ReadFile("../../packaging/nfpm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Name     string `yaml:"name"`
		Contents []struct {
			Dst string `yaml:"dst"`
			Src string `yaml:"src"`
		} `yaml:"contents"`
		Scripts map[string]string `yaml:"scripts"`
	}
	if err := yaml.Unmarshal(body, &cfg); err != nil {
		t.Fatalf("nfpm.yaml is not valid YAML: %v", err)
	}
	if cfg.Name != "veil" {
		t.Fatalf("nfpm package name = %q, want veil", cfg.Name)
	}
	dsts := map[string]string{}
	for _, entry := range cfg.Contents {
		dsts[entry.Dst] = entry.Src
	}
	for _, dst := range []string{
		"/usr/local/bin/veil",
		"/lib/systemd/system/veil.service",
		"/lib/systemd/system/veil-helper.service",
		"/lib/systemd/system/veil-helper.socket",
		"/lib/systemd/system/veil-backup.service",
		"/lib/systemd/system/veil-backup.timer",
		"/lib/systemd/system/veil-caddy.service",
		"/lib/systemd/system/veil-hysteria2@.service",
		"/lib/systemd/system/veil-olcrtc@.service",
		"/lib/systemd/system/veil-mieru.service",
		"/lib/systemd/system/veil-warp.service",
	} {
		src, ok := dsts[dst]
		if !ok {
			t.Fatalf("nfpm contents missing dst %q", dst)
		}
		// src and dst must name the same file so a swapped src cannot
		// silently ship a different unit under a required path.
		if filepath.ToSlash(filepath.Base(src)) != filepath.ToSlash(filepath.Base(dst)) {
			t.Fatalf("nfpm contents entry for %s ships mismatched src %s", dst, src)
		}
	}
	for hook, want := range map[string]string{
		"postinstall": "packaging/scripts/postinstall.sh",
		"preremove":   "packaging/scripts/preremove.sh",
		"postremove":  "packaging/scripts/postremove.sh",
	} {
		if cfg.Scripts[hook] != want {
			t.Fatalf("nfpm scripts.%s = %q, want %q", hook, cfg.Scripts[hook], want)
		}
	}
}

// shellCode strips comment lines so a `# systemctl …` remark cannot satisfy a
// check that is meant to prove a live invocation.
func shellCode(body string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// TestPackageScriptsExist ensures the packaging maintainer scripts are present
// and perform real systemctl lifecycle invocations — not just comments that
// mention systemd.
func TestPackageScriptsExist(t *testing.T) {
	scripts := map[string][]string{
		"../../packaging/scripts/postinstall.sh": {"systemctl daemon-reload", "systemctl enable", "systemctl try-restart"},
		"../../packaging/scripts/preremove.sh":   {"systemctl stop", "systemctl disable"},
		"../../packaging/scripts/postremove.sh":  {"systemctl daemon-reload"},
	}
	for script, invocations := range scripts {
		body, err := os.ReadFile(script)
		if err != nil {
			t.Fatalf("missing packaging script %s: %v", script, err)
		}
		code := shellCode(strings.ReplaceAll(string(body), "\r\n", "\n"))
		for _, want := range invocations {
			if !strings.Contains(code, want) {
				t.Fatalf("packaging script %s has no live %q invocation", script, want)
			}
		}
	}
	preremove, err := os.ReadFile("../../packaging/scripts/preremove.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := stripHashComments(t, strings.ReplaceAll(string(preremove), "\r\n", "\n"))
	for _, want := range []string{"is_upgrade", "upgrade|deconfigure|failed-upgrade", "[ \"$arg\" -gt 0 ]"} {
		if !strings.Contains(script, want) {
			t.Fatalf("preremove.sh missing upgrade guard %q:\n%s", want, script)
		}
	}
	postinstall, err := os.ReadFile("../../packaging/scripts/postinstall.sh")
	if err != nil {
		t.Fatal(err)
	}
	postinstallScript := stripHashComments(t, strings.ReplaceAll(string(postinstall), "\r\n", "\n"))
	// #775: backup members store restore mode in their own permission bits.
	// The real contract is structural — the backup-dir loop must chmod
	// directories only; a `-type f` normalization would silently flatten
	// member modes and rollback would restore the wrong mode. Asserting the
	// comment would be comment-satisfiable.
	loopStart := strings.Index(postinstallScript, "for dir in backups promotion-backups migration-backups")
	if loopStart < 0 {
		t.Fatal("postinstall.sh lost the backup-dir ownership loop over backups/promotion-backups/migration-backups")
	}
	loopEnd := strings.Index(postinstallScript[loopStart:], "\ndone")
	if loopEnd < 0 {
		t.Fatal("postinstall.sh backup-dir loop is not terminated by done")
	}
	backupLoop := postinstallScript[loopStart : loopStart+loopEnd]
	if !strings.Contains(backupLoop, `install -d -m 0700 -o root -g root "/var/lib/veil/$dir"`) {
		t.Fatalf("backup dirs must be root-owned 0700:\n%s", backupLoop)
	}
	if !strings.Contains(backupLoop, "-type d -exec chmod 0700") {
		t.Fatalf("backup-dir loop must normalize directories to 0700:\n%s", backupLoop)
	}
	if strings.Contains(backupLoop, "-type f -exec chmod") {
		t.Fatalf("backup-dir loop must NOT normalize member files — restore mode lives in member permission bits:\n%s", backupLoop)
	}
	if !strings.Contains(postinstallScript, "/etc/veil/panel") {
		t.Fatal("postinstall.sh must migrate Panel TLS material under /etc/veil/panel")
	}
	if strings.Contains(postinstallScript, "usermod -aG veil-proxy veil || true") ||
		strings.Contains(postinstallScript, "addgroup veil veil-proxy || true") {
		t.Fatal("postinstall.sh must not swallow veil-proxy group membership failures")
	}
}

func TestDockerEntrypointKeepsApplyRootOffLiveGeneratedTree(t *testing.T) {
	body, err := os.ReadFile("../../packaging/docker/entrypoint.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(string(body), "\r\n", "\n")
	if strings.Contains(script, "VEIL_APPLY_ROOT:-/etc/veil}") || strings.Contains(script, "VEIL_APPLY_ROOT=/etc/veil") {
		t.Fatal("container entrypoint must not default VEIL_APPLY_ROOT to the live config tree")
	}
	// #672: leaf envs default from the VEIL_VAR_DIR/VEIL_ETC_DIR roots so a
	// custom-roots container is not pinned back onto the packaged tree, while
	// an explicit leaf override still wins.
	for _, want := range []string{
		`VEIL_STATE_PATH:-${VEIL_VAR_DIR:-/var/lib/veil}/state.json}`,
		`VEIL_APPLY_ROOT:-${VEIL_VAR_DIR:-/var/lib/veil}/staging}`,
		`VEIL_KEY_PATH:-${VEIL_ETC_DIR:-/etc/veil}/state.key}`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("container entrypoint must derive leaf default from the root env: %s", want)
		}
	}
}

func TestSystemdUnitsShipHardenedByDefault(t *testing.T) {
	runtimeUnits := []string{
		"../../packaging/systemd/veil-caddy.service",
		"../../packaging/systemd/veil-hysteria2@.service",
		"../../packaging/systemd/veil-olcrtc@.service",
		"../../packaging/systemd/veil-mieru.service",
		"../../packaging/systemd/veil-warp.service",
	}
	for _, unit := range append([]string{
		"../../packaging/systemd/veil.service",
		"../../packaging/systemd/veil-helper.service",
	}, runtimeUnits...) {
		body, err := os.ReadFile(unit)
		if err != nil {
			t.Fatalf("missing systemd unit %s: %v", unit, err)
		}
		config := strings.ReplaceAll(string(body), "\r\n", "\n")
		for _, want := range []string{
			"NoNewPrivileges=true",
			"ProtectSystem=strict",
			"ProtectHome=yes",
			"PrivateTmp=true",
			"SystemCallArchitectures=native",
			"ProtectKernelTunables=true",
			"ProtectKernelModules=true",
			"ProtectControlGroups=true",
			"RestrictSUIDSGID=true",
			"LockPersonality=true",
			"RestrictRealtime=true",
			"MemoryDenyWriteExecute=true",
		} {
			if !strings.Contains(config, want) {
				t.Fatalf("systemd unit %s missing hardening directive %q:\n%s", unit, want, config)
			}
		}
		wantUMask := "UMask=0077"
		if strings.HasSuffix(unit, "veil-mieru.service") {
			// mita's appctl UDS must stay group-writable for the veil panel.
			wantUMask = "UMask=0007"
		}
		if !strings.Contains(config, wantUMask) {
			t.Fatalf("systemd unit %s missing %q:\n%s", unit, wantUMask, config)
		}
	}
	protocolUnits := []string{
		"../../packaging/systemd/veil-caddy.service",
		"../../packaging/systemd/veil-hysteria2@.service",
		"../../packaging/systemd/veil-olcrtc@.service",
		"../../packaging/systemd/veil-mieru.service",
		"../../packaging/systemd/veil-warp.service",
	}
	for _, unit := range runtimeUnits {
		body, err := os.ReadFile(unit)
		if err != nil {
			t.Fatal(err)
		}
		config := string(body)
		for _, want := range []string{
			"CapabilityBoundingSet=CAP_NET_BIND_SERVICE",
			"AmbientCapabilities=CAP_NET_BIND_SERVICE",
			"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
		} {
			if !strings.Contains(config, want) {
				t.Fatalf("runtime unit %s missing %q", unit, want)
			}
		}
	}
	panelUser := "User=veil"
	for _, unit := range protocolUnits {
		body, err := os.ReadFile(unit)
		if err != nil {
			t.Fatal(err)
		}
		config := strings.ReplaceAll(string(body), "\r\n", "\n")
		if strings.HasSuffix(unit, "veil-mieru.service") {
			// mieru runs as the dedicated veil-mita identity (issue #624): the
			// appctl UDS is a control plane, so it must not share the
			// veil-proxy edge uid/gid other protocol units use.
			for _, want := range []string{
				"User=veil-mita\n", "Group=veil-mita\n",
				"SupplementaryGroups=veil-proxy", "RuntimeDirectoryMode=0750",
			} {
				if !strings.Contains(config, want) {
					t.Fatalf("veil-mieru.service missing %q:\n%s", want, config)
				}
			}
			if strings.Contains(config, "User=veil-proxy") || strings.Contains(config, "Group=veil-proxy\n") {
				t.Fatalf("veil-mieru.service must not run as the shared veil-proxy identity:\n%s", config)
			}
		} else if !strings.Contains(config, "User=veil-proxy") || !strings.Contains(config, "Group=veil-proxy") {
			t.Fatalf("protocol unit %s must run as veil-proxy, not the Panel UID:\n%s", unit, config)
		}
		if strings.Contains(config, panelUser+"\n") {
			t.Fatalf("protocol unit %s must not share User=veil with veil.service:\n%s", unit, config)
		}
		if strings.Contains(config, "ReadWritePaths=/var/lib/veil") || strings.Contains(config, "ReadWritePaths=/etc/veil") {
			t.Fatalf("protocol unit %s must not remount Panel state writable:\n%s", unit, config)
		}
		for _, want := range []string{
			"InaccessiblePaths=/run/veil/helper.sock /var/lib/veil",
		} {
			if !strings.Contains(config, want) {
				t.Fatalf("protocol unit %s missing %q:\n%s", unit, want, config)
			}
		}
	}
	// veil-caddy.service is asserted with the other veil-proxy protocol units
	// above (issue #497); it additionally needs ReadOnlyPaths for /etc/veil
	// because its live config lives under /etc/veil/generated/caddy.
	caddyBody, err := os.ReadFile("../../packaging/systemd/veil-caddy.service")
	if err != nil {
		t.Fatal(err)
	}
	caddyConfig := strings.ReplaceAll(string(caddyBody), "\r\n", "\n")
	for _, want := range []string{"User=veil-proxy\n", "Group=veil-proxy\n", "PrivateDevices=true", "ReadOnlyPaths=/etc/veil"} {
		if !strings.Contains(caddyConfig, want) {
			t.Fatalf("veil-caddy.service missing %q:\n%s", want, caddyConfig)
		}
	}
	if strings.Contains(caddyConfig, "ReadWritePaths=/etc/veil") || strings.Contains(caddyConfig, "ReadWritePaths=/var/lib/veil") {
		t.Fatalf("veil-caddy.service must not remount Veil paths writable:\n%s", caddyConfig)
	}
	// Caddy is internet-facing: it must not reach the helper socket or the
	// panel state tree (audit #507/#508).
	for _, want := range []string{"InaccessiblePaths=/run/veil/helper.sock /var/lib/veil"} {
		if !strings.Contains(caddyConfig, want) {
			t.Fatalf("veil-caddy.service missing %q:\n%s", want, caddyConfig)
		}
	}
	for _, unit := range runtimeUnits {
		body, err := os.ReadFile(unit)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.ReplaceAll(string(body), "\r\n", "\n"), "User=") {
			t.Fatalf("runtime unit %s is missing User=", unit)
		}
	}
	// The unprivileged Panel keeps an empty capability set; it delegates privileged
	// work to the helper.
	panelBody, err := os.ReadFile("../../packaging/systemd/veil.service")
	if err != nil {
		t.Fatal(err)
	}
	panelConfig := strings.ReplaceAll(string(panelBody), "\r\n", "\n")
	for _, want := range []string{"CapabilityBoundingSet=\n", "AmbientCapabilities=\n"} {
		if !strings.Contains(panelConfig, want) {
			t.Fatalf("control-plane unit veil.service missing empty %q", want)
		}
	}
	// The root helper needs file capabilities to read veil-owned state/staging and
	// manage root-owned generated configs and backups across ownership boundaries.
	helperBody, err := os.ReadFile("../../packaging/systemd/veil-helper.service")
	if err != nil {
		t.Fatal(err)
	}
	helperConfig := strings.ReplaceAll(string(helperBody), "\r\n", "\n")
	if !strings.Contains(helperConfig, "CapabilityBoundingSet=CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH CAP_CHOWN CAP_FOWNER") {
		t.Fatalf("veil-helper.service must grant the DAC/chown capabilities the helper needs:\n%s", helperConfig)
	}
	if !strings.Contains(helperConfig, "RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK") {
		t.Fatalf("veil-helper.service must allow Caddy Admin IPv4/IPv6 plus netlink:\n%s", helperConfig)
	}
	backupBody, err := os.ReadFile("../../packaging/systemd/veil-backup.service")
	if err != nil {
		t.Fatal(err)
	}
	backupConfig := strings.ReplaceAll(string(backupBody), "\r\n", "\n")
	if !strings.Contains(backupConfig, "CapabilityBoundingSet=CAP_DAC_OVERRIDE CAP_DAC_READ_SEARCH\n") {
		t.Fatalf("veil-backup.service must grant DAC capabilities to read veil-owned state:\n%s", backupConfig)
	}
	if strings.Contains(backupConfig, "CapabilityBoundingSet=\n") {
		t.Fatalf("veil-backup.service must not drop all capabilities:\n%s", backupConfig)
	}
}

// TestOpenAPISpecCoversCoreRoutes parses the OpenAPI document and verifies it
// documents the core management routes and the bearer/token auth schemes —
// parsed `paths:`/`securitySchemes:` keys, so the check cannot pass on a
// prose mention or a comment.
func TestOpenAPISpecCoversCoreRoutes(t *testing.T) {
	body, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		OpenAPI    string         `yaml:"openapi"`
		Paths      map[string]any `yaml:"paths"`
		Components map[string]any `yaml:"components"`
	}
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("openapi.yaml must parse as YAML: %v", err)
	}
	if doc.OpenAPI != "3.1.0" {
		t.Fatalf("openapi version = %q, want 3.1.0", doc.OpenAPI)
	}
	for _, path := range []string{
		"/api/auth/login",
		"/api/auth/status",
		"/api/status",
		"/api/settings",
		"/api/inbounds",
		"/api/apply",
		"/api/warp",
		"/api/routing/rules",
	} {
		if _, ok := doc.Paths[path]; !ok {
			t.Fatalf("openapi.yaml paths missing %q", path)
		}
	}
	schemes, _ := doc.Components["securitySchemes"].(map[string]any)
	for _, scheme := range []string{"bearerAuth", "sessionCookie", "csrfToken", "veilToken"} {
		if _, ok := schemes[scheme]; !ok {
			t.Fatalf("openapi.yaml securitySchemes missing %q", scheme)
		}
	}
	// The apiKey scheme must bind the documented header name.
	if veilToken, _ := schemes["veilToken"].(map[string]any); veilToken["name"] != "X-Veil-Token" {
		t.Fatalf("veilToken security scheme name = %v, want X-Veil-Token", veilToken["name"])
	}
	spec := strings.ReplaceAll(string(body), "\r\n", "\n")
	if !strings.Contains(spec, "--metrics-access") {
		t.Fatal("openapi.yaml missing --metrics-access policy documentation")
	}
	for _, dangerous := range []string{
		"the API is open at the application layer",
		"Non-API routes (the Panel UI, `/healthz`, `/metrics`) are not token-gated",
	} {
		if strings.Contains(spec, dangerous) {
			t.Fatalf("openapi.yaml still documents unsafe exposure model %q", dangerous)
		}
	}
}

// TestHardeningGuideExists ensures the hardening guide is present and covers
// the key operational topics as real sections with concrete procedures — not
// isolated token mentions that could appear anywhere in prose.
func TestHardeningGuideExists(t *testing.T) {
	body, err := os.ReadFile("../../docs/HARDENING.md")
	if err != nil {
		t.Fatal(err)
	}
	guide := strings.ReplaceAll(string(body), "\r\n", "\n")

	// Each operational topic must have a dedicated section heading.
	for _, heading := range []string{
		"## 2. API authentication",
		"## 4. Encrypted backups and key lifecycle",
		"## 5. Panel audit history",
		"## 6. Supply-chain integrity",
		"## 7. Host and runtime hardening",
		"## 8. Updates and rollback",
	} {
		if !strings.Contains(guide, heading) {
			t.Fatalf("HARDENING.md missing section heading %q", heading)
		}
	}

	// And the guide must ship the concrete verification/recovery commands an
	// operator runs — a section heading alone proves nothing.
	for _, procedure := range []string{
		"cosign verify-blob",
		"gh attestation verify",
		"sha256sum -c",
		"veil rollback restore",
	} {
		if !strings.Contains(guide, procedure) {
			t.Fatalf("HARDENING.md missing procedure %q", procedure)
		}
	}
}

func TestPrivilegeBoundaryDocumentation(t *testing.T) {
	documents := map[string][]string{
		"../../README.md": {
			"Privilege separation",
			"`veil` user",
			"`veil-helper.socket`",
		},
		"../../docs/HARDENING.md": {
			"`User=veil`",
			"`/run/veil/helper.sock`",
			"`SO_PEERCRED`",
			"`/var/lib/veil/migration-backups`",
		},
		"../../docs/install.md": {
			"`0640 root:veil`",
			"`0600 veil:veil`",
			"veil-helper.socket",
			"veil uninstall --yes --purge",
		},
		"../../docs/troubleshooting.md": {
			"systemctl status veil-helper.socket veil-helper.service",
			"journalctl -u veil-helper.service",
			"`/run/veil/helper.sock`",
		},
		"../../docs/operations.md": {
			"`ErrorEnvelope`",
			"`veil-helper.socket`",
			"application/json",
		},
		"../../docs/known-limitations.md": {
			"bare-metal privileged helper",
			"not a supported substitute",
		},
		"../../CONTEXT.md": {
			"Privileged helper",
			"`--keep-data`",
			"`--purge`",
		},
	}

	htmlComment := regexp.MustCompile(`(?s)<!--.*?-->`)
	for path, wants := range documents {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		// Strip HTML comments so a marker like <!-- TODO: document
		// SO_PEERCRED --> cannot satisfy the privilege-boundary requirement.
		content := htmlComment.ReplaceAllString(strings.ReplaceAll(string(body), "\r\n", "\n"), "")
		for _, want := range wants {
			if !strings.Contains(content, want) {
				t.Errorf("%s missing privilege-boundary documentation %q", path, want)
			}
		}
	}
}
