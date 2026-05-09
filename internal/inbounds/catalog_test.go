package inbounds

import "testing"

func TestCatalogCreatesInboundWithGeneratedCredentials(t *testing.T) {
	catalog := NewCatalogWithPasswordGenerator(nil, func() string { return "generated" })
	created, next, err := catalog.Create(Inbound{Name: "mieru", Protocol: "mieru", Transport: "tcp", Port: 443, Enabled: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Password != "generated" {
		t.Fatalf("password = %q", created.Password)
	}
	if got, ok := next.Get("mieru"); !ok || got.Password != "generated" {
		t.Fatalf("stored inbound = %+v ok=%v", got, ok)
	}
}
