package update

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceBinaryFromArchiveReturnsBackupError(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "veil")
	if err := os.Mkdir(currentPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)

	archive := createTestTarGz(t, "veil", []byte("new-binary"))
	_, err := ReplaceBinaryFromArchive(currentPath, archive, true)
	if err == nil {
		t.Fatal("expected backup error")
	}
}

func TestReplaceBinaryFromArchiveReturnsReplaceError(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "veil")
	if err := os.WriteFile(currentPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	archive := createTestTarGz(t, "veil", []byte("new-binary"))

	orig := ReplaceBinaryAtomic
	ReplaceBinaryAtomic = func(string, []byte) error { return errors.New("replace failed") }
	defer func() { ReplaceBinaryAtomic = orig }()

	_, err := ReplaceBinaryFromArchive(currentPath, archive, true)
	if err == nil {
		t.Fatal("expected replace error")
	}
}

func TestDownloadAssetReturnsErrorOnInvalidURL(t *testing.T) {
	orig := HTTPClient
	HTTPClient = http.DefaultClient
	defer func() { HTTPClient = orig }()

	_, err := DownloadAsset("http://[::1%bad:80/asset")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestDownloadAssetReturnsErrorOnDoFailure(t *testing.T) {
	orig := HTTPClient
	HTTPClient = &http.Client{Transport: errorRoundTripper{err: errors.New("network unreachable")}}
	defer func() { HTTPClient = orig }()

	_, err := DownloadAsset("https://example.com/asset")
	if err == nil {
		t.Fatal("expected error from RoundTripper")
	}
}

type errorRoundTripper struct {
	err error
}

func (rt errorRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, rt.err
}

func TestReleaseCatalogReturnsErrorOnBodyReadFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("short"))
	}))
	defer server.Close()

	catalog := NewReleaseCatalog("acme", "veil")
	catalog.BaseURL = server.URL
	catalog.HTTPClient = server.Client()

	_, err := catalog.Latest()
	if err == nil {
		t.Fatal("expected body read error")
	}
}

func TestFetchLatestReleaseUsesGitHubAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/mikkelchokolate/Veil/releases/latest" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
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

	release, err := FetchLatestRelease()
	if err != nil {
		t.Fatalf("FetchLatestRelease: %v", err)
	}
	if release.TagName != "v9.9.9" {
		t.Fatalf("tag = %q", release.TagName)
	}
}

type errReader struct{}

func (errReader) Read(p []byte) (int, error) {
	return 0, errors.New("read failed")
}

func (errReader) Close() error {
	return nil
}

func TestReleaseCatalogReturnsErrorOnBodyReadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, errReader{})
	}))
	defer server.Close()

	catalog := NewReleaseCatalog("acme", "veil")
	catalog.BaseURL = server.URL
	catalog.HTTPClient = server.Client()

	_, err := catalog.Latest()
	if err == nil {
		t.Fatal("expected body read error")
	}
}
