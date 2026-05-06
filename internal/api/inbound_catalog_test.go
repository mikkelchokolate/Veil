package api

import "testing"

func TestInboundCatalogCreatesInboundWithGeneratedPassword(t *testing.T) {
	catalog := NewInboundCatalogWithPasswordGenerator(nil, func() string { return "generated-pass" })

	created, next, err := catalog.Create(Inbound{
		Name:      "hy2-vip",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      8443,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Password != "generated-pass" {
		t.Fatalf("generated password = %q", created.Password)
	}
	got, ok := next.Get("hy2-vip")
	if !ok {
		t.Fatalf("created inbound not found")
	}
	if got.Password != "generated-pass" {
		t.Fatalf("stored generated password = %q", got.Password)
	}
}

func TestInboundCatalogUpdatePreservesPasswordWhenEmpty(t *testing.T) {
	catalog := NewInboundCatalogWithPasswordGenerator([]Inbound{{
		Name:      "hy2-vip",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      8443,
		Enabled:   true,
		Password:  "existing-pass",
	}}, func() string { return "generated-pass" })

	updated, _, err := catalog.Update("hy2-vip", Inbound{
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      9443,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Password != "existing-pass" {
		t.Fatalf("password should be preserved, got %q", updated.Password)
	}
}

func TestInboundCatalogRejectsDuplicateTransportPort(t *testing.T) {
	catalog := NewInboundCatalogWithPasswordGenerator([]Inbound{{
		Name:      "naive",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		Enabled:   true,
	}}, func() string { return "generated-pass" })

	_, _, err := catalog.Create(Inbound{
		Name:      "naive-alt",
		Protocol:  "naiveproxy",
		Transport: "tcp",
		Port:      443,
		Enabled:   true,
	})
	if err != ErrInboundDuplicateTransportPort {
		t.Fatalf("expected duplicate transport/port error, got %v", err)
	}
}
