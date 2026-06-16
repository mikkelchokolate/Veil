package runtimeinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func makeTarGz(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestCatalogCoversEveryProtocolRuntime(t *testing.T) {
	runtimes := Catalog("amd64")
	wantBinaries := map[string]string{
		"naiveproxy": "caddy",
		"hysteria2":  "hysteria",
		"mieru":      "mita",
		"warp":       "sing-box",
		"olcrtc":     "olcrtc",
	}
	got := map[string]string{}
	for _, r := range runtimes {
		got[r.Name] = r.Binary
	}
	for name, binary := range wantBinaries {
		if got[name] != binary {
			t.Fatalf("catalog missing %s -> %s, got %q", name, binary, got[name])
		}
	}
}

func TestCatalogAssetMatchersMatchUpstreamNames(t *testing.T) {
	byBinary := map[string]Runtime{}
	for _, r := range Catalog("amd64") {
		byBinary[r.Binary] = r
	}
	cases := []struct {
		binary string
		asset  string
	}{
		{"hysteria", "hysteria-linux-amd64"},
		{"mita", "mita_3.34.0_linux_amd64.tar.gz"},
		{"sing-box", "sing-box-1.13.13-linux-amd64.tar.gz"},
	}
	for _, c := range cases {
		r := byBinary[c.binary]
		if r.AssetMatch == nil || !r.AssetMatch(c.asset) {
			t.Fatalf("%s asset matcher did not match %q", c.binary, c.asset)
		}
	}
}

func TestInstallCaddyNaiveInvokesBuilder(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")

	var caddyNaive Runtime
	for _, r := range Catalog("amd64") {
		if r.Binary == "caddy" {
			caddyNaive = r
		}
	}
	if caddyNaive.Method != MethodCaddyNaive {
		t.Fatalf("expected naiveproxy method=caddy-naive, got %s", caddyNaive.Method)
	}

	var gotOutPath string
	opts := Options{
		BinDir: binDir,
		Arch:   "amd64",
		BuildCaddy: func(ctx context.Context, outPath string) error {
			gotOutPath = outPath
			return os.WriteFile(outPath, []byte("caddy-with-forwardproxy"), 0o755)
		},
	}

	result := Install(context.Background(), opts, caddyNaive)
	if result.Err != nil {
		t.Fatalf("Install: %v", result.Err)
	}
	wantPath := filepath.Join(binDir, "caddy")
	if gotOutPath != wantPath {
		t.Fatalf("BuildCaddy outPath = %q, want %q", gotOutPath, wantPath)
	}
	body, _ := os.ReadFile(wantPath)
	if string(body) != "caddy-with-forwardproxy" {
		t.Fatalf("installed binary = %q", string(body))
	}
}

func TestSingBoxMatcherRejectsMuslAndGlibcVariants(t *testing.T) {
	var sb Runtime
	for _, r := range Catalog("amd64") {
		if r.Binary == "sing-box" {
			sb = r
		}
	}
	for _, name := range []string{
		"sing-box-1.13.13-linux-amd64-musl.tar.gz",
		"sing-box-1.13.13-linux-amd64-glibc.tar.gz",
	} {
		if sb.AssetMatch(name) {
			t.Fatalf("sing-box matcher should reject variant %q", name)
		}
	}
	if !sb.AssetMatch("sing-box-1.13.13-linux-amd64.tar.gz") {
		t.Fatalf("sing-box matcher should accept the plain asset")
	}
}

func TestVerifyChecksumAcceptsAssetNameForm(t *testing.T) {
	body := []byte("mita-binary-archive")
	checksums := fmt.Sprintf("%s  mita_3.34.0_linux_amd64.tar.gz\n", sha256Hex(body))
	if err := VerifyChecksum(body, "mita_3.34.0_linux_amd64.tar.gz", "mita", []byte(checksums)); err != nil {
		t.Fatalf("VerifyChecksum: %v", err)
	}
}

func TestVerifyChecksumAcceptsBuildPathForm(t *testing.T) {
	body := []byte("hysteria-binary")
	// hysteria hashes.txt records "build/hysteria-linux-amd64"
	checksums := fmt.Sprintf("%s  build/hysteria-linux-amd64\n", sha256Hex(body))
	if err := VerifyChecksum(body, "hysteria-linux-amd64", "hysteria", []byte(checksums)); err != nil {
		t.Fatalf("VerifyChecksum build-path form: %v", err)
	}
}

func TestVerifyChecksumRejectsMismatch(t *testing.T) {
	checksums := fmt.Sprintf("%s  asset.tar.gz\n", sha256Hex([]byte("expected")))
	if err := VerifyChecksum([]byte("actual"), "asset.tar.gz", "bin", []byte(checksums)); err == nil {
		t.Fatalf("expected checksum mismatch error")
	}
}

func TestVerifyChecksumMissingEntry(t *testing.T) {
	if err := VerifyChecksum([]byte("x"), "asset.tar.gz", "bin", []byte("deadbeef  other.tar.gz\n")); err == nil {
		t.Fatalf("expected missing-checksum error")
	}
}

func TestExtractArchiveBinaryNested(t *testing.T) {
	archive := makeTarGz(t, map[string][]byte{
		"sing-box-1.13.13-linux-amd64/LICENSE":  []byte("license"),
		"sing-box-1.13.13-linux-amd64/sing-box": []byte("sing-box-bin"),
	})
	got, err := ExtractArchiveBinary(archive, "sing-box")
	if err != nil {
		t.Fatalf("ExtractArchiveBinary: %v", err)
	}
	if string(got) != "sing-box-bin" {
		t.Fatalf("got %q", string(got))
	}
}

func TestExtractArchiveBinaryFlat(t *testing.T) {
	archive := makeTarGz(t, map[string][]byte{"mita": []byte("mita-bin")})
	got, err := ExtractArchiveBinary(archive, "mita")
	if err != nil {
		t.Fatalf("ExtractArchiveBinary: %v", err)
	}
	if string(got) != "mita-bin" {
		t.Fatalf("got %q", string(got))
	}
}

func TestExtractArchiveBinaryNotFound(t *testing.T) {
	archive := makeTarGz(t, map[string][]byte{"README": []byte("x")})
	if _, err := ExtractArchiveBinary(archive, "mita"); err == nil {
		t.Fatalf("expected not-found error")
	}
}

func TestFindAssetPrefersShortestName(t *testing.T) {
	assets := []Asset{
		{Name: "sing-box-1.13.13-linux-amd64.tar.gz.sig"},
		{Name: "sing-box-1.13.13-linux-amd64.tar.gz"},
	}
	got, ok := findAsset(assets, func(n string) bool {
		return len(n) > 0 && n[:8] == "sing-box"
	})
	if !ok || got.Name != "sing-box-1.13.13-linux-amd64.tar.gz" {
		t.Fatalf("findAsset = %+v ok=%v", got, ok)
	}
}

func TestInstallArchiveRuntimeWritesExecutableBinary(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	mitaBinary := []byte("mita-executable")
	archive := makeTarGz(t, map[string][]byte{"mita": mitaBinary})
	checksums := fmt.Sprintf("%s  mita_3.34.0_linux_amd64.tar.gz\n", sha256Hex(archive))

	mieru := Runtime{}
	for _, r := range Catalog("amd64") {
		if r.Binary == "mita" {
			mieru = r
		}
	}

	opts := Options{
		BinDir: binDir,
		Arch:   "amd64",
		FetchRelease: func(ctx context.Context, repo string) (*Release, error) {
			return &Release{TagName: "v3.34.0", Assets: []Asset{
				{Name: "mita_3.34.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example/mita.tar.gz"},
				{Name: "mita_3.34.0_linux_amd64.tar.gz.sha256.txt", BrowserDownloadURL: "https://example/mita.sha256"},
			}}, nil
		},
		Download: func(ctx context.Context, url string) ([]byte, error) {
			switch url {
			case "https://example/mita.tar.gz":
				return archive, nil
			case "https://example/mita.sha256":
				return []byte(checksums), nil
			}
			return nil, fmt.Errorf("unexpected url %s", url)
		},
	}

	result := Install(context.Background(), opts, mieru)
	if result.Err != nil {
		t.Fatalf("Install: %v", result.Err)
	}
	path := filepath.Join(binDir, "mita")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat installed binary: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("installed binary should be executable, mode=%v", info.Mode())
	}
	body, _ := os.ReadFile(path)
	if string(body) != string(mitaBinary) {
		t.Fatalf("installed binary content = %q", string(body))
	}
}

