package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestReleaseWorkflowBuildsSignedPackagesAndSBOM locks in the supply-chain
// release gates: native deb/rpm/apk packages, an SBOM, and keyless signatures.
func TestReleaseWorkflowBuildsSignedPackagesAndSBOM(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := strings.ReplaceAll(string(body), "\r\n", "\n")
	// Every marker must live in the job that produces the artifact — a
	// step-name or comment elsewhere in the file is not evidence (#774).
	releaseJob := stripHashComments(t, workflowJobBlock(t, workflow, "release"))
	for _, want := range []string{
		"Build native packages (deb/rpm/apk)",
		"packaging/nfpm.yaml",
		"dist/*.deb",
		"dist/*.rpm",
		"dist/*.apk",
	} {
		if !strings.Contains(releaseJob, want) {
			t.Fatalf("release job missing supply-chain gate %q", want)
		}
	}
	dockerJob := stripHashComments(t, workflowJobBlock(t, workflow, "docker-publish"))
	for _, want := range []string{
		"cosign",
		"provenance: mode=max",
	} {
		if !strings.Contains(dockerJob, want) {
			t.Fatalf("docker-publish job missing supply-chain gate %q", want)
		}
	}
	publishJob := stripHashComments(t, workflowJobBlock(t, workflow, "publish"))
	for _, want := range []string{
		"Generate SBOM",
		"veil.sbom.spdx.json",
		"sign-blob",
		"attest-build-provenance",
	} {
		if !strings.Contains(publishJob, want) {
			t.Fatalf("publish job missing supply-chain gate %q", want)
		}
	}
	// OIDC permissions are top-level workflow keys, not job steps.
	header := stripHashComments(t, workflow[:strings.Index(workflow, "\njobs:")])
	for _, want := range []string{"id-token: write", "attestations: write"} {
		if !strings.Contains(header, want) {
			t.Fatalf("release workflow missing top-level permission %q", want)
		}
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

// TestPackageScriptsExist ensures the packaging maintainer scripts are present
// and reference systemd lifecycle handling.
func TestPackageScriptsExist(t *testing.T) {
	for _, script := range []string{
		"../../packaging/scripts/postinstall.sh",
		"../../packaging/scripts/preremove.sh",
		"../../packaging/scripts/postremove.sh",
	} {
		body, err := os.ReadFile(script)
		if err != nil {
			t.Fatalf("missing packaging script %s: %v", script, err)
		}
		// Comments stripped: a `# handles systemctl` note is not evidence.
		if !strings.Contains(stripHashComments(t, string(body)), "systemctl") {
			t.Fatalf("packaging script %s does not handle systemd", script)
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

// TestOpenAPISpecCoversCoreRoutes verifies the OpenAPI document exists and
// documents the core management routes and the bearer/token auth schemes.
func TestOpenAPISpecCoversCoreRoutes(t *testing.T) {
	body, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	spec := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{
		"openapi: 3.1.0",
		"/api/auth/login",
		"/api/auth/status",
		"sessionCookie",
		"csrfToken",
		"--metrics-access",
		"/api/status",
		"/api/settings",
		"/api/inbounds",
		"/api/apply",
		"/api/warp",
		"/api/routing/rules",
		"X-Veil-Token",
		"bearerAuth",
	} {
		if !strings.Contains(spec, want) {
			t.Fatalf("openapi.yaml missing %q", want)
		}
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
// the key operational topics.
func TestHardeningGuideExists(t *testing.T) {
	body, err := os.ReadFile("../../docs/HARDENING.md")
	if err != nil {
		t.Fatal(err)
	}
	guide := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{
		"bearer token",
		"checksum",
		"cosign",
		"SBOM",
		"systemd",
	} {
		if !strings.Contains(guide, want) {
			t.Fatalf("HARDENING.md missing %q", want)
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

	for path, wants := range documents {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		content := strings.ReplaceAll(string(body), "\r\n", "\n")
		for _, want := range wants {
			if !strings.Contains(content, want) {
				t.Errorf("%s missing privilege-boundary documentation %q", path, want)
			}
		}
	}
}
