package hostenv

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolvePublicIPEmptyValueReturnsNil(t *testing.T) {
	ip, err := ResolvePublicIP(context.Background(), "", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ip != nil {
		t.Fatalf("expected nil IP for empty value, got %v", ip)
	}
}

func TestResolvePublicIPRejectsInvalidExplicitIP(t *testing.T) {
	_, err := ResolvePublicIP(context.Background(), "not-an-ip", nil, nil)
	if err == nil {
		t.Fatalf("expected error for invalid explicit IP")
	}
	if !strings.Contains(err.Error(), "public IP must be a valid IPv4 or IPv6 address") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolvePublicIPAutoWithNilClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("93.184.216.34\n"))
	}))
	defer server.Close()

	ip, err := ResolvePublicIP(context.Background(), "auto", nil, []string{server.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ip.Equal(net.ParseIP("93.184.216.34")) {
		t.Fatalf("unexpected IP: %v", ip)
	}
}

func TestDetectPublicIPWithNilClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("93.184.216.34\n"))
	}))
	defer server.Close()

	ip, err := DetectPublicIP(context.Background(), nil, []string{server.URL})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ip.Equal(net.ParseIP("93.184.216.34")) {
		t.Fatalf("unexpected IP: %v", ip)
	}
}

func TestDetectPublicIPWithEmptyEndpointsUsesDefaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("93.184.216.34\n"))
	}))
	defer server.Close()

	orig := defaultPublicIPEndpointProvider
	defer func() { defaultPublicIPEndpointProvider = orig }()
	defaultPublicIPEndpointProvider = func() []string { return []string{server.URL} }

	ip, err := DetectPublicIP(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ip.Equal(net.ParseIP("93.184.216.34")) {
		t.Fatalf("unexpected IP: %v", ip)
	}
}

func TestDetectPublicIPAggregatesFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not an ip"))
	}))
	defer server.Close()

	_, err := DetectPublicIP(context.Background(), server.Client(), []string{server.URL, server.URL})
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), server.URL) {
		t.Fatalf("expected aggregated endpoint failures, got: %v", err)
	}
	if !strings.Contains(err.Error(), ";") {
		t.Fatalf("expected multiple failures joined by semicolon, got: %v", err)
	}
}

func TestDetectPublicIPFromEndpointRejectsNon2xxStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	_, err := detectPublicIPFromEndpoint(context.Background(), server.Client(), server.URL)
	if err == nil {
		t.Fatalf("expected error for non-2xx status")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected status in error, got: %v", err)
	}
}

type failingRoundTripper struct{ err error }

func (f failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, f.err
}

func TestDetectPublicIPFromEndpointReturnsClientDoError(t *testing.T) {
	wantErr := errors.New("network unreachable")
	client := &http.Client{Transport: failingRoundTripper{err: wantErr}}

	_, err := detectPublicIPFromEndpoint(context.Background(), client, "http://example.com")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected client error %v, got %v", wantErr, err)
	}
}

type errorReadCloser struct {
	io.Reader
	err error
}

func (e errorReadCloser) Read(p []byte) (int, error) {
	n, err := e.Reader.Read(p)
	if err == io.EOF {
		return n, e.err
	}
	return n, err
}

func (errorReadCloser) Close() error { return nil }

type bodyReturningRoundTripper struct{ body io.ReadCloser }

func (b bodyReturningRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       b.body,
	}, nil
}

func TestDetectPublicIPFromEndpointReturnsRequestError(t *testing.T) {
	// A malformed URL causes http.NewRequestWithContext to fail before the
	// HTTP client is invoked.
	_, err := detectPublicIPFromEndpoint(context.Background(), http.DefaultClient, "http://[fe80::%lo0")
	if err == nil {
		t.Fatalf("expected error for malformed URL")
	}
}

func TestDetectPublicIPFromEndpointReturnsReadError(t *testing.T) {
	wantErr := errors.New("read failed")
	body := errorReadCloser{Reader: strings.NewReader("93.184.216.34"), err: wantErr}
	client := &http.Client{Transport: bodyReturningRoundTripper{body: body}}

	_, err := detectPublicIPFromEndpoint(context.Background(), client, "http://example.com")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected read error %v, got %v", wantErr, err)
	}
}
