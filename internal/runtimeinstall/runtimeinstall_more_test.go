package runtimeinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeTransport is a test http.RoundTripper that delegates to a handler.
type fakeTransport struct {
	handler func(*http.Request) (*http.Response, error)
}

func (f *fakeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if f.handler != nil {
		return f.handler(req)
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Request: req}, nil
}

func TestDefaultBinDir(t *testing.T) {
	if got := DefaultBinDir(); got != "/usr/local/bin" {
		t.Fatalf("DefaultBinDir = %q, want /usr/local/bin", got)
	}
}

func TestWithDefaultsSetsAllDefaults(t *testing.T) {
	opts := Options{}.withDefaults()
	if opts.BinDir != "/usr/local/bin" {
		t.Errorf("BinDir = %q", opts.BinDir)
	}
	if opts.Arch != "amd64" {
		t.Errorf("Arch = %q", opts.Arch)
	}
	if opts.HTTPClient == nil {
		t.Error("HTTPClient not set")
	}
	if opts.FetchRelease == nil {
		t.Error("FetchRelease not set")
	}
	if opts.Download == nil {
		t.Error("Download not set")
	}
	if opts.GoInstall == nil {
		t.Error("GoInstall not set")
	}
	if opts.BuildCaddy == nil {
		t.Error("BuildCaddy not set")
	}
	if opts.EnsureGo == nil {
		t.Error("EnsureGo not set")
	}
	if opts.LookPath == nil {
		t.Error("LookPath not set")
	}
	if opts.Now == nil {
		t.Error("Now not set")
	}
}

func TestWithDefaultsPreservesCustomValues(t *testing.T) {
	custom := Options{
		BinDir:        "/custom",
		Arch:          "arm64",
		CaddyCacheDir: "/cache",
		HTTPClient:    &http.Client{},
		FetchRelease:  func(ctx context.Context, repo string) (*Release, error) { return nil, nil },
		Download:      func(ctx context.Context, url string) ([]byte, error) { return nil, nil },
		GoInstall:     func(ctx context.Context, binDir, sourcePackage string) error { return nil },
		BuildCaddy:    func(ctx context.Context, outPath string) error { return nil },
		EnsureGo:      func(ctx context.Context) (string, error) { return "", nil },
		LookPath:      func(string) (string, error) { return "", nil },
		Now:           func() time.Time { return time.Time{} },
	}
	got := custom.withDefaults()
	if got.BinDir != "/custom" || got.Arch != "arm64" || got.CaddyCacheDir != "/cache" {
		t.Fatalf("withDefaults overrode custom values: %+v", got)
	}
}

func TestWithDefaultsFetchRelease(t *testing.T) {
	body := `{"tag_name":"v1.0.0","assets":[]}`
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}}
	opts := Options{HTTPClient: &http.Client{Transport: tr}}.withDefaults()
	release, err := opts.FetchRelease(context.Background(), "owner/repo")
	if err != nil || release.TagName != "v1.0.0" {
		t.Fatalf("FetchRelease: %v, %+v", err, release)
	}
}

func TestWithDefaultsDownload(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("data")), Request: req}, nil
	}}
	opts := Options{HTTPClient: &http.Client{Transport: tr}}.withDefaults()
	body, err := opts.Download(context.Background(), "https://example.com/file")
	if err != nil || string(body) != "data" {
		t.Fatalf("Download: %v, %q", err, body)
	}
}

func TestWithDefaultsGoInstall(t *testing.T) {
	dir := t.TempDir()
	goBin := filepath.Join(dir, "go")
	script := `#!/usr/bin/env bash
case "$1" in
  version) echo 'go version go1.99.0 linux/amd64' ;;
  install) : ;;
  *) : ;;
esac
exit 0
`
	if err := os.WriteFile(goBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	opts := Options{
		BinDir:        binDir,
		CaddyCacheDir: dir,
		EnsureGo:      func(ctx context.Context) (string, error) { return goBin, nil },
	}.withDefaults()
	if err := opts.GoInstall(context.Background(), binDir, "example.com/pkg@latest"); err != nil {
		t.Fatalf("GoInstall: %v", err)
	}
}

func TestWithDefaultsBuildCaddy(t *testing.T) {
	dir := t.TempDir()
	goBin := fakeGoScript(t, dir)
	outPath := filepath.Join(dir, "caddy")
	opts := Options{
		CaddyCacheDir: dir,
		EnsureGo:      func(ctx context.Context) (string, error) { return goBin, nil },
	}.withDefaults()
	if err := opts.BuildCaddy(context.Background(), outPath); err != nil {
		t.Fatalf("BuildCaddy: %v", err)
	}
	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("caddy binary not written: %v", err)
	}
}

func TestInstallCreatesBinDirError(t *testing.T) {
	runtime := Runtime{Name: "hysteria2", Binary: "hysteria", Method: MethodRawBinary}
	opts := Options{BinDir: "/dev/null/veil/bin"}
	result := Install(context.Background(), opts, runtime)
	if result.Err == nil {
		t.Fatal("expected error creating bin dir")
	}
}

