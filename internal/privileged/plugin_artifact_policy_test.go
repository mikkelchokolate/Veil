package privileged

import "testing"

func TestManagedArtifactPathRejectsAggregateOnlyProtocolSidecars(t *testing.T) {
	policy := Policy{}
	if _, ok := policy.managedArtifactPath("mieru/sidecar.json"); ok {
		t.Fatal("aggregate-only Mieru sidecar should not be treated as a managed generated artifact")
	}
}

func TestManagedArtifactPathUsesProtocolArtifactSpecs(t *testing.T) {
	policy := Policy{}
	for _, id := range []string{
		"caddy/edge.json",
		"hysteria2/udp-edge.yaml",
		"olcrtc/rtc-edge.yaml",
		"mieru/server_config.json",
		"sing-box/warp.json",
	} {
		if _, ok := policy.managedArtifactPath(id); !ok {
			t.Fatalf("managedArtifactPath(%q) rejected managed artifact", id)
		}
	}
}

func TestManagedArtifactPathRestrictsUnknownDynamicNames(t *testing.T) {
	allowed := map[string]struct{}{"edge": {}}
	policy := Policy{AllowedArtifactNames: allowed}

	if _, ok := policy.managedArtifactPath("caddy/edge.json"); !ok {
		t.Fatal("expected allowed dynamic name to be accepted")
	}
	if _, ok := policy.managedArtifactPath("hysteria2/edge.yaml"); !ok {
		t.Fatal("expected allowed dynamic name to be accepted for hysteria2")
	}
	if _, ok := policy.managedArtifactPath("caddy/other.json"); ok {
		t.Fatal("expected unknown dynamic name to be rejected")
	}
	if _, ok := policy.managedArtifactPath("hysteria2/other.yaml"); ok {
		t.Fatal("expected unknown dynamic name to be rejected for hysteria2")
	}
	// Aggregate-only static artifacts are always evaluated without allow-list.
	if _, ok := policy.managedArtifactPath("mieru/server_config.json"); !ok {
		t.Fatal("expected aggregate artifact to be accepted regardless of allow-list")
	}
}

func TestManagedArtifactPathAllowsAllDynamicNamesWhenUnrestricted(t *testing.T) {
	policy := Policy{}
	if _, ok := policy.managedArtifactPath("caddy/edge.json"); !ok {
		t.Fatal("expected dynamic name to be accepted when no allow-list is set")
	}
	if _, ok := policy.managedArtifactPath("hysteria2/udp-edge.yaml"); !ok {
		t.Fatal("expected dynamic name to be accepted when no allow-list is set")
	}
}
