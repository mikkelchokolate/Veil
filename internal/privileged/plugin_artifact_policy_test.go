package privileged

import "testing"

func TestManagedArtifactPathRejectsAggregateOnlyProtocolSidecars(t *testing.T) {
	if _, ok := managedArtifactPath("mieru/sidecar.json"); ok {
		t.Fatal("aggregate-only Mieru sidecar should not be treated as a managed generated artifact")
	}
}

func TestManagedArtifactPathUsesProtocolArtifactSpecs(t *testing.T) {
	for _, id := range []string{
		"caddy/edge.Caddyfile",
		"hysteria2/udp-edge.yaml",
		"olcrtc/rtc-edge.yaml",
		"mieru/server_config.json",
		"sing-box/warp.json",
	} {
		if _, ok := managedArtifactPath(id); !ok {
			t.Fatalf("managedArtifactPath(%q) rejected managed artifact", id)
		}
	}
}