func TestInstallUnsupportedMethod(t *testing.T) {
	dir := t.TempDir()
	runtime := Runtime{Name: "x", Binary: "x", Method: Method("unknown")}
	opts := Options{BinDir: dir}
	result := Install(context.Background(), opts, runtime)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "unsupported method") {
		t.Fatalf("expected unsupported method error, got %v", result.Err)
	}
}

func hysteriaRuntime(t *testing.T) Runtime {
	t.Helper()
	return Runtime{
		Name:   "hysteria2",
		Binary: "hysteria",
		Method: MethodRawBinary,
		Repo:   "apernet/hysteria",
		AssetMatch: func(name string) bool {
			return name == "hysteria-linux-amd64"
		},
		ChecksumMatch: func(name string) bool {
			return name == "hashes.txt"
		},
	}
}

func mieruRuntime(t *testing.T) Runtime {
	t.Helper()
	return Runtime{
		Name:   "mieru",
		Binary: "mita",
		Method: MethodArchive,
		Repo:   "enfein/mieru",
		AssetMatch: func(name string) bool {
			return strings.HasPrefix(name, "mita_") && strings.HasSuffix(name, "_linux_amd64.tar.gz")
		},
		ChecksumMatch: func(name string) bool {
			return strings.HasPrefix(name, "mita_") && strings.HasSuffix(name, "_linux_amd64.tar.gz.sha256.txt")
		},
	}
}

func caddyRuntime(t *testing.T) Runtime {
	t.Helper()
	return Runtime{
		Name:   "naiveproxy",
		Binary: "caddy",
		Method: MethodCaddyNaive,
	}
}

func olcrtcRuntime(t *testing.T) Runtime {
	t.Helper()
	return Runtime{
		Name:          "olcrtc",
		Binary:        "olcrtc",
		Method:        MethodGoInstall,
		SourcePackage: "github.com/openlibrecommunity/olcrtc/cmd/olcrtc@latest",
	}
}

func warpRuntime(t *testing.T) Runtime {
	t.Helper()
	for _, r := range Catalog("amd64") {
		if r.Binary == "sing-box" {
			return r
		}
	}
	t.Fatal("warp runtime not found in catalog")
	return Runtime{}
}

func TestInstallRawBinaryNoAssetMatch(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		BinDir: dir,
		Arch:   "amd64",
		FetchRelease: func(ctx context.Context, repo string) (*Release, error) {
			return &Release{TagName: "v1", Assets: []Asset{{Name: "other"}}}, nil
		},
	}
	result := Install(context.Background(), opts, hysteriaRuntime(t))
	if result.Err == nil || !strings.Contains(result.Err.Error(), "no asset") {
		t.Fatalf("expected no asset error, got %v", result.Err)
	}
}

func TestInstallArchiveNoAssetMatch(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		BinDir: dir,
		Arch:   "amd64",
		FetchRelease: func(ctx context.Context, repo string) (*Release, error) {
			return &Release{TagName: "v1", Assets: []Asset{{Name: "other"}}}, nil
		},
	}
	result := Install(context.Background(), opts, mieruRuntime(t))
	if result.Err == nil || !strings.Contains(result.Err.Error(), "no asset") {
		t.Fatalf("expected no asset error, got %v", result.Err)
	}
}

func TestInstallGoInstallError(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		BinDir:   dir,
		Arch:     "amd64",
		EnsureGo: func(ctx context.Context) (string, error) { return "/usr/local/go/bin/go", nil },
		GoInstall: func(ctx context.Context, binDir, sourcePackage string) error {
			return errors.New("go install failed")
		},
	}
	result := Install(context.Background(), opts, olcrtcRuntime(t))
	if result.Err == nil || !strings.Contains(result.Err.Error(), "go install failed") {
		t.Fatalf("expected go install error, got %v", result.Err)
	}
}

func TestInstallCaddyNaiveError(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		BinDir:   dir,
		Arch:     "amd64",
		EnsureGo: func(ctx context.Context) (string, error) { return "/usr/local/go/bin/go", nil },
		BuildCaddy: func(ctx context.Context, outPath string) error {
			return errors.New("build failed")
		},
	}
	result := Install(context.Background(), opts, caddyRuntime(t))
	if result.Err == nil || !strings.Contains(result.Err.Error(), "build failed") {
		t.Fatalf("expected build error, got %v", result.Err)
	}
}

func TestInstallFromReleaseFetchError(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		BinDir: dir,
		Arch:   "amd64",
		FetchRelease: func(ctx context.Context, repo string) (*Release, error) {
			return nil, errors.New("fetch failed")
		},
	}
	result := Install(context.Background(), opts, hysteriaRuntime(t))
	if result.Err == nil || !strings.Contains(result.Err.Error(), "resolve") {
		t.Fatalf("expected resolve error, got %v", result.Err)
	}
}

