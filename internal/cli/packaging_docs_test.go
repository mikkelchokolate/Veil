package cli

import (
	"os"
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
		"id-token: write",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing supply-chain gate %q:\n%s", want, workflow)
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
