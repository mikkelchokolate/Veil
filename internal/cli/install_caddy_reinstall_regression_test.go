package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

func TestRetainCaddyJSONOnReinstallKeepsNaiveRoutes(t *testing.T) {
	etc := t.TempDir()
	livePath := filepath.Join(etc, "generated", "caddy", "config.json")
	if err := os.MkdirAll(filepath.Dir(livePath), 0o755); err != nil {
		t.Fatal(err)
	}
	live := `{"marker":"naive-live","apps":{"http":{"servers":{"naive":{"listen":[":443"],"routes":[{"match":[{"host":["naive.example.com"]}]}]}}}}}`
	if err := os.WriteFile(livePath, []byte(live), 0o600); err != nil {
		t.Fatal(err)
	}
	profile := installer.RURecommendedProfile{
		InstallPanelCaddy: true,
		CaddyJSON:         `{"apps":{"http":{"servers":{"panel":{"listen":[":443"]}}}}}`,
		Domain:            "panel.example.com",
		Email:             "admin@example.com",
		WebBasePath:       "/fresh/",
		PanelListen:       "127.0.0.1:2096",
	}
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{
			PanelAccess:       "caddy",
			Domain:            "panel.example.com",
			Email:             "admin@example.com",
			PanelDomain:       "panel.example.com",
			PanelEmail:        "admin@example.com",
			WebBasePath:       "/existing/",
			PanelListen:       "127.0.0.1:2096",
			PanelPublicPort:   443,
			AcmeChallengeMode: "tls-alpn-01",
		},
		Inbounds: []model.Inbound{{
			Name:     "naive-edge",
			Protocol: "naiveproxy",
			Enabled:  true,
			Profiles: []model.ClientProfile{{Name: "default", Username: "naive-user", Password: "naive-pass", Enabled: true}},
			ProtocolFields: map[string]any{
				"domain":     "naive.example.com",
				"transport":  "tcp",
				"publicPort": 443,
				"email":      "admin@example.com",
			},
		}},
	}

	got := retainCaddyJSONOnReinstall(profile, snapshot, etc)
	if !strings.Contains(got, "naive.example.com") {
		t.Fatalf("retained Caddy JSON missing naive inbound host:\n%s", got)
	}
	if got == profile.CaddyJSON {
		t.Fatalf("reinstall must not keep using the first-install panel-only Caddy JSON:\n%s", got)
	}
}

// Reinstall switching direct → caddy must render the NEW profile's panel
// server, not the stale direct-mode snapshot settings: PanelAccess=direct
// produces no panel server at all, so caddy starts, logs "serving initial
// configuration", and binds nothing on :443 (audit #304 caddy leg — the
// user-facing install reported success for an unreachable URL).
func TestRetainCaddyJSONOnReinstallDirectToCaddySwitch(t *testing.T) {
	etc := t.TempDir()
	profile := installer.RURecommendedProfile{
		InstallPanelCaddy: true,
		CaddyJSON:         `{"apps":{"http":{"servers":{"panel":{"listen":[":443"]}}}}}`,
		Domain:            "veil-ci.test",
		Email:             "ci@veil-ci.test",
		WebBasePath:       "/panel/",
		PanelListen:       "127.0.0.1:36228",
	}
	snapshot := model.ManagementSnapshot{
		Settings: model.Settings{
			PanelAccess:     "direct",
			Domain:          "127.0.0.1",
			PanelListen:     "0.0.0.0:2096",
			PanelPublicPort: 2096,
			WebBasePath:     "/panel/",
		},
		Inbounds: []model.Inbound{{
			Name:     "ci-hy2",
			Protocol: "hysteria2",
			Enabled:  true,
			Profiles: []model.ClientProfile{{Name: "default", Username: "u", Password: "p", Enabled: true}},
			ProtocolFields: map[string]any{
				"domain":     "hy2.example.com",
				"transport":  "udp",
				"publicPort": 34443,
			},
		}},
	}

	got := retainCaddyJSONOnReinstall(profile, snapshot, etc)
	if !strings.Contains(got, "veil-ci.test") {
		t.Fatalf("retained Caddy JSON missing the new panel domain:\n%s", got)
	}
	if !strings.Contains(got, `":443"`) {
		t.Fatalf("retained Caddy JSON has no :443 listener — caddy would serve nothing:\n%s", got)
	}
	if !strings.Contains(got, "127.0.0.1:36228") {
		t.Fatalf("retained Caddy JSON must proxy to the new panel listen address:\n%s", got)
	}
}