func TestInstallFromReleaseDownloadError(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		BinDir: dir,
		Arch:   "amd64",
		FetchRelease: func(ctx context.Context, repo string) (*Release, error) {
			return &Release{TagName: "v1", Assets: []Asset{
				{Name: "hysteria-linux-amd64", BrowserDownloadURL: "https://example/hy"},
			}}, nil
		},
		Download: func(ctx context.Context, url string) ([]byte, error) {
			return nil, errors.New("download failed")
		},
	}
	result := Install(context.Background(), opts, hysteriaRuntime(t))
	if result.Err == nil || !strings.Contains(result.Err.Error(), "download") {
		t.Fatalf("expected download error, got %v", result.Err)
	}
}

func TestInstallFromReleaseChecksumDownloadError(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		BinDir: dir,
		Arch:   "amd64",
		FetchRelease: func(ctx context.Context, repo string) (*Release, error) {
			return &Release{TagName: "v1", Assets: []Asset{
				{Name: "hysteria-linux-amd64", BrowserDownloadURL: "https://example/hy"},
				{Name: "hashes.txt", BrowserDownloadURL: "https://example/hashes"},
			}}, nil
		},
		Download: func(ctx context.Context, url string) ([]byte, error) {
			if url == "https://example/hy" {
				return []byte("binary"), nil
			}
			return nil, errors.New("checksum download failed")
		},
	}
	result := Install(context.Background(), opts, hysteriaRuntime(t))
	if result.Err == nil || !strings.Contains(result.Err.Error(), "download checksums") {
		t.Fatalf("expected checksum download error, got %v", result.Err)
	}
}

func TestInstallFromReleaseSkipsMissingChecksumAsset(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		BinDir: dir,
		Arch:   "amd64",
		FetchRelease: func(ctx context.Context, repo string) (*Release, error) {
			return &Release{TagName: "v1", Assets: []Asset{
				{Name: "hysteria-linux-amd64", BrowserDownloadURL: "https://example/hy"},
			}}, nil
		},
		Download: func(ctx context.Context, url string) ([]byte, error) {
			return []byte("binary"), nil
		},
	}
	result := Install(context.Background(), opts, hysteriaRuntime(t))
	if result.Err != nil {
		t.Fatalf("Install: %v", result.Err)
	}
}

func TestInstallFromReleaseExtractError(t *testing.T) {
	dir := t.TempDir()
	opts := Options{
		BinDir: dir,
		Arch:   "amd64",
		FetchRelease: func(ctx context.Context, repo string) (*Release, error) {
			return &Release{TagName: "v1", Assets: []Asset{
				{Name: "mita_3.0.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example/mita"},
			}}, nil
		},
		Download: func(ctx context.Context, url string) ([]byte, error) {
			return []byte("not-a-tar-gz"), nil
		},
	}
	result := Install(context.Background(), opts, mieruRuntime(t))
	if result.Err == nil {
		t.Fatal("expected extract error")
	}
}

func TestWriteBinaryAtomicInvalidDir(t *testing.T) {
	err := writeBinaryAtomic("/dev/null/veil/bin/x", []byte("x"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWriteBinaryAtomicRenameFails(t *testing.T) {
	dir := t.TempDir()
	err := writeBinaryAtomic(filepath.Join(dir, "nonexistent", "dest"), []byte("x"))
	if err == nil {
		t.Fatal("expected rename error")
	}
}

func TestFindAssetNilMatch(t *testing.T) {
	_, ok := findAsset([]Asset{{Name: "x"}}, nil)
	if ok {
		t.Fatal("expected no match with nil matcher")
	}
}

func TestFindAssetNoMatch(t *testing.T) {
	_, ok := findAsset([]Asset{{Name: "x"}}, func(n string) bool { return false })
	if ok {
		t.Fatal("expected no match")
	}
}

func TestExtractArchiveBinaryInvalidGzip(t *testing.T) {
	if _, err := ExtractArchiveBinary([]byte("not gzip"), "bin"); err == nil {
		t.Fatal("expected gzip error")
	}
}

func TestExtractArchiveBinaryInvalidTar(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write([]byte("not a tar"))
	gz.Close()
	if _, err := ExtractArchiveBinary(buf.Bytes(), "bin"); err == nil {
		t.Fatal("expected tar error")
	}
}

func TestVerifyChecksumSHA512(t *testing.T) {
	body := []byte("caddy-binary")
	sum := sha512.Sum512(body)
	checksums := fmt.Sprintf("%s  caddy\n", hex.EncodeToString(sum[:]))
	if err := VerifyChecksum(body, "caddy.tar.gz", "caddy", []byte(checksums)); err != nil {
		t.Fatalf("VerifyChecksum SHA-512: %v", err)
	}
}

func TestVerifyChecksumUnsupportedLength(t *testing.T) {
	checksums := "abcd1234  asset.tar.gz\n"
	err := VerifyChecksum([]byte("x"), "asset.tar.gz", "bin", []byte(checksums))
	if err == nil || !strings.Contains(err.Error(), "unsupported checksum length") {
		t.Fatalf("expected unsupported checksum length error, got %v", err)
	}
}

func TestExtractChecksumWindowsPath(t *testing.T) {
	checksums := "deadbeef0123456789abcdefdeadbeef0123456789abcdefdeadbeef01234567  build\\binary\n"
	got := extractChecksum(checksums, "binary", "binary")
	if got == "" {
		t.Fatal("expected checksum from windows path")
	}
}

func TestParseRelease(t *testing.T) {
	body := []byte(`{"tag_name":"v1","assets":[{"name":"a","browser_download_url":"https://example/a"}]}`)
	r, err := parseRelease(body)
	if err != nil || r.TagName != "v1" || len(r.Assets) != 1 {
		t.Fatalf("parseRelease: %+v, %v", r, err)
	}
}

func TestParseReleaseInvalidJSON(t *testing.T) {
	_, err := parseRelease([]byte("{invalid"))
	if err == nil {
		t.Fatal("expected JSON parse error")
	}
}

func TestFetchLatestReleaseAPISuccess(t *testing.T) {
	body := `{"tag_name":"v1.0.0","assets":[{"name":"x","browser_download_url":"https://example/x"}]}`
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Request:    req,
		}, nil
	}}
	client := &http.Client{Transport: tr}
	release, err := fetchLatestRelease(context.Background(), client, "owner/repo")
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if release.TagName != "v1.0.0" {
		t.Fatalf("tag = %q", release.TagName)
	}
}

