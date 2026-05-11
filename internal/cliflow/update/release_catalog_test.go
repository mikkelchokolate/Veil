package update

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateReleaseCatalogFetchesLatestRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/veil/releases/latest" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("User-Agent") != "veil" {
			t.Fatalf("missing User-Agent: %q", r.Header.Get("User-Agent"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.2.3","assets":[{"name":"checksums.txt","browser_download_url":"https://example.com/checksums"}]}`))
	}))
	t.Cleanup(server.Close)

	catalog := NewReleaseCatalog("acme", "veil")
	catalog.BaseURL = server.URL
	catalog.HTTPClient = server.Client()

	release, err := catalog.Latest()
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	if release.TagName != "v1.2.3" || len(release.Assets) != 1 || release.Assets[0].Name != "checksums.txt" {
		t.Fatalf("release = %+v", release)
	}
}
