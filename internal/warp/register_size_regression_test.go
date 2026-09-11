package warp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistrarRegisterRejectsOversizedSuccessResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(bytes.Repeat([]byte("a"), maxRegistrationResponseBytes+64))
	}))
	defer server.Close()

	_, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size-limit error, got %v", err)
	}
}

func TestRegistrarRegisterRejectsOversizedErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write(bytes.Repeat([]byte("e"), maxRegistrationResponseBytes+32))
	}))
	defer server.Close()

	_, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size-limit error, got %v", err)
	}
}

func TestRegistrarRegisterStopsAfterSizeLimitOnChunkedBody(t *testing.T) {
	counter := &countingReadCloser{r: bytes.NewReader(bytes.Repeat([]byte("z"), maxRegistrationResponseBytes*4))}
	r := &Registrar{
		BaseURL: "http://example.com",
		Client: &http.Client{Transport: fixedResponseRoundTripper{resp: &http.Response{
			StatusCode:    http.StatusOK,
			Body:          counter,
			Header:        make(http.Header),
			ContentLength: -1,
		}}},
	}
	_, err := r.Register(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size-limit error, got %v", err)
	}
	if counter.n > maxRegistrationResponseBytes+1 {
		t.Fatalf("read %d bytes, want at most %d", counter.n, maxRegistrationResponseBytes+1)
	}
}

func TestRegistrarRegisterStopsAfterSizeLimitWhenContentLengthLies(t *testing.T) {
	counter := &countingReadCloser{r: bytes.NewReader(bytes.Repeat([]byte("z"), maxRegistrationResponseBytes*2))}
	header := make(http.Header)
	header.Set("Content-Length", "16")
	r := &Registrar{
		BaseURL: "http://example.com",
		Client: &http.Client{Transport: fixedResponseRoundTripper{resp: &http.Response{
			StatusCode:    http.StatusOK,
			Body:          counter,
			Header:        header,
			ContentLength: 16,
		}}},
	}
	_, err := r.Register(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected size-limit error, got %v", err)
	}
	if counter.n > maxRegistrationResponseBytes+1 {
		t.Fatalf("read %d bytes, want at most %d", counter.n, maxRegistrationResponseBytes+1)
	}
}

func TestRegistrarRegisterAcceptsResponseAtSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"device-123","token":"tok-456","account":{"license":"LICENSE-KEY"},"config":{"client_id":"t0o+","interface":{"addresses":{"v4":"172.16.0.2"}},"peers":[{"public_key":"PEERPUBKEY=","endpoint":{"host":"engage.cloudflareclient.com:2408"}}]}}`))
	}))
	defer server.Close()

	if _, err := (&Registrar{BaseURL: server.URL, Client: server.Client()}).Register(context.Background()); err != nil {
		t.Fatalf("Register: %v", err)
	}
}

type countingReadCloser struct {
	r io.Reader
	n int
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n
	return n, err
}

func (c *countingReadCloser) Close() error { return nil }