func TestFetchLatestReleaseAPIError(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("network error")
	}}
	client := &http.Client{Transport: tr}
	_, err := fetchLatestRelease(context.Background(), client, "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "network error") {
		t.Fatalf("expected network error, got %v", err)
	}
}

func TestFetchLatestReleaseAPIRateLimitFallsBack(t *testing.T) {
	html := `<a href="/owner/repo/releases/download/v1.0.0/asset.tar.gz">asset</a>`
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host + req.URL.Path {
		case "api.github.com/repos/owner/repo/releases/latest":
			return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		case "github.com/owner/repo/releases/latest":
			return &http.Response{
				StatusCode: 302,
				Header:     http.Header{"Location": []string{"https://github.com/owner/repo/releases/tag/v1.0.0"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		case "github.com/owner/repo/releases/expanded_assets/v1.0.0":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(html)), Request: req}, nil
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}}
	client := &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	release, err := fetchLatestRelease(context.Background(), client, "owner/repo")
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if release.TagName != "v1.0.0" {
		t.Fatalf("tag = %q", release.TagName)
	}
}

func TestFetchLatestReleaseAPINonOK(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Status: "500 Internal Server Error", Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}}
	client := &http.Client{Transport: tr}
	_, err := fetchLatestRelease(context.Background(), client, "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "GitHub API") {
		t.Fatalf("expected GitHub API error, got %v", err)
	}
}

func TestFetchLatestReleaseWebResolveTagError(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/owner/repo/releases/latest" {
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: nil}, nil
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}}
	client := &http.Client{Transport: tr}
	_, err := fetchLatestReleaseWeb(context.Background(), client, "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "no redirect location") {
		t.Fatalf("expected resolve tag error, got %v", err)
	}
}

func TestFetchLatestReleaseWebSuccess(t *testing.T) {
	html := `<a href="/owner/repo/releases/download/v1.0.0/asset.tar.gz">asset</a>`
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/owner/repo/releases/latest":
			return &http.Response{
				StatusCode: 302,
				Header:     http.Header{"Location": []string{"https://github.com/owner/repo/releases/tag/v1.0.0"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		case "/owner/repo/releases/expanded_assets/v1.0.0":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(html)), Request: req}, nil
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}}
	client := &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	release, err := fetchLatestReleaseWeb(context.Background(), client, "owner/repo")
	if err != nil {
		t.Fatalf("fetchLatestReleaseWeb: %v", err)
	}
	if len(release.Assets) != 1 || release.Assets[0].Name != "asset.tar.gz" {
		t.Fatalf("assets = %+v", release.Assets)
	}
}

func TestFetchLatestReleaseWebError(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/owner/repo/releases/latest":
			return &http.Response{
				StatusCode: 302,
				Header:     http.Header{"Location": []string{"https://github.com/owner/repo/releases/tag/v1.0.0"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		case "/owner/repo/releases/expanded_assets/v1.0.0":
			return &http.Response{StatusCode: 500, Status: "500 Internal Server Error", Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}}
	client := &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	_, err := fetchLatestReleaseWeb(context.Background(), client, "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "expanded assets") {
		t.Fatalf("expected error, got %v", err)
	}
}

func TestResolveLatestTagWebSuccess(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 302,
			Header:     http.Header{"Location": []string{"https://github.com/owner/repo/releases/tag/v1.0.0"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}}
	client := &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	tag, err := resolveLatestTagWeb(context.Background(), client, "owner/repo")
	if err != nil || tag != "v1.0.0" {
		t.Fatalf("tag = %q, err = %v", tag, err)
	}
}

func TestResolveLatestTagWebFromRequestURL(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		u, _ := url.Parse("https://github.com/owner/repo/releases/tag/v2.0.0")
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    &http.Request{URL: u},
		}, nil
	}}
	client := &http.Client{Transport: tr}
	tag, err := resolveLatestTagWeb(context.Background(), client, "owner/repo")
	if err != nil || tag != "v2.0.0" {
		t.Fatalf("tag = %q, err = %v", tag, err)
	}
}

