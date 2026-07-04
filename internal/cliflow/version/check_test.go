package version

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewCheckUsesDefaultLatestFunc(t *testing.T) {
	c := NewCheck("v0.0.1", io.Discard, nil)
	if c.latest == nil {
		t.Fatal("expected default latest func")
	}
	if fmt.Sprintf("%p", c.latest) == "" {
		t.Fatal("expected non-nil function pointer")
	}
}

func TestCheckReportsNewerRelease(t *testing.T) {
	var out bytes.Buffer
	check := NewCheck("v0.3.16", &out, func() (string, error) { return "v0.3.17", nil })
	if err := check.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Newer release available: v0.3.16 → v0.3.17",
		"Download: https://github.com/mikkelchokolate/Veil/releases/tag/v0.3.17",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("output missing %q:\n%s", want, got)
		}
	}
}

func TestCheckReportsOlderRelease(t *testing.T) {
	var out bytes.Buffer
	check := NewCheck("v0.3.18", &out, func() (string, error) { return "v0.3.17", nil })
	if err := check.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "Running a version newer than the latest release (v0.3.18 > v0.3.17).") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestCheckReportsUpToDate(t *testing.T) {
	var out bytes.Buffer
	check := NewCheck("v0.3.17", &out, func() (string, error) { return "v0.3.17", nil })
	if err := check.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "Veil is up to date (v0.3.17).") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestCheckNoReleases(t *testing.T) {
	var out bytes.Buffer
	check := NewCheck("v0.3.17", &out, func() (string, error) { return "", nil })
	if err := check.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "No releases found on GitHub.") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestCheckLatestFuncError(t *testing.T) {
	check := NewCheck("v0.3.17", io.Discard, func() (string, error) { return "", errors.New("boom") })
	err := check.Run()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "update check failed: boom") {
		t.Fatalf("error = %v", err)
	}
}

func TestFetchLatestReleaseTagSuccess(t *testing.T) {
	want := "v1.2.3"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Errorf("Accept = %q, want %q", got, "application/vnd.github+json")
		}
		if got := r.Header.Get("User-Agent"); got != "veil" {
			t.Errorf("User-Agent = %q, want %q", got, "veil")
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": want})
	}))
	defer server.Close()

	oldClient := HTTPClient
	oldURL := releasesAPIURL
	HTTPClient = server.Client()
	releasesAPIURL = server.URL + "/releases/latest"
	t.Cleanup(func() {
		HTTPClient = oldClient
		releasesAPIURL = oldURL
	})

	got, err := FetchLatestReleaseTag()
	if err != nil {
		t.Fatalf("FetchLatestReleaseTag: %v", err)
	}
	if got != want {
		t.Fatalf("tag = %q, want %q", got, want)
	}
}

func TestFetchLatestReleaseTagNonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	oldClient := HTTPClient
	oldURL := releasesAPIURL
	HTTPClient = server.Client()
	releasesAPIURL = server.URL + "/releases/latest"
	t.Cleanup(func() {
		HTTPClient = oldClient
		releasesAPIURL = oldURL
	})

	_, err := FetchLatestReleaseTag()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "GitHub API returned 403 Forbidden") {
		t.Fatalf("error = %v", err)
	}
}

func TestFetchLatestReleaseTagInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "not-json")
	}))
	defer server.Close()

	oldClient := HTTPClient
	oldURL := releasesAPIURL
	HTTPClient = server.Client()
	releasesAPIURL = server.URL + "/releases/latest"
	t.Cleanup(func() {
		HTTPClient = oldClient
		releasesAPIURL = oldURL
	})

	_, err := FetchLatestReleaseTag()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "parse release JSON") {
		t.Fatalf("error = %v", err)
	}
}

func TestFetchLatestReleaseTagInvalidURL(t *testing.T) {
	oldURL := releasesAPIURL
	releasesAPIURL = "://invalid-url"
	t.Cleanup(func() { releasesAPIURL = oldURL })

	_, err := FetchLatestReleaseTag()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchLatestReleaseTagNetworkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	oldClient := HTTPClient
	oldURL := releasesAPIURL
	HTTPClient = server.Client()
	releasesAPIURL = server.URL + "/releases/latest"
	t.Cleanup(func() {
		HTTPClient = oldClient
		releasesAPIURL = oldURL
	})

	_, err := FetchLatestReleaseTag()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "fetch releases") {
		t.Fatalf("error = %v", err)
	}
}

func TestFetchLatestReleaseTagBodyReadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}))
	defer server.Close()

	oldClient := HTTPClient
	oldURL := releasesAPIURL
	HTTPClient = &http.Client{Timeout: 50 * time.Millisecond}
	releasesAPIURL = server.URL + "/releases/latest"
	t.Cleanup(func() {
		HTTPClient = oldClient
		releasesAPIURL = oldURL
	})

	_, err := FetchLatestReleaseTag()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.2.0", "1.3.0", -1},
		{"v1.4.0", "1.3.0", 1},
		{"v1.3.0", "1.3.0", 0},
		{"1.0.0", "v1.0.0", 0},
		{"1.10.0", "1.2.0", 1},
		{"1.0", "1.0.1", -1},
		{"1.0.1", "1.0", 1},
		{"", "1.0", -1},
		{"1.0", "", 1},
		{"1.abc", "1.0", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