func TestApplyRURecommendedInstallDoesNotOverwriteLiveCaddyInbounds(t *testing.T) {
	withMockedInstallRuntimes(t)

	tempEtc := t.TempDir()
	tempVar := t.TempDir()
	tempSystemd := t.TempDir()
	livePath := filepath.Join(tempEtc, "generated", "caddy", "config.json")
	if err := os.MkdirAll(filepath.Dir(livePath), 0o755); err != nil {
		t.Fatal(err)
	}
	liveJSON := `{
  "apps": {
    "http": {
      "servers": {
        "panel": {"listen": [":443"], "routes": [{"match": [{"path": ["/existing-path/*"]}]}]},
        "naive": {"listen": [":443"], "routes": [{"match": [{"host": ["naive.example.com"]}], "handle": [{"handler": "forward_proxy", "auth_user": "naive-user"}]}]}
      }
    }
  }
}`
	if err := os.WriteFile(livePath, []byte(liveJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	resolvedKeyPath := filepath.Join(tempEtc, "state.key")
	resolvedStatePath := filepath.Join(tempVar, "state.json")
	key, err := secrets.LoadOrCreateKey(resolvedKeyPath)
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		t.Fatalf("create cipher: %v", err)
	}
	existing := model.ManagementSnapshot{
		Settings: model.Settings{
			PanelAccess:       "caddy",
			PanelListen:       "127.0.0.1:2096",
			WebBasePath:       "/existing-path/",
			Domain:            "panel.example.com",
			Email:             "admin@example.com",
			PanelDomain:       "panel.example.com",
			PanelEmail:        "admin@example.com",
			PanelPublicPort:   443,
			AcmeChallengeMode: "tls-alpn-01",
		},
		Users: []model.User{{Username: "existing_admin", PasswordHash: "hash", Role: "admin"}},
		Inbounds: []model.Inbound{{
			Name:     "naive-edge",
			Protocol: "naiveproxy",
			Enabled:  true,
			Profiles: []model.ClientProfile{{Name: "default", Username: "naive-user", Password: "naive-pass", Enabled: true}},
			ProtocolFields: map[string]any{
				"domain":     "naive.example.com",
				"transport":  "tcp",
				"publicPort": 443,
				"email":      "admin@example.com",
			},
		}},
	}
	if err := managementstate.NewStore(resolvedStatePath, cipher).Save(existing); err != nil {
		t.Fatalf("save existing state: %v", err)
	}

	cmd := NewRootCommand("test")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	profile := installer.RURecommendedProfile{
		Domain:            "panel.example.com",
		Email:             "admin@example.com",
		Username:          "fresh",
		Password:          "fresh-pass",
		WebBasePath:       "/fresh-path/",
		PanelAccess:       "caddy",
		PanelListen:       "127.0.0.1:2096",
		InstallPanelCaddy: true,
		CaddyJSON:         `{"apps":{"http":{"servers":{"panel":{"listen":[":443"],"routes":[{"match":[{"path":["/fresh-path/*"]}]}]}}}}}`,
	}
	if err := applyRURecommendedInstall(cmd, profile, ruRecommendedInstallOptions{
		EtcDir:      tempEtc,
		VarDir:      tempVar,
		SystemdDir:  tempSystemd,
		PanelAccess: "caddy",
	}); err != nil {
		t.Fatalf("applyRURecommendedInstall: %v\n%s", err, out.String())
	}

	written, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatalf("read live Caddy JSON: %v", err)
	}
	if !strings.Contains(string(written), "naive.example.com") {
		t.Fatalf("reinstall overwrote live Caddy JSON without retained NaiveProxy routes:\n%s", written)
	}

	snapshot, ok, err := managementstate.NewStore(resolvedStatePath, cipher).Load()
	if err != nil || !ok {
		t.Fatalf("reload state: ok=%v err=%v", ok, err)
	}
	if len(snapshot.Inbounds) != 1 || snapshot.Inbounds[0].Name != "naive-edge" {
		t.Fatalf("reinstall must retain the NaiveProxy inbound, got %+v", snapshot.Inbounds)
	}
}