func TestResolveLatestTagWebNoLocation(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 302,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    nil,
		}, nil
	}}
	client := &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	_, err := resolveLatestTagWeb(context.Background(), client, "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "no redirect location") {
		t.Fatalf("expected no redirect location error, got %v", err)
	}
}

func TestResolveLatestTagWebUnexpectedPath(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 302,
			Header:     http.Header{"Location": []string{"https://github.com/owner/repo/unknown"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}}
	client := &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	_, err := resolveLatestTagWeb(context.Background(), client, "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "unexpected location path") {
		t.Fatalf("expected unexpected path error, got %v", err)
	}
}

func TestParseExpandedAssetsNoAssets(t *testing.T) {
	_, err := parseExpandedAssets("<html></html>", "v1.0.0", "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "no assets found") {
		t.Fatalf("expected no assets error, got %v", err)
	}
}

func TestDownloadURLSuccess(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("hello")), Request: req}, nil
	}}
	client := &http.Client{Transport: tr}
	body, err := downloadURL(context.Background(), client, "https://example.com/file")
	if err != nil || string(body) != "hello" {
		t.Fatalf("body = %q, err = %v", body, err)
	}
}

func TestDownloadURLErrorStatus(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Status: "404 Not Found", Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}}
	client := &http.Client{Transport: tr}
	_, err := downloadURL(context.Background(), client, "https://example.com/file")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error, got %v", err)
	}
}

func TestDownloadURLNetworkError(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	}}
	client := &http.Client{Transport: tr}
	_, err := downloadURL(context.Background(), client, "https://example.com/file")
	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("expected network error, got %v", err)
	}
}

func fakeGoInstallScript(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "go")
	script := `#!/usr/bin/env bash
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunGoInstallSuccess(t *testing.T) {
	dir := t.TempDir()
	goBin := fakeGoInstallScript(t, dir)
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runGoInstall(context.Background(), goBin, binDir, "example.com/pkg@latest"); err != nil {
		t.Fatalf("runGoInstall: %v", err)
	}
}

func TestRunGoInstallErrorWithOutput(t *testing.T) {
	dir := t.TempDir()
	goBin := filepath.Join(dir, "go")
	script := `#!/usr/bin/env bash
echo "compile error" >&2
exit 1
`
	if err := os.WriteFile(goBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	err := runGoInstall(context.Background(), goBin, binDir, "example.com/pkg@latest")
	if err == nil || !strings.Contains(err.Error(), "compile error") {
		t.Fatalf("expected compile error, got %v", err)
	}
}

func TestRunGoInstallErrorWithoutOutput(t *testing.T) {
	dir := t.TempDir()
	goBin := filepath.Join(dir, "go")
	script := `#!/usr/bin/env bash
exit 1
`
	if err := os.WriteFile(goBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	err := runGoInstall(context.Background(), goBin, binDir, "example.com/pkg@latest")
	if err == nil || !strings.Contains(err.Error(), "go install") {
		t.Fatalf("expected go install error, got %v", err)
	}
}

func TestRunCaddyNaiveBuildRemoveAllFails(t *testing.T) {
	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "cache-file")
	if err := os.WriteFile(cacheFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "caddy")
	err := runCaddyNaiveBuild(context.Background(), "/bin/true", cacheFile, outPath)
	if err == nil || !strings.Contains(err.Error(), "clean caddy build dir") {
		t.Fatalf("expected clean caddy build dir error, got %v", err)
	}
}

func TestRunCaddyNaiveBuildNonExistentGo(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "caddy")
	err := runCaddyNaiveBuild(context.Background(), filepath.Join(dir, "nonexistent-go"), dir, outPath)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunCaddyNaiveBuildModInitFails(t *testing.T) {
	dir := t.TempDir()
	goBin := filepath.Join(dir, "go")
	script := `#!/usr/bin/env bash
case "$1" in
  mod)
    case "$2" in
      init) echo "go mod init failed" >&2; exit 1 ;;
    esac
    ;;
esac
exit 0
`
	if err := os.WriteFile(goBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "caddy")
	err := runCaddyNaiveBuild(context.Background(), goBin, dir, outPath)
	if err == nil || !strings.Contains(err.Error(), "mod init") {
		t.Fatalf("expected mod init error, got %v", err)
	}
}

func TestRunCaddyNaiveBuildBuildFails(t *testing.T) {
	dir := t.TempDir()
	goBin := filepath.Join(dir, "go")
	script := `#!/usr/bin/env bash
