//go:build e2e

package e2e

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/renderer"
)

// TestRequiredWarpSingBoxConfig exercises the WARP/sing-box runtime listed in
// the upstream-compat drift table: it renders the exact sing-box configuration
// Veil writes to generated/sing-box/warp.json and asserts the installed
// sing-box binary accepts it. sing-box upstream breaks config schema between
// releases (WireGuard moved from outbounds to endpoints in 1.11, inline
// geoip/geosite route rules were removed in 1.12), so `check` on the rendered
// artifact is the compat signal this workflow exists for (#470).
func TestRequiredWarpSingBoxConfig(t *testing.T) {
	singbox := requiredBinary(t, "sing-box")

	cfg, err := renderer.RenderWarpSingBox(renderer.WarpSingBoxConfig{
		PrivateKey:    testWireGuardKey(t),
		PeerPublicKey: testWireGuardKey(t),
		LocalAddress:  "172.16.0.2/32",
		// Route rules without remote rule_sets: ip_is_private and domain atoms
		// validate offline (`check` must not need to fetch remote rule-sets).
		RoutingRules: []renderer.WarpRoutingRule{
			{Match: "geoip:private", Outbound: "direct"},
			{Match: "example.com", Outbound: "direct"},
		},
	})
	if err != nil {
		t.Fatalf("render warp sing-box config: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "warp.json")
	if err := os.WriteFile(configPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write warp config: %v", err)
	}

	cmd := exec.Command(singbox, "check", "-c", configPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sing-box check rejected the rendered warp config: %v\n%s", err, out)
	}
	t.Logf("sing-box check accepted rendered warp config: %s", out)
}

// testWireGuardKey returns a well-formed WireGuard key (32-byte base64) —
// schema-valid for `sing-box check` without being a real credential.
func testWireGuardKey(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("generate wireguard key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(buf)
}
