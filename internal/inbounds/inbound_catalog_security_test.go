package inbounds

import "testing"

func TestInboundCatalogRejectsUpdateOfUnsafeCanonicalName(t *testing.T) {
	catalog := NewInboundCatalog([]Inbound{{
		Name:      "../legacy",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      8443,
		Enabled:   true,
	}})

	_, _, err := catalog.Update("../legacy", Inbound{
		Name:      "safe-payload-name",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      9443,
		Enabled:   true,
	})
	if err != ErrInboundInvalid {
		t.Fatalf("Update error = %v, want ErrInboundInvalid", err)
	}
}

func TestInboundCatalogUsesCanonicalNameInsteadOfPayloadRename(t *testing.T) {
	catalog := NewInboundCatalog([]Inbound{{
		Name:      "stable-name",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      8443,
		Enabled:   true,
	}})

	updated, next, err := catalog.Update("stable-name", Inbound{
		Name:      "attempted-rename",
		Protocol:  "hysteria2",
		Transport: "udp",
		Port:      9443,
		Enabled:   true,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "stable-name" {
		t.Fatalf("updated name = %q, want stable-name", updated.Name)
	}
	if _, ok := next.Get("attempted-rename"); ok {
		t.Fatal("payload name unexpectedly renamed the inbound")
	}
}
