package hysteria2

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
)

func renderOneInbound(t *testing.T, inbound model.Inbound, paths generatedconfig.Paths) (body string, artifacts int) {
	t.Helper()
	got, ok, err := New().RenderConfig(generatedconfig.ProtocolRenderInput{
		Settings: model.Settings{Domain: "example.com", Hysteria2Password: "fallback-secret"},
		Paths:    paths,
		Inbounds: []model.Inbound{inbound},
	})
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	if !ok {
		return "", len(got)
	}
	if len(got) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(got))
	}
	return got[0].Body, len(got)
}

// #1095: every Hysteria2 config — even without a WARP upstream — must carry
// the deny-by-default ACL so proxy sessions cannot pivot to loopback control
// planes (Caddy admin 127.0.0.1:2019), private ranges or link-local metadata.
func TestRenderConfigEmitsEgressDenyACLWithoutWarp(t *testing.T) {
	body, _ := renderOneInbound(t, model.Inbound{
		Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true,
	}, generatedconfig.NewPaths(t.TempDir()))
	if body == "" {
		t.Fatal("no hysteria2 artifact rendered")
	}
	for _, want := range []string{
		"reject(0.0.0.0/8)",
		"reject(127.0.0.0/8)",
		"reject(169.254.0.0/16)",
		"reject(100.64.0.0/10)",
		"reject(::1/128)",
		"reject(::/128)",
		"reject(fc00::/7)",
		"reject(fe80::/10)",
		"direct(all)",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("hysteria2 config missing %q:\n%s", want, body)
		}
	}
	// The deny block must precede the final rule and no warp outbound may
	// appear without an upstream.
	if strings.Index(body, "reject(0.0.0.0/8)") >= strings.Index(body, "direct(all)") {
		t.Fatalf("deny block must precede final rule:\n%s", body)
	}
	if strings.Contains(body, "name: warp") {
		t.Fatalf("warp outbound rendered without upstream:\n%s", body)
	}
}

// #1098: an inbound that still has normalized bindings but zero usable
// credentials (disabled/expired/depleted) must fail closed — no artifact, so
// promotion removes the live YAML and stops the instance. Reviving the shared
// fallback password would resurrect revoked access.
func TestRenderConfigDropsClientManagedInboundWithNoUsableCredentials(t *testing.T) {
	inbound := model.Inbound{
		Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true,
		Password:          "fallback-secret",
		HasClientBindings: true,
	}
	body, n := renderOneInbound(t, inbound, generatedconfig.NewPaths(t.TempDir()))
	if n != 0 || body != "" {
		t.Fatalf("revoked inbound produced artifacts=%d body:\n%s", n, body)
	}
	// Same shape driven by all-disabled profiles (the pre-normalization
	// management surface) must drop identically.
	inbound.HasClientBindings = false
	inbound.Profiles = []model.ClientProfile{{Name: "alice", Username: "alice", Password: "alice-pass", Enabled: false}}
	body, n = renderOneInbound(t, inbound, generatedconfig.NewPaths(t.TempDir()))
	if n != 0 || body != "" {
		t.Fatalf("all-disabled-profiles inbound produced artifacts=%d body:\n%s", n, body)
	}
	// And an unmanaged inbound keeps the fallback — no regression there.
	inbound.Profiles = nil
	body, n = renderOneInbound(t, inbound, generatedconfig.NewPaths(t.TempDir()))
	if n != 1 || !strings.Contains(body, "fallback-secret") {
		t.Fatalf("unmanaged inbound lost its fallback credential:\n%s", body)
	}
}

// #1132: the staged apply flow writes rules/*.dat under <applyRoot>/generated
// BEFORE render, but promotion copies them to <liveRoot>/rules — the YAML must
// embed the live path, not the staging path the runtime unit cannot read.
func TestRenderConfigReferencesLiveRoutingDatPath(t *testing.T) {
	applyRoot := t.TempDir()
	liveRoot := t.TempDir()
	staged := filepath.Join(applyRoot, "generated", "rules")
	if err := os.MkdirAll(staged, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "geoip.dat"), []byte("geoip"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := generatedconfig.NewPathsWithLiveRoot(applyRoot, liveRoot)
	body, _ := renderOneInbound(t, model.Inbound{
		Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true,
	}, paths)
	if body == "" {
		t.Fatal("no hysteria2 artifact rendered")
	}
	liveDat := filepath.Join(liveRoot, "rules", "geoip.dat")
	stagedDat := filepath.Join(staged, "geoip.dat")
	if !strings.Contains(body, "geoip: "+liveDat) {
		t.Fatalf("geoip must reference the live path %q:\n%s", liveDat, body)
	}
	if strings.Contains(body, stagedDat) {
		t.Fatalf("geoip leaked the staging path %q:\n%s", stagedDat, body)
	}
	// geosite.dat was never staged or promoted — it must not be referenced.
	if strings.Contains(body, "geosite") {
		t.Fatalf("absent geosite.dat must not be referenced:\n%s", body)
	}
}

// routingDatPath regressions around the absent/live/staged matrix.
func TestRoutingDatPathPrefersLiveOverStagedAndOmitsAbsent(t *testing.T) {
	applyRoot := t.TempDir()
	liveRoot := t.TempDir()
	paths := generatedconfig.NewPathsWithLiveRoot(applyRoot, liveRoot)

	if got := routingDatPath(paths, "geoip.dat"); got != "" {
		t.Fatalf("absent dat resolved to %q, want empty", got)
	}
	liveDat := filepath.Join(liveRoot, "rules", "geoip.dat")
	stagedDat := filepath.Join(applyRoot, "generated", "rules", "geoip.dat")
	if err := os.MkdirAll(filepath.Dir(stagedDat), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedDat, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := routingDatPath(paths, "geoip.dat"); got != liveDat {
		t.Fatalf("staged dat resolved to %q, want live destination %q", got, liveDat)
	}
	// Without a live root (test/preview renders) the staging path stands.
	stagingOnly := generatedconfig.NewPaths(applyRoot)
	if got := routingDatPath(stagingOnly, "geoip.dat"); got != stagedDat {
		t.Fatalf("staging-only dat resolved to %q, want %q", got, stagedDat)
	}
}
