package api

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// TestRendererIncludesNormalizedClientCredentials asserts (A6a) that a client
// that exists ONLY as normalized Client+Binding+Credential (no legacy embedded
// profile) is rendered into the live hysteria2 config via the inbound's
// runtime credentials resolved from the client store.
func TestRendererIncludesNormalizedClientCredentials(t *testing.T) {
	s := &managementState{}
	s.settings = Settings{Domain: "x.example", PanelListen: "127.0.0.1:2096"}
	s.inbounds = []Inbound{{Name: "hy2", Protocol: "hysteria2", Transport: "udp", Port: 443, Enabled: true, Password: "inbound-pass"}}

	// Seed a normalized client bound to hy2 with a credential.
	repo := client.NewRepository(openApplyTestDB(t))
	s.cipher = newTestCipher(t)
	creds := client.NewCredentialStore(openApplyTestDB(t), s.cipher)
	// repo and creds must share the same DB.
	db := openApplyTestDB(t)
	repo = client.NewRepository(db)
	creds = client.NewCredentialStore(db, s.cipher)
	svc := client.NewService(repo, creds)
	s.clientService = svc

	view, err := svc.Create(client.Client{Name: "alice", Enabled: true})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	b, err := svc.AddBinding(view.ID, "hy2")
	if err != nil {
		t.Fatalf("add binding: %v", err)
	}
	if _, err := svc.SetCredential(b.ID, "password", "alice-secret-pass"); err != nil {
		t.Fatalf("set credential: %v", err)
	}

	// Render and confirm the normalized client's credential reaches the config.
	configs, err := s.renderManagementConfigsLocked()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	found := false
	for path, body := range configs {
		if strings.Contains(body, "alice") && strings.Contains(body, "alice-secret-pass") {
			found = true
			t.Logf("normalized client rendered into %s", path)
		}
	}
	if !found {
		t.Fatalf("normalized client credential NOT rendered into any config; A6a gap remains. configs: %v", keysOfStrMap(configs))
	}
}

func keysOfStrMap(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
