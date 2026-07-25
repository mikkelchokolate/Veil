package cli

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestReleaseWorkflowBuildsSignedPackagesAndSBOM locks in the supply-chain
// release gates: native deb/rpm/apk packages, an SBOM, and keyless signatures.
func TestReleaseWorkflowBuildsSignedPackagesAndSBOM(t *testing.T) {
	body, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{
		"Build native packages (deb/rpm/apk)",
		"packaging/nfpm.yaml",
		"dist/*.deb",
		"dist/*.rpm",
		"dist/*.apk",
		"Generate SBOM",
		"veil.sbom.spdx.json",
		"cosign",
		"sign-blob",
		"attest-build-provenance",
		"provenance: mode=max",
		"id-token: write",
		"attestations: write",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing supply-chain gate %q:\n%s", want, workflow)
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
// the Panel binary and the managed systemd units.
func TestNfpmConfigShipsBinaryAndUnits(t *testing.T) {
	body, err := os.ReadFile("../../packaging/nfpm.yaml")
	if err != nil {
		t.Fatal(err)
	}
	config := strings.ReplaceAll(string(body), "\r\n", "\n")
	for _, want := range []string{
		"name: veil",
		"dst: /usr/local/bin/veil",
		"veil.service",
		"veil-backup.service",
		"veil-backup.timer",
		"veil-caddy.service",
		"veil-hysteria2@.service",
		"veil-olcrtc@.service",
		"veil-mieru.service",
		"veil-warp.service",
		"postinstall: packaging/scripts/postinstall.sh",
		"preremove: packaging/scripts/preremove.sh",
		"postremove: packaging/scripts/postremove.sh",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("nfpm config missing %q:\n%s", want, config)
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
		if !strings.Contains(string(body), "systemctl") {
			t.Fatalf("packaging script %s does not handle systemd", script)
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
			"UMask=0077",
		} {
			if !strings.Contains(config, want) {
				t.Fatalf("systemd unit %s missing hardening directive %q:\n%s", unit, want, config)
			}
		}
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
			"`PrivilegedErrorEnvelope`",
			"`veil-helper.socket`",
			"`text/plain`",
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
