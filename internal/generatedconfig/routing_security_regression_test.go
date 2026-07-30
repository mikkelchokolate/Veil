package generatedconfig

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
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

func TestRoutingSourceRejectsUnapprovedPublicHost(t *testing.T) {
	if _, err := DownloadRouteDat("http://127.0.0.1/data"); err == nil {
		t.Fatal("exported downloader accepted non-HTTPS/private source")
	}
	file := RoutingSourceFile{Name: "geoip.dat", URL: "https://unapproved.invalid/geoip.dat", SHA256URL: "https://unapproved.invalid/geoip.dat.sha256sum", PinnedSHA256: strings.Repeat("0", 64)}
	material := NewRoutingSourceMaterial(t.TempDir(), RoutingSource{Files: []RoutingSourceFile{file}}).WithDownloader(func(string) ([]byte, error) { return []byte("unused"), nil })
	if _, err := material.Fetch(file); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected host allowlist rejection, got %v", err)
	}
}

func TestRouteDatRetryBackoffHonorsCancellation(t *testing.T) {
	old := routeDatHTTPClient
	defer func() { routeDatHTTPClient = old }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	routeDatHTTPClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		cancel()
		return &http.Response{StatusCode: http.StatusServiceUnavailable, Status: "503 Service Unavailable", Body: io.NopCloser(strings.NewReader("retry")), Header: make(http.Header)}, nil
	})}
	start := time.Now()
	_, err := downloadRouteDatContext(ctx, "http://example.test/data")
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("cancellation was not prompt: err=%v elapsed=%v", err, time.Since(start))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
