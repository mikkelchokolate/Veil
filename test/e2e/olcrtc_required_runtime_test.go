//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// TestRequiredOlcRTCRuntimeContract closes the fourth-protocol gap in the
// required real-runtime suite. It exercises the panel's public API and apply
// pipeline, verifies that the generated server config and exported compact URI
// carry the same provider/transport/room/key, then feeds the generated YAML to
// the exact olcRTC binary provisioned by scripts/ci/e2e.sh. The room points at
// an intentionally unavailable loopback Jitsi endpoint: the runtime may fail
// later while establishing signaling, but it must never reject Veil's YAML at
// load/validation time.
func TestRequiredOlcRTCRuntimeContract(t *testing.T) {
	olcrtcPath := requiredBinary(t, "olcrtc")
	requireRealOlcRTCBinary(t, olcrtcPath)
	key := strings.Repeat("ab", 32)
	room := fmt.Sprintf("https://127.0.0.1:%d/veil-required-e2e", freePort(t))

	srv := startServer(t, serverOptions{token: "e2e-secret-token"})
	resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("settings expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)

	inboundPort := freePort(t)
	body := fmt.Sprintf(`{"name":"olcrtc-required","protocol":"olcrtc","transport":"udp","port":%d,"enabled":true,"protocolFields":{"password":%q,"olcrtcAuth":"jitsi","olcrtcTransport":"datachannel","olcrtcRoomID":%q}}`, inboundPort, key, room)
	resp = srv.do(http.MethodPost, "/api/inbounds", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("inbound expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	drain(resp)
	applyPanelConfiguration(t, srv)

	configPath := filepath.Join(srv.applyRoot, "generated", "olcrtc", "olcrtc-required.yaml")
	configBody, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read generated olcRTC config: %v", err)
	}
	var rendered struct {
		Mode string `yaml:"mode"`
		Auth struct {
			Provider string `yaml:"provider"`
		} `yaml:"auth"`
		Room struct {
			ID string `yaml:"id"`
		} `yaml:"room"`
		Crypto struct {
			Key string `yaml:"key"`
		} `yaml:"crypto"`
		Net struct {
			Transport string `yaml:"transport"`
		} `yaml:"net"`
	}
	if err := yaml.Unmarshal(configBody, &rendered); err != nil {
		t.Fatalf("decode panel-generated olcRTC YAML: %v\n%s", err, configBody)
	}
	if rendered.Mode != "srv" || rendered.Auth.Provider != "jitsi" || rendered.Net.Transport != "datachannel" || rendered.Room.ID != room || rendered.Crypto.Key != key {
		t.Fatalf("panel-generated olcRTC contract mismatch: %+v", rendered)
	}

	resp = srv.do(http.MethodGet, "/api/client-links", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("client links expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
	}
	linksBody := readJSON(t, resp)
	linksRaw, _ := json.Marshal(linksBody["links"])
	var links []struct {
		Protocol string `json:"protocol"`
		URI      string `json:"uri"`
	}
	if err := json.Unmarshal(linksRaw, &links); err != nil {
		t.Fatalf("decode olcRTC links: %v", err)
	}
	wantURI := fmt.Sprintf("olcrtc://jitsi?datachannel@%s#%s$", room, key)
	gotURI := ""
	for _, link := range links {
		if link.Protocol == "olcrtc" {
			gotURI = link.URI
			break
		}
	}
	if gotURI != wantURI {
		t.Fatalf("olcRTC exported URI = %q, want %q", gotURI, wantURI)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, olcrtcPath, configPath)
	var runtimeLog bytes.Buffer
	cmd.Stdout = &runtimeLog
	cmd.Stderr = &runtimeLog
	err = cmd.Run()
	log := runtimeLog.String()
	lowerLog := strings.ToLower(log)
	for _, rejection := range []string{"load config:", "validate config:"} {
		if strings.Contains(lowerLog, rejection) {
			t.Fatalf("real pinned olcRTC rejected panel-generated config: %v\n%s", err, log)
		}
	}
	// A still-running process is killed by the context after successfully
	// entering runtime setup; an early failure is acceptable only after config
	// loading/validation, because the loopback signaling endpoint is deliberately
	// unavailable and external-provider reachability is not a deterministic CI
	// dependency.
	if ctx.Err() != nil {
		return
	}
	if err == nil {
		t.Fatal("olcRTC exited cleanly even though the required server session should remain active")
	}
	// Early exit must carry positive evidence that the process consumed the
	// generated config and reached the signaling stage — otherwise a stub, a
	// flag-parse failure, or any unrelated crash would satisfy the contract.
	// The unique room path is mandatory: only it proves THIS generated config
	// was loaded; generic networking words can appear in unrelated failures.
	if strings.TrimSpace(log) == "" {
		t.Fatalf("olcRTC exited early (%v) with no output — cannot prove it loaded the panel config", err)
	}
	if !strings.Contains(log, "veil-required-e2e") {
		t.Fatalf("olcRTC exited early (%v) without echoing the unique room marker — cannot prove it consumed the generated config:\n%s", err, log)
	}
	// Corroboration must add evidence BEYOND the room marker: signaling-stage
	// vocabulary proves the process progressed past config load toward the
	// (deliberately unreachable) Jitsi endpoint. Room-URL substrings
	// ("veil-required-e2e", "127.0.0.1") are excluded — they are already
	// implied by the mandatory check above and would make this loop a no-op.
	signalingEvidence := []string{
		"signal", "signaling", "websocket", "dial", "connect",
		"refused", "handshake", "tls", "jitsi",
	}
	proven := false
	for _, marker := range signalingEvidence {
		if strings.Contains(lowerLog, marker) {
			proven = true
			break
		}
	}
	if !proven {
		t.Fatalf("olcRTC exited early (%v) without evidence of reaching signaling setup — not a config-acceptance signal:\n%s", err, log)
	}
}

// requireRealOlcRTCBinary proves the binary under test is the pinned upstream
// olcRTC build — not a stub script, not a symlink to veil, not another daemon.
// scripts/ci/e2e.sh resolves the module version the same way.
func requireRealOlcRTCBinary(t *testing.T, path string) {
	t.Helper()
	out, err := exec.Command("go", "version", "-m", path).CombinedOutput()
	if err != nil {
		t.Fatalf("olcRTC binary %s is not a Go module binary (stub or symlink?): %v\n%s", path, err, out)
	}
	if !strings.Contains(string(out), "github.com/openlibrecommunity/olcrtc") {
		t.Fatalf("olcRTC binary %s does not carry the openlibrecommunity/olcrtc module identity:\n%s", path, out)
	}
}
