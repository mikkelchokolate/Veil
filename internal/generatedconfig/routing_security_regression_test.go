package generatedconfig

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestRoutingSourceMaterialRequiresHTTPSChecksumAndSafeName(t *testing.T) {
	validURL := "https://example.test/geoip.dat"
	validChecksum := validURL + ".sha256sum"
	for _, file := range []RoutingSourceFile{
		{Name: "geoip.dat", URL: validURL},
		{Name: "geoip.dat", URL: "http://example.test/geoip.dat", SHA256URL: validChecksum},
		{Name: "geoip.dat", URL: validURL, SHA256URL: "http://example.test/checksum"},
	} {
		material := NewRoutingSourceMaterial(t.TempDir(), RoutingSource{Files: []RoutingSourceFile{file}}).WithDownloader(func(string) ([]byte, error) { return []byte("unused"), nil })
		if _, err := material.Fetch(file); err == nil {
			t.Fatalf("accepted unsafe routing source: %+v", file)
		}
	}
	material := NewRoutingSourceMaterial(t.TempDir(), RoutingSource{Files: []RoutingSourceFile{{Name: "../geoip.dat", URL: validURL, SHA256URL: validChecksum}}}).WithDownloader(func(string) ([]byte, error) { return nil, nil })
	if _, err := material.WriteGenerated(); err == nil {
		t.Fatal("accepted path-traversal routing filename")
	}
}

func TestSecureRouteDatDialerRejectsLoopback(t *testing.T) {
	client := newSecureRouteDatHTTPClient()
	_, err := client.Transport.(*http.Transport).DialContext(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", "443"))
	if err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("loopback dial error=%v", err)
	}
}
