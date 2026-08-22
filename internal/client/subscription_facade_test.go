package client

import (
	"strings"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
)

// TestSubscriptionRendererMatchesLegacyFacade verifies the per-client
// subscription renderer produces links consistent with the clientaccess
// registry facade for each of the 4 protocols. This is the contract that lets
// legacy BuildClientLinks delegate to the same rendering path.
func TestSubscriptionRendererMatchesLegacyFacade(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cipher := newTestCipher(t)
	repo := NewRepository(db)
	creds := NewCredentialStore(db, cipher)
	r := NewSubscriptionRenderer(repo, creds).WithSettings(clientaccess.Settings{Domain: "vpn.example.com"})

	protocols := []struct {
		name   string
		proto  string
		snapID string
		snap   InboundSnapshot
	}{
		{"hy2", "hysteria2", "i1", InboundSnapshot{Name: "i1", Protocol: "hysteria2", Transport: "udp", Enabled: true, Port: 443, ProtocolFields: map[string]any{"domain": "vpn.example.com"}}},
		{"naive", "naiveproxy", "i2", InboundSnapshot{Name: "i2", Protocol: "naiveproxy", Transport: "tcp", Enabled: true, Port: 8443, ProtocolFields: map[string]any{"domain": "vpn.example.com"}}},
		{"olcrtc", "olcrtc", "i3", InboundSnapshot{Name: "i3", Protocol: "olcrtc", Transport: "udp", Enabled: true, Port: 9000, ProtocolFields: map[string]any{"domain": "vpn.example.com", "olcrtcAuth": "a", "olcrtcTransport": "udp", "olcrtcRoomID": "r1"}}},
		{"mieru", "mieru", "i4", InboundSnapshot{Name: "i4", Protocol: "mieru", Transport: "tcp", Enabled: true, Port: 2999, ProtocolFields: map[string]any{"domain": "vpn.example.com"}}},
	}
	registry := clientaccess.NewClientAccessProtocolRegistry()

	for _, p := range protocols {
		t.Run(p.name, func(t *testing.T) {
			c, _ := repo.Create(Client{Name: "u-" + p.name, Enabled: true, QuotaResetPolicy: ResetNever})
			runtimeIdentity := "runtime_" + p.name
			b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: p.snapID, RuntimeIdentity: runtimeIdentity, Enabled: true})
			if _, err := creds.Set(b.ID, "password", "pw-"+p.name); err != nil {
				t.Fatalf("set cred: %v", err)
			}

			links, err := r.LinksForClient(c, func(id string) (InboundSnapshot, bool) {
				if id == p.snapID {
					return p.snap, true
				}
				return InboundSnapshot{}, false
			})
			if err != nil {
				t.Fatalf("render: %v", err)
			}
			joined := ""
			for _, l := range links {
				joined += l.URI + "\n" + l.Config + "\n"
			}
			// Per-client semantics differ by protocol:
			//  - hysteria2/naiveproxy embed the per-client password.
			//  - olcRTC/Mieru use the binding's immutable runtime identity,
			//    never the mutable display name.
			switch p.proto {
			case "hysteria2", "naiveproxy":
				if !strings.Contains(joined, "pw-"+p.name) {
					t.Fatalf("%s link must carry per-client credential, got %q", p.proto, joined)
				}
			case "olcrtc", "mieru":
				if !strings.Contains(joined, runtimeIdentity) {
					t.Fatalf("%s link must carry immutable runtime identity %q, got %q", p.proto, runtimeIdentity, joined)
				}
			}
			// Registry must advertise per-client capability for this protocol.
			if cap, ok := registry.Capability(p.proto); !ok || !cap.SupportsPerClientCredentials {
				t.Fatalf("registry must support per-client creds for %s", p.proto)
			}
		})
	}
}
