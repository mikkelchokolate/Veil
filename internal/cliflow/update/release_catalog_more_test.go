package update

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestReleaseCatalogUsesHTTPClientFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	}))
	defer server.Close()

	catalog := NewReleaseCatalog("acme", "veil")
	catalog.BaseURL = server.URL
	catalog.HTTPClient = nil

	release, err := catalog.Latest()
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if release.TagName != "v1.0.0" {
		t.Fatalf("tag = %q", release.TagName)
	}
}

type rewriteTransport struct {
	base    http.RoundTripper
	rewrite func(*http.Request) *http.Request
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return rt.base.RoundTrip(rt.rewrite(req.Clone(req.Context())))
}

func TestReleaseCatalogUsesBaseURLFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/veil/releases/latest" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	}))
	defer server.Close()

	baseURL, _ := url.Parse(server.URL)
	client := server.Client()
	client.Transport = &rewriteTransport{
		base: client.Transport,
		rewrite: func(req *http.Request) *http.Request {
			req.URL.Scheme = baseURL.Scheme
			req.URL.Host = baseURL.Host
			req.Host = baseURL.Host
			return req
		},
	}

	orig := HTTPClient
	HTTPClient = client
	defer func() { HTTPClient = orig }()

	catalog := NewReleaseCatalog("acme", "veil")
	catalog.BaseURL = ""

	release, err := catalog.Latest()
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if release.TagName != "v1.0.0" {
		t.Fatalf("tag = %q", release.TagName)
	}
}

func TestReleaseCatalogTrimsTrailingSlash(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/veil/releases/latest" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	}))
	defer server.Close()

	catalog := NewReleaseCatalog("acme", "veil")
	catalog.BaseURL = server.URL + "/"
	catalog.HTTPClient = server.Client()

	_, err := catalog.Latest()
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
}

func TestReleaseCatalogReturnsErrorOnNonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	catalog := NewReleaseCatalog("acme", "veil")
	catalog.BaseURL = server.URL
	catalog.HTTPClient = server.Client()

	_, err := catalog.Latest()
	if err == nil {
		t.Fatal("expected error for non-OK response")
	}
}

func TestReleaseCatalogReturnsErrorOnInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	catalog := NewReleaseCatalog("acme", "veil")
	catalog.BaseURL = server.URL
	catalog.HTTPClient = server.Client()

	_, err := catalog.Latest()
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestReleaseCatalogReturnsErrorOnHTTPFailure(t *testing.T) {
	catalog := NewReleaseCatalog("acme", "veil")
	catalog.BaseURL = "http://127.0.0.1:1"
	catalog.HTTPClient = http.DefaultClient

	_, err := catalog.Latest()
	if err == nil {
		t.Fatal("expected error when HTTP request fails")
	}
}

func TestReleaseCatalogLatestContextHonoursContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	}))
	defer server.Close()

	catalog := NewReleaseCatalog("acme", "veil")
	catalog.BaseURL = server.URL
	catalog.HTTPClient = server.Client()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	_, err := catalog.LatestContext(ctx)
	if err == nil {
		t.Fatal("expected context deadline error")
	}
}

func TestReleaseCatalogReturnsErrorOnRequestFailure(t *testing.T) {
	catalog := NewReleaseCatalog("acme", "veil")
	catalog.BaseURL = "http://[::1]:1"
	catalog.HTTPClient = &http.Client{Timeout: 1 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := catalog.LatestContext(ctx)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestReleaseCatalogReturnsErrorOnInvalidRequestURL(t *testing.T) {
	catalog := NewReleaseCatalog("acme", "veil")
	catalog.BaseURL = "http://[::1%bad"
	catalog.HTTPClient = http.DefaultClient

	_, err := catalog.Latest()
	if err == nil {
		t.Fatal("expected error for invalid request URL")
	}
}