func TestInstallRawBinaryRuntimeVerifiesChecksum(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	hysteriaBinary := []byte("hysteria-executable")
	checksums := fmt.Sprintf("%s  build/hysteria-linux-amd64\n", sha256Hex(hysteriaBinary))

	var hysteria Runtime
	for _, r := range Catalog("amd64") {
		if r.Binary == "hysteria" {
			hysteria = r
		}
	}

	opts := Options{
		BinDir: binDir,
		Arch:   "amd64",
		FetchRelease: func(ctx context.Context, repo string) (*Release, error) {
			return &Release{TagName: "app/v2.9.2", Assets: []Asset{
				{Name: "hysteria-linux-amd64", BrowserDownloadURL: "https://example/hy"},
				{Name: "hashes.txt", BrowserDownloadURL: "https://example/hashes"},
			}}, nil
		},
		Download: func(ctx context.Context, url string) ([]byte, error) {
			switch url {
			case "https://example/hy":
				return hysteriaBinary, nil
			case "https://example/hashes":
				return []byte(checksums), nil
			}
			return nil, fmt.Errorf("unexpected url %s", url)
		},
	}

	result := Install(context.Background(), opts, hysteria)
	if result.Err != nil {
		t.Fatalf("Install: %v", result.Err)
	}
	body, _ := os.ReadFile(filepath.Join(binDir, "hysteria"))
	if string(body) != string(hysteriaBinary) {
		t.Fatalf("installed binary content = %q", string(body))
	}
}

