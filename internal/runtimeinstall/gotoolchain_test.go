package runtimeinstall

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestPinnedGoChecksumsMatchUpstream guards against a wrong pinned Go
// toolchain checksum, which silently breaks `veil runtime install` on a
// fresh server with no system Go (the installer provisions Go itself, then
// rejects the download as a checksum mismatch). It queries the go.dev
// release index. In the required `test` job (non-short mode) fetch, HTTP and
// decode failures are fatal — a soft skip would green the pin gate on a
// registry outage or API change (#431). `go test -short` is the only escape,
// reserved for offline local development.
func TestPinnedGoChecksumsMatchUpstream(t *testing.T) {
	skipOrFail := func(format string, args ...any) {
		if testing.Short() {
			t.Skipf(format, args...)
		}
		t.Fatalf(format, args...)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Get("https://go.dev/dl/?mode=json&include=all")
	if err != nil {
		skipOrFail("cannot reach go.dev release index (required-job checksum gate must not soft-skip): %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		skipOrFail("go.dev returned HTTP %d (required-job checksum gate must not soft-skip)", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		skipOrFail("read go.dev index failed (required-job checksum gate must not soft-skip): %v", err)
	}

	type goFile struct {
		Filename string `json:"filename"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Kind     string `json:"kind"`
		SHA256   string `json:"sha256"`
	}
	type goRelease struct {
		Version string   `json:"version"`
		Files   []goFile `json:"files"`
	}
	var releases []goRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		skipOrFail("decode go.dev index failed (required-job checksum gate must not soft-skip): %v", err)
	}

	// platform key (e.g. "linux-amd64") -> upstream sha256
	upstream := map[string]string{}
	for _, r := range releases {
		if r.Version != "go"+defaultGoVersion {
			continue
		}
		for _, f := range r.Files {
			if f.Kind == "archive" {
				upstream[f.OS+"-"+f.Arch] = f.SHA256
			}
		}
	}

	for platform, want := range defaultGoSHA256 {
		got, ok := upstream[platform]
		if !ok {
			t.Fatalf("go.dev index has no %s archive for go%s", platform, defaultGoVersion)
		}
		if got != want {
			t.Fatalf("pinned Go checksum for %s = %q, upstream publishes %q", platform, want, got)
		}
	}
}
