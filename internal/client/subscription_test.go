package client

import (
	"strings"
	"testing"
)

func TestSubscriptionRendererRendersLinksFromBindings(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cipher := newTestCipher(t)
	repo := NewRepository(db)
	cs := NewCredentialStore(db, cipher)
	r := NewSubscriptionRenderer(repo, cs)

	c, _ := repo.Create(Client{Name: "alice", Enabled: true, QuotaResetPolicy: ResetNever})
	b, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-hy2", Enabled: true})
	_, _ = cs.Set(b.ID, "password", "pw-alice")

	resolve := func(inboundID string) (InboundSnapshot, bool) {
		if inboundID == "in-hy2" {
			return InboundSnapshot{
				Name: "hy2-main", Protocol: "hysteria2", Transport: "udp",
				Port: 443, Enabled: true,
				ProtocolFields: map[string]any{"domain": "vpn.example.com"},
			}, true
		}
		return InboundSnapshot{}, false
	}
	links, err := r.LinksForClient(c, resolve)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d (%+v)", len(links), links)
	}
	if !strings.Contains(links[0].URI, "pw-alice") {
		t.Fatalf("link must carry the binding credential, got %q", links[0].URI)
	}
}

func TestSubscriptionRendererSkipsMissingCredentialAndInbound(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	cipher := newTestCipher(t)
	repo := NewRepository(db)
	cs := NewCredentialStore(db, cipher)
	r := NewSubscriptionRenderer(repo, cs)

	c, _ := repo.Create(Client{Name: "bob", Enabled: true, QuotaResetPolicy: ResetNever})
	// Binding with no credential -> skipped.
	_, _ = repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-hy2", Enabled: true})
	// Binding to unknown inbound -> skipped.
	b2, _ := repo.CreateBinding(Binding{ClientID: c.ID, InboundID: "in-missing", Enabled: true})
	_, _ = cs.Set(b2.ID, "password", "pw")

	resolve := func(inboundID string) (InboundSnapshot, bool) {
		return InboundSnapshot{}, false
	}
	links, err := r.LinksForClient(c, resolve)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(links) != 0 {
		t.Fatalf("expected no links for broken bindings, got %+v", links)
	}
}