func TestInstallRawBinaryFailsOnChecksumMismatch(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")

	var hysteria Runtime
	for _, r := range Catalog("amd64") {
		if r.Binary == "hysteria" {
			hysteria = r
		}
	}

	opts := Options{
		BinDir: binDir,
		Arch:   "amd64",
		FetchRelease: func(ctx context.Context, repo string) (*Release, error) {
			return &Release{TagName: "app/v2.9.2", Assets: []Asset{
				{Name: "hysteria-linux-amd64", BrowserDownloadURL: "https://example/hy"},
				{Name: "hashes.txt", BrowserDownloadURL: "https://example/hashes"},
			}}, nil
		},
		Download: func(ctx context.Context, url string) ([]byte, error) {
			switch url {
			case "https://example/hy":
				return []byte("tampered"), nil
			case "https://example/hashes":
				return []byte(sha256Hex([]byte("original")) + "  build/hysteria-linux-amd64\n"), nil
			}
			return nil, fmt.Errorf("unexpected url %s", url)
		},
	}

	result := Install(context.Background(), opts, hysteria)
	if result.Err == nil {
		t.Fatalf("expected checksum mismatch to fail install")
	}
	if _, err := os.Stat(filepath.Join(binDir, "hysteria")); !os.IsNotExist(err) {
		t.Fatalf("binary should not be written on checksum failure")
	}
}

func TestInstallGoInstallRuntimeInvokesBuilder(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")

	var olcrtc Runtime
	for _, r := range Catalog("amd64") {
		if r.Binary == "olcrtc" {
			olcrtc = r
		}
	}

	var gotPackage, gotBinDir string
	opts := Options{
		BinDir: binDir,
		Arch:   "amd64",
		GoInstall: func(ctx context.Context, binDirArg, sourcePackage string) error {
			gotBinDir = binDirArg
			gotPackage = sourcePackage
			return os.WriteFile(filepath.Join(binDirArg, "olcrtc"), []byte("built"), 0o755)
		},
	}

	result := Install(context.Background(), opts, olcrtc)
	if result.Err != nil {
		t.Fatalf("Install: %v", result.Err)
	}
	if gotBinDir != binDir {
		t.Fatalf("go install bin dir = %q", gotBinDir)
	}
	if gotPackage != "github.com/openlibrecommunity/olcrtc/cmd/olcrtc@latest" {
		t.Fatalf("go install package = %q", gotPackage)
	}
}

func TestInstallAllContinuesPastFailures(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	opts := Options{
		BinDir: binDir,
		Arch:   "amd64",
		FetchRelease: func(ctx context.Context, repo string) (*Release, error) {
			return nil, fmt.Errorf("release unavailable")
		},
		GoInstall: func(ctx context.Context, binDir, sourcePackage string) error {
			return os.WriteFile(filepath.Join(binDir, "olcrtc"), []byte("built"), 0o755)
		},
		BuildCaddy: func(ctx context.Context, outPath string) error {
			return os.WriteFile(outPath, []byte("caddy-built"), 0o755)
		},
		EnsureGo: func(ctx context.Context) (string, error) { return "/usr/local/go/bin/go", nil },
	}
	results := InstallAll(context.Background(), opts)
	if len(results) != len(Catalog("amd64")) {
		t.Fatalf("expected one result per runtime, got %d", len(results))
	}
	var builtOK, releaseFailures int
	for _, r := range results {
		if (r.Method == MethodGoInstall || r.Method == MethodCaddyNaive) && r.Installed {
			builtOK++
		}
		if r.Method != MethodGoInstall && r.Method != MethodCaddyNaive && r.Err != nil {
			releaseFailures++
		}
	}
	if builtOK != 2 {
		t.Fatalf("expected olcrtc + caddy builds to succeed (2), got %d", builtOK)
	}
	if releaseFailures != 3 {
		t.Fatalf("expected 3 release-based runtimes to fail, got %d", releaseFailures)
	}
}