case "$1" in
  mod) : ;;
  build) echo "build failed" >&2; exit 1 ;;
esac
exit 0
`
	if err := os.WriteFile(goBin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "caddy")
	err := runCaddyNaiveBuild(context.Background(), goBin, dir, outPath)
	if err == nil || !strings.Contains(err.Error(), "caddy build") {
		t.Fatalf("expected build error, got %v", err)
	}
}

func TestCatalogArm64(t *testing.T) {
	runtimes := Catalog("arm64")
	for _, r := range runtimes {
		if r.Name == "warp" && !r.AssetMatch("sing-box-1.13.13-linux-arm64.tar.gz") {
			t.Error("warp arm64 matcher failed")
		}
	}
}

// errorReader is an io.Reader that always returns a configured error.
type errorReader struct {
	err error
}

func (e *errorReader) Read([]byte) (int, error) {
	return 0, e.err
}

// fakeAtomicFile satisfies the atomicFile interface for testing writeBinaryAtomic
// error paths without needing a full filesystem.
type fakeAtomicFile struct {
	name     string
	writeErr error
	chmodErr error
	closeErr error
}

func (f *fakeAtomicFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *fakeAtomicFile) Name() string            { return f.name }
func (f *fakeAtomicFile) Chmod(os.FileMode) error { return f.chmodErr }
func (f *fakeAtomicFile) Close() error            { return f.closeErr }

func TestFetchLatestReleaseForbiddenFallsBack(t *testing.T) {
	html := `<a href="/owner/repo/releases/download/v1.0.0/asset.tar.gz">asset</a>`
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host + req.URL.Path {
		case "api.github.com/repos/owner/repo/releases/latest":
			return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
		case "github.com/owner/repo/releases/latest":
			return &http.Response{
				StatusCode: 302,
				Header:     http.Header{"Location": []string{"https://github.com/owner/repo/releases/tag/v1.0.0"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		case "github.com/owner/repo/releases/expanded_assets/v1.0.0":
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(html)), Request: req}, nil
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}}
	client := &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	release, err := fetchLatestRelease(context.Background(), client, "owner/repo")
	if err != nil {
		t.Fatalf("fetchLatestRelease: %v", err)
	}
	if release.TagName != "v1.0.0" {
		t.Fatalf("tag = %q", release.TagName)
	}
}

func TestFetchLatestReleaseAPIBodyReadError(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(&errorReader{err: errors.New("read failed")}),
			Request:    req,
		}, nil
	}}
	client := &http.Client{Transport: tr}
	_, err := fetchLatestRelease(context.Background(), client, "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("expected body read error, got %v", err)
	}
}

func TestFetchLatestReleaseWebDoError(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/owner/repo/releases/latest":
			return &http.Response{
				StatusCode: 302,
				Header:     http.Header{"Location": []string{"https://github.com/owner/repo/releases/tag/v1.0.0"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		case "/owner/repo/releases/expanded_assets/v1.0.0":
			return nil, errors.New("network down")
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}}
	client := &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	_, err := fetchLatestReleaseWeb(context.Background(), client, "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("expected network error, got %v", err)
	}
}

func TestFetchLatestReleaseWebBodyReadError(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/owner/repo/releases/latest":
			return &http.Response{
				StatusCode: 302,
				Header:     http.Header{"Location": []string{"https://github.com/owner/repo/releases/tag/v1.0.0"}},
				Body:       io.NopCloser(strings.NewReader("")),
				Request:    req,
			}, nil
		case "/owner/repo/releases/expanded_assets/v1.0.0":
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(&errorReader{err: errors.New("read failed")}),
				Request:    req,
			}, nil
		}
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Request: req}, nil
	}}
	client := &http.Client{Transport: tr, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	_, err := fetchLatestReleaseWeb(context.Background(), client, "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("expected body read error, got %v", err)
	}
}

func TestResolveLatestTagWebParseLocationError(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("")),
			// An invalid URL in the request forces url.Parse to fail in our code.
			Request: &http.Request{URL: &url.URL{Scheme: "http", Host: "[::1]:namedport", Path: "/owner/repo/releases/tag/v1.0.0"}},
		}, nil
	}}
	client := &http.Client{Transport: tr}
	_, err := resolveLatestTagWeb(context.Background(), client, "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "parse location") {
		t.Fatalf("expected parse location error, got %v", err)
	}
}

func TestResolveLatestTagWebDoError(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	}}
	client := &http.Client{Transport: tr}
	_, err := resolveLatestTagWeb(context.Background(), client, "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "network down") {
		t.Fatalf("expected network error, got %v", err)
	}
}

func TestParseExpandedAssetsSkipsDuplicates(t *testing.T) {
	html := `
		<a href="/owner/repo/releases/download/v1.0.0/asset.tar.gz">asset</a>
		<a href="/owner/repo/releases/download/v1.0.0/asset.tar.gz">asset</a>
	`
	assets, err := parseExpandedAssets(html, "v1.0.0", "owner/repo")
	if err != nil {
		t.Fatalf("parseExpandedAssets: %v", err)
	}
	if len(assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(assets))
	}
}

func TestFindAssetTieBreaksLexically(t *testing.T) {
	assets := []Asset{{Name: "b"}, {Name: "a"}}
	got, ok := findAsset(assets, func(n string) bool { return true })
	if !ok || got.Name != "a" {
		t.Fatalf("findAsset = %+v ok=%v", got, ok)
	}
}

func TestExtractArchiveBinarySkipsNonRegularEntries(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	// Directory entry should be skipped.
	if err := tw.WriteHeader(&tar.Header{Name: "dir/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	// Symlink entry should be skipped.
	if err := tw.WriteHeader(&tar.Header{Name: "link", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "bin"}); err != nil {
		t.Fatal(err)
	}
	// The actual binary.
	content := []byte("binary-data")
	if err := tw.WriteHeader(&tar.Header{Name: "bin", Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := ExtractArchiveBinary(buf.Bytes(), "bin")
	if err != nil {
		t.Fatalf("ExtractArchiveBinary: %v", err)
	}
	if string(got) != "binary-data" {
		t.Fatalf("got %q", string(got))
	}
}

func TestDownloadURLBodyReadError(t *testing.T) {
	tr := &fakeTransport{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(&errorReader{err: errors.New("read failed")}),
			Request:    req,
		}, nil
	}}
	client := &http.Client{Transport: tr}
	_, err := downloadURL(context.Background(), client, "https://example.com/file")
	if err == nil || !strings.Contains(err.Error(), "read failed") {
		t.Fatalf("expected body read error, got %v", err)
	}
}

func TestWriteBinaryAtomicCreateTempError(t *testing.T) {
	old := writeBinaryAtomicCreateTemp
	writeBinaryAtomicCreateTemp = func(dir, pattern string) (atomicFile, error) {
		return nil, errors.New("create temp failed")
	}
	defer func() { writeBinaryAtomicCreateTemp = old }()

	err := writeBinaryAtomic(filepath.Join(t.TempDir(), "bin"), []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "create temp failed") {
		t.Fatalf("expected create temp error, got %v", err)
	}
}

func TestWriteBinaryAtomicWriteError(t *testing.T) {
	dir := t.TempDir()
	old := writeBinaryAtomicCreateTemp
	writeBinaryAtomicCreateTemp = func(d, pattern string) (atomicFile, error) {
		return &fakeAtomicFile{name: filepath.Join(dir, "tmp-write"), writeErr: errors.New("write failed")}, nil
	}
	defer func() { writeBinaryAtomicCreateTemp = old }()

	err := writeBinaryAtomic(filepath.Join(dir, "dest"), []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("expected write error, got %v", err)
	}
}

func TestWriteBinaryAtomicChmodError(t *testing.T) {
	dir := t.TempDir()
	old := writeBinaryAtomicCreateTemp
	writeBinaryAtomicCreateTemp = func(d, pattern string) (atomicFile, error) {
		return &fakeAtomicFile{name: filepath.Join(dir, "tmp-chmod"), chmodErr: errors.New("chmod failed")}, nil
	}
	defer func() { writeBinaryAtomicCreateTemp = old }()

	err := writeBinaryAtomic(filepath.Join(dir, "dest"), []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "chmod failed") {
		t.Fatalf("expected chmod error, got %v", err)
	}
}

func TestWriteBinaryAtomicCloseError(t *testing.T) {
	dir := t.TempDir()
	old := writeBinaryAtomicCreateTemp
	writeBinaryAtomicCreateTemp = func(d, pattern string) (atomicFile, error) {
		return &fakeAtomicFile{name: filepath.Join(dir, "tmp-close"), closeErr: errors.New("close failed")}, nil
	}
	defer func() { writeBinaryAtomicCreateTemp = old }()

	err := writeBinaryAtomic(filepath.Join(dir, "dest"), []byte("x"))
	if err == nil || !strings.Contains(err.Error(), "close failed") {
		t.Fatalf("expected close error, got %v", err)
	}
}

func TestInstallGoInstallSkippedWhenNoGo(t *testing.T) {
	old := findBestGoFn
	findBestGoFn = func(minVersion string) (string, error) { return "", nil }
	defer func() { findBestGoFn = old }()

	dir := t.TempDir()
	opts := Options{
		BinDir:   dir,
		Arch:     "amd64",
		EnsureGo: func(ctx context.Context) (string, error) { return "", nil },
	}
	result := Install(context.Background(), opts, olcrtcRuntime(t))
	if !result.Skipped || !strings.Contains(result.SkipReason, "go toolchain not found") {
		t.Fatalf("expected skip, got %+v", result)
	}
}

func TestInstallCaddyNaiveSkippedWhenNoGo(t *testing.T) {
	old := findBestGoFn
	findBestGoFn = func(minVersion string) (string, error) { return "", nil }
	defer func() { findBestGoFn = old }()

	dir := t.TempDir()
	opts := Options{
		BinDir:   dir,
		Arch:     "amd64",
		EnsureGo: func(ctx context.Context) (string, error) { return "", nil },
	}
	result := Install(context.Background(), opts, caddyRuntime(t))
	if !result.Skipped || !strings.Contains(result.SkipReason, "go toolchain not found") {
		t.Fatalf("expected skip, got %+v", result)
	}
}

func TestInstallGoInstallResolveError(t *testing.T) {
	old := findBestGoFn
	findBestGoFn = func(minVersion string) (string, error) { return "", nil }
	defer func() { findBestGoFn = old }()

	dir := t.TempDir()
	opts := Options{
		BinDir:   dir,
		Arch:     "amd64",
		EnsureGo: func(ctx context.Context) (string, error) { return "", errors.New("ensure failed") },
	}
	result := Install(context.Background(), opts, olcrtcRuntime(t))
	if result.Err == nil || !strings.Contains(result.Err.Error(), "ensure failed") {
		t.Fatalf("expected ensure error, got %v", result.Err)
	}
}

func TestInstallCaddyNaiveResolveError(t *testing.T) {
	old := findBestGoFn
	findBestGoFn = func(minVersion string) (string, error) { return "", nil }
	defer func() { findBestGoFn = old }()

	dir := t.TempDir()
	opts := Options{
		BinDir:   dir,
		Arch:     "amd64",
		EnsureGo: func(ctx context.Context) (string, error) { return "", errors.New("ensure failed") },
	}
	result := Install(context.Background(), opts, caddyRuntime(t))
	if result.Err == nil || !strings.Contains(result.Err.Error(), "ensure failed") {
		t.Fatalf("expected ensure error, got %v", result.Err)
	}
}

func TestWithDefaultsGoInstallNoGo(t *testing.T) {
	old := findBestGoFn
	findBestGoFn = func(minVersion string) (string, error) { return "", nil }
	defer func() { findBestGoFn = old }()

	dir := t.TempDir()
	opts := Options{
		BinDir:        dir,
		CaddyCacheDir: dir,
		EnsureGo:      func(ctx context.Context) (string, error) { return "", nil },
	}.withDefaults()
	err := opts.GoInstall(context.Background(), dir, "example.com/pkg@latest")
	if err == nil || !strings.Contains(err.Error(), "go toolchain not found") {
		t.Fatalf("expected no go error, got %v", err)
	}
}

func TestWithDefaultsBuildCaddyNoGo(t *testing.T) {
	old := findBestGoFn
	findBestGoFn = func(minVersion string) (string, error) { return "", nil }
	defer func() { findBestGoFn = old }()

	dir := t.TempDir()
	opts := Options{
		BinDir:        dir,
		CaddyCacheDir: dir,
		EnsureGo:      func(ctx context.Context) (string, error) { return "", nil },
	}.withDefaults()
	err := opts.BuildCaddy(context.Background(), filepath.Join(dir, "caddy"))
	if err == nil || !strings.Contains(err.Error(), "go toolchain not found") {
		t.Fatalf("expected no go error, got %v", err)
	}
}

func TestWithDefaultsGoInstallEnsureError(t *testing.T) {
	old := findBestGoFn
	findBestGoFn = func(minVersion string) (string, error) { return "", nil }
	defer func() { findBestGoFn = old }()

	dir := t.TempDir()
	opts := Options{
		BinDir:        dir,
		CaddyCacheDir: dir,
		EnsureGo:      func(ctx context.Context) (string, error) { return "", errors.New("ensure failed") },
	}.withDefaults()
	err := opts.GoInstall(context.Background(), dir, "example.com/pkg@latest")
	if err == nil || !strings.Contains(err.Error(), "ensure failed") {
		t.Fatalf("expected ensure error, got %v", err)
	}
}

func TestWithDefaultsBuildCaddyEnsureError(t *testing.T) {
	old := findBestGoFn
	findBestGoFn = func(minVersion string) (string, error) { return "", nil }
	defer func() { findBestGoFn = old }()

	dir := t.TempDir()
	opts := Options{
		BinDir:        dir,
		CaddyCacheDir: dir,
		EnsureGo:      func(ctx context.Context) (string, error) { return "", errors.New("ensure failed") },
	}.withDefaults()
	err := opts.BuildCaddy(context.Background(), filepath.Join(dir, "caddy"))
	if err == nil || !strings.Contains(err.Error(), "ensure failed") {
		t.Fatalf("expected ensure error, got %v", err)
	}
}

func TestRunCaddyNaiveBuildMkdirAllFails(t *testing.T) {
	dir := t.TempDir()
	cacheFile := filepath.Join(dir, "cache-file")
	if err := os.WriteFile(cacheFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runCaddyNaiveBuild(context.Background(), "/bin/true", cacheFile, filepath.Join(dir, "caddy"))
	if err == nil {
		t.Fatal("expected error")
	}
}
