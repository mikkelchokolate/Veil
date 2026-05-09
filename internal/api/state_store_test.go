package api

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/veil-panel/veil/internal/secrets"
)

func TestStateStoreRejectsRemovedStackField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{
		"settings":{"panelListen":"127.0.0.1:2096","mode":"dev","stack":"both"},
		"inbounds":[],
		"routingRules":[],
		"warp":{"enabled":false,"endpoint":"engage.cloudflareclient.com:2408"}
	}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	_, _, err := NewStateStore(path, nil).Load()
	if err == nil || !strings.Contains(err.Error(), `json: unknown field "stack"`) {
		t.Fatalf("expected removed stack field rejection, got %v", err)
	}
}

func TestStateStoreEncryptsAndDecryptsInboundPasswords(t *testing.T) {
	var key [secrets.KeySize]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	cipher, err := secrets.NewCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	path := filepath.Join(t.TempDir(), "state.json")
	store := NewStateStore(path, cipher)

	snapshot := managementSnapshot{
		Settings: Settings{PanelListen: "127.0.0.1:2096", Mode: "dev", Domain: "vpn.example.com"},
		Inbounds: []Inbound{{Name: "hy2-vip", Protocol: "hysteria2", Transport: "udp", Port: 8443, Enabled: true, Password: "vip-secret", Profiles: []ClientProfile{{Name: "alice", Username: "alice", Password: "alice-secret", Enabled: true}}}},
	}
	if err := store.Save(snapshot); err != nil {
		t.Fatalf("save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw state: %v", err)
	}
	if strings.Contains(string(raw), "vip-secret") || strings.Contains(string(raw), "alice-secret") {
		t.Fatalf("raw state leaked inbound/client profile password: %s", string(raw))
	}

	loaded, ok, err := store.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !ok {
		t.Fatalf("expected state to exist")
	}
	if got := loaded.Inbounds[0].Password; got != "vip-secret" {
		t.Fatalf("decrypted inbound password = %q", got)
	}
	if got := loaded.Inbounds[0].Profiles[0].Password; got != "alice-secret" {
		t.Fatalf("decrypted client profile password = %q", got)
	}
}
