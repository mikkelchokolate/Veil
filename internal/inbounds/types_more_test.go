package inbounds

import "testing"

func TestGenerateInboundPassword(t *testing.T) {
	p := generateInboundPassword()
	if p == "" {
		t.Fatal("expected generated password")
	}
	if len(p) < 8 {
		t.Fatalf("password too short: %q", p)
	}
}

func TestNewCatalogUsesDefaultGenerator(t *testing.T) {
	catalog := NewCatalog(nil)
	created, _, err := catalog.Create(Inbound{
		Name:      "mieru",
		Protocol:  "mieru",
		Transport: "tcp",
		Port:      443,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Password == "" {
		t.Fatal("expected generated password")
	}
}
