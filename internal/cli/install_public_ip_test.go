package cli

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveInstallPublicIPParsesExplicitAddress(t *testing.T) {
	ip, err := resolveInstallPublicIP(context.Background(), "93.184.216.34")
	if err != nil {
		t.Fatalf("resolveInstallPublicIP: %v", err)
	}
	if !ip.Equal(net.ParseIP("93.184.216.34")) {
		t.Fatalf("unexpected IP: %v", ip)
	}
}

func TestResolveInstallPublicIPDetectsAutoAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("93.184.216.34\n"))
	}))
	defer server.Close()

	oldClient := installPublicIPClient
	oldEndpoints := installPublicIPEndpoints
	installPublicIPClient = server.Client()
	installPublicIPEndpoints = []string{server.URL}
	t.Cleanup(func() {
		installPublicIPClient = oldClient
		installPublicIPEndpoints = oldEndpoints
	})

	ip, err := resolveInstallPublicIP(context.Background(), "auto")
	if err != nil {
		t.Fatalf("resolveInstallPublicIP: %v", err)
	}
	if !ip.Equal(net.ParseIP("93.184.216.34")) {
		t.Fatalf("unexpected IP: %v", ip)
	}
}
