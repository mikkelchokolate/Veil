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
	for _, want := range []string{"docker-build:", "Docker image build", "docker build --pull --tag veil:ci ."} {
		if !strings.Contains(ciWorkflow, want) {
			t.Fatalf("ci.yml missing Docker build verification %q", want)
		}
	}

	dependabot, err := os.ReadFile("../../.github/dependabot.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"gomod", "docker", "github-actions"} {
		if !strings.Contains(string(dependabot), want) {
			t.Fatalf("dependabot.yml missing ecosystem %q", want)
		}
	}
	dependabotConfig := strings.ReplaceAll(string(dependabot), "\r\n", "\n")
	for _, want := range []string{"go-modules:", "container-images:", "github-actions-updates:"} {
		if !strings.Contains(dependabotConfig, want) {
			t.Fatalf("dependabot.yml missing grouped updates policy %q", want)
		}
	}
	if got := strings.Count(dependabotConfig, "open-pull-requests-limit: 1"); got != 3 {
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
		"veil-caddy@.service",
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
	for _, unit := range []string{
		"../../packaging/systemd/veil.service",
		"../../packaging/systemd/veil-caddy@.service",
		"../../packaging/systemd/veil-hysteria2@.service",
		"../../packaging/systemd/veil-olcrtc@.service",
		"../../packaging/systemd/veil-mieru.service",
		"../../packaging/systemd/veil-warp.service",
	} {
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
			"CapabilityBoundingSet=CAP_NET_BIND_SERVICE",
			"AmbientCapabilities=CAP_NET_BIND_SERVICE",
			"RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
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
