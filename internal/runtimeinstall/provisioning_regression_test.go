package runtimeinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Audit #296: a PATH entry like /usr/bin/go is typically a symlink into a
// versioned libdir. Deriving GOROOT from the unresolved link yields /usr and
// the toolchain loses its standard library.
func TestGoRootForResolvesSymlinkedGoBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink toolchain layout is a unix scenario")
	}
	root := t.TempDir()
	realRoot := filepath.Join(root, "go-1.27")
	realBin := filepath.Join(realRoot, "bin")
	if err := os.MkdirAll(realBin, 0o755); err != nil {
		t.Fatal(err)
	}
	goExe := filepath.Join(realBin, "go")
	// The toolchain reports its own root via `go env GOROOT`.
	script := "#!/bin/sh\necho '" + realRoot + "'\n"
	if err := os.WriteFile(goExe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(root, "usr", "bin")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(linkDir, "go")
	if err := os.Symlink(goExe, link); err != nil {
		t.Fatal(err)
	}
	if got := goRootFor(context.Background(), link); got != realRoot {
		t.Fatalf("goRootFor(symlink) = %q, want %q", got, realRoot)
	}
}

// When `go env` cannot answer (unusable shim), the resolved symlink target
// still yields the correct root instead of the link's parent prefix.
func TestGoRootForFallsBackToResolvedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink toolchain layout is a unix scenario")
	}
	root := t.TempDir()
	realRoot := filepath.Join(root, "lib", "go-1.27")
	if err := os.MkdirAll(filepath.Join(realRoot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	goExe := filepath.Join(realRoot, "bin", "go")
	if err := os.WriteFile(goExe, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(linkDir, "go")
	if err := os.Symlink(goExe, link); err != nil {
		t.Fatal(err)
	}
	if got := goRootFor(context.Background(), link); got != realRoot {
		t.Fatalf("goRootFor(resolved) = %q, want %q", got, realRoot)
	}
}

// buildFakeGoArchive returns a gzipped tar matching the Go release layout with
// an executable bin/go member, plus its sha256.
func buildFakeGoArchive(t *testing.T) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, dir := range []string{"go/", "go/bin/", "go/src/", "go/pkg/"} {
		if err := tw.WriteHeader(&tar.Header{Name: dir, Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.WriteHeader(&tar.Header{Name: "go/bin/go", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len("#!/bin/sh\n"))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("#!/bin/sh\n")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

// stubGoDownload pins the archive hash for the test platform and points the
// toolchain client at a transport serving the archive.
func stubGoDownload(t *testing.T, gt *GoToolchain, archive []byte, hash string) {
	t.Helper()
	platform := runtime.GOOS + "-" + runtime.GOARCH
	old := defaultGoSHA256[platform]
	defaultGoSHA256[platform] = hash
	t.Cleanup(func() { defaultGoSHA256[platform] = old })
	gt.client = &http.Client{Transport: &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Status:     "200 OK",
			Body:       io.NopCloser(bytes.NewReader(archive)),
			Request:    req,
		}, nil
	}}}
}

// Audit #297: a directory (or otherwise unusable member) at bin/go previously
// satisfied the cache check and was returned as a ready toolchain forever.
func TestEnsureReprovisionsIncompleteCachedToolchain(t *testing.T) {
	cache := t.TempDir()
	gt := NewGoToolchain(cache)
	gt.Version = "0.0.0-test"
	archive, hash := buildFakeGoArchive(t)
	stubGoDownload(t, gt, archive, hash)

	goBin := filepath.Join(cache, "go0.0.0-test", "bin", "go")
	if runtime.GOOS == "windows" {
		goBin += ".exe"
	}
	// Interrupted extraction leftovers: bin/go exists but is a directory.
	if err := os.MkdirAll(goBin, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := gt.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	info, err := os.Stat(got)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("Ensure returned %q which is not a regular file: %v", got, err)
	}
}

func TestEnsureReprovisionsNonExecutableCachedBinary(t *testing.T) {
	cache := t.TempDir()
	gt := NewGoToolchain(cache)
	gt.Version = "0.0.0-test2"
	archive, hash := buildFakeGoArchive(t)
	stubGoDownload(t, gt, archive, hash)

	goBin := filepath.Join(cache, "go0.0.0-test2", "bin", "go")
	if runtime.GOOS == "windows" {
		goBin += ".exe"
	}
	if err := os.MkdirAll(filepath.Dir(goBin), 0o755); err != nil {
		t.Fatal(err)
	}
	// Truncated write leftover: a plain non-executable file.
	if err := os.WriteFile(goBin, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := gt.Ensure(context.Background())
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 == 0 {
		t.Fatalf("Ensure returned non-executable toolchain %q", got)
	}
}

// A failed extraction must not publish the staging tree as the final dir.
func TestEnsureFailedExtractionLeavesNoPublishedToolchain(t *testing.T) {
	cache := t.TempDir()
	gt := NewGoToolchain(cache)
	gt.Version = "0.0.0-test3"
	platform := runtime.GOOS + "-" + runtime.GOARCH
	old := defaultGoSHA256[platform]
	defaultGoSHA256[platform] = strings.Repeat("0", 64)
	t.Cleanup(func() { defaultGoSHA256[platform] = old })
	gt.client = &http.Client{Transport: &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Status: "200 OK", Body: io.NopCloser(strings.NewReader("not a gzip")), Request: req}, nil
	}}}
	_, err := gt.Ensure(context.Background())
	if err == nil {
		t.Fatal("expected checksum/extraction failure")
	}
	goDir := filepath.Join(cache, "go0.0.0-test3")
	if _, statErr := os.Stat(goDir); !os.IsNotExist(statErr) {
		t.Fatalf("failed extraction must not publish %s", goDir)
	}
}

// Audit #302: a pinned release fetch hitting the API rate limit must fall back
// to the release's expanded-assets page instead of aborting.
func TestFetchReleaseByTagFallsBackToExpandedAssets(t *testing.T) {
	html := `<a href="/owner/repo/releases/download/v9.9.9/asset-linux-amd64.tar.gz">asset</a>`
	var sawLatest bool
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/repos/owner/repo/releases/tags/v9.9.9":
			return &http.Response{StatusCode: 429, Status: "429 Too Many Requests", Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		case "/owner/repo/releases/expanded_assets/v9.9.9":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(html)), Request: req}, nil
		case "/owner/repo/releases/latest":
			sawLatest = true
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}}
	client := &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	release, err := fetchReleaseByTag(context.Background(), client, "owner/repo", "v9.9.9")
	if err != nil {
		t.Fatalf("fetchReleaseByTag: %v", err)
	}
	if release.TagName != "v9.9.9" {
		t.Fatalf("tag = %q, want v9.9.9", release.TagName)
	}
	if len(release.Assets) != 1 || release.Assets[0].Name != "asset-linux-amd64.tar.gz" {
		t.Fatalf("assets = %+v", release.Assets)
	}
	if sawLatest {
		t.Fatal("pinned-tag fallback must not consult /releases/latest")
	}
}

func TestFetchReleaseByTag403FallsBackToo(t *testing.T) {
	html := `<a href="/owner/repo/releases/download/v9.9.9/asset.tar.gz">asset</a>`
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/repos/owner/repo/releases/tags/v9.9.9":
			return &http.Response{StatusCode: 403, Status: "403 Forbidden", Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		case "/owner/repo/releases/expanded_assets/v9.9.9":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(html)), Request: req}, nil
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}}
	client := &http.Client{Transport: tr}
	release, err := fetchReleaseByTag(context.Background(), client, "owner/repo", "v9.9.9")
	if err != nil {
		t.Fatalf("fetchReleaseByTag: %v", err)
	}
	if release.TagName != "v9.9.9" || len(release.Assets) != 1 {
		t.Fatalf("release = %+v", release)
	}
}
