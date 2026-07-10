// Package runtimeinstall acquires and installs the external runtime binaries
// that Veil's managed systemd units invoke. After the plugin-based protocol
// refactor, protocol plugins contribute their own install descriptors via
// RuntimeProvider.RuntimeInstall; this package's Catalog only holds runtimes
// that are not tied to a protocol plugin, such as WARP (sing-box).
//
// Veil install only writes the Panel and the dormant managed unit files; before
// this package, those units pointed at /usr/local/bin/<binary> paths that were
// never created, so every protocol failed to start with systemd status
// 203/EXEC ("Failed to locate executable"). This package fills that gap by
// downloading each runtime from its upstream GitHub release (verifying SHA256
// checksums where the project publishes them) or, for runtimes that ship no
// release binaries, building them from source with `go install`.
package runtimeinstall

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Method describes how a runtime binary is acquired.
type Method string

const (
	// MethodRawBinary downloads a single executable file directly.
	MethodRawBinary Method = "raw-binary"
	// MethodArchive downloads a .tar.gz and extracts a single named binary.
	MethodArchive Method = "archive"
	// MethodGoInstall builds the binary from source with `go install`.
	MethodGoInstall Method = "go-install"
	// MethodCaddyNaive builds Caddy from source with the klzgrad/forwardproxy
	// (naive) fork, which vanilla Caddy releases do not include.
	MethodCaddyNaive Method = "caddy-naive"
)

// Runtime describes one protocol runtime binary Veil installs.
type Runtime struct {
	// Name is the protocol-facing label (e.g. "mieru", "hysteria2").
	Name string
	// Binary is the installed binary filename under the bin directory
	// (e.g. "mita", "hysteria", "sing-box", "caddy", "olcrtc").
	Binary string
	// Method selects the acquisition strategy.
	Method Method
	// Repo is the GitHub "owner/name" for release-based methods.
	Repo string
	// AssetMatch selects the release asset for the current platform.
	AssetMatch func(assetName string) bool
	// ChecksumMatch selects the checksums asset, when the project ships one.
	ChecksumMatch func(assetName string) bool
	// SourcePackage is the Go package path for MethodGoInstall.
	SourcePackage string
}

// Catalog returns the runtime install descriptors for non-plugin runtimes such
// as WARP (sing-box). Protocol plugins supply their own descriptors via
// RuntimeProvider.RuntimeInstall, so they are not duplicated here.
func Catalog(arch string) []Runtime {
	return []Runtime{
		{
			Name:   "warp",
			Binary: "sing-box",
			Method: MethodArchive,
			Repo:   "SagerNet/sing-box",
			AssetMatch: func(name string) bool {
				return strings.HasPrefix(name, "sing-box-") &&
					strings.HasSuffix(name, "-linux-"+arch+".tar.gz") &&
					!strings.Contains(name, "-musl") && !strings.Contains(name, "-glibc")
			},
		},
	}
}

// Release is a subset of the GitHub Releases API response.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset is a single release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Options configures an installation run.
type Options struct {
	// BinDir is where binaries are written (default /usr/local/bin).
	BinDir string
	// CaddyCacheDir is where the Caddy build workspace is cached. Defaults to
	// a directory under the OS cache (e.g. /var/cache/veil or /tmp/veil-runtime).
	CaddyCacheDir string
	// Arch is the normalized architecture ("amd64" or "arm64").
	Arch string
	// HTTPClient performs release-metadata and download requests.
	HTTPClient *http.Client
	// FetchRelease resolves the latest release for a repo. Injectable for tests.
	FetchRelease func(ctx context.Context, repo string) (*Release, error)
	// Download fetches a URL's bytes. Injectable for tests.
	Download func(ctx context.Context, url string) ([]byte, error)
	// GoInstall builds a source package into BinDir. Injectable for tests.
	GoInstall func(ctx context.Context, binDir, sourcePackage string) error
	// BuildCaddy builds a Caddy binary with the naive forwardproxy fork.
	// Injectable for tests.
	BuildCaddy func(ctx context.Context, outPath string) error
	// EnsureGo provisions a Go toolchain if system go is unavailable, returning
	// the path to the go binary. If nil, methods requiring Go are skipped when
	// no system go is found.
	EnsureGo func(ctx context.Context) (string, error)
	// LookPath resolves an executable in PATH. Injectable for tests; defaults
	// to exec.LookPath. Used to detect whether a Go toolchain is present.
	LookPath func(string) (string, error)
	// Now supplies the clock (unused today, reserved for retry/backoff).
	Now func() time.Time
}

// Result records the outcome for a single runtime.
type Result struct {
	Name       string
	Binary     string
	Path       string
	Method     Method
	Version    string
	Installed  bool
	Skipped    bool
	SkipReason string
	Err        error
}

const defaultBinDir = "/usr/local/bin"

// DefaultBinDir is the canonical install directory for runtime binaries.
func DefaultBinDir() string { return defaultBinDir }

func (o Options) withDefaults() Options {
	if o.BinDir == "" {
		o.BinDir = defaultBinDir
	}
	if o.CaddyCacheDir == "" {
		o.CaddyCacheDir = filepath.Join(os.TempDir(), "veil-runtime")
	}
	if o.Arch == "" {
		o.Arch = "amd64"
	}
	if o.HTTPClient == nil {
		o.HTTPClient = &http.Client{Timeout: 120 * time.Second}
	}
	if o.FetchRelease == nil {
		o.FetchRelease = func(ctx context.Context, repo string) (*Release, error) {
			return fetchLatestRelease(ctx, o.HTTPClient, repo)
		}
	}
	if o.Download == nil {
		o.Download = func(ctx context.Context, url string) ([]byte, error) {
			return downloadURL(ctx, o.HTTPClient, url)
		}
	}
	if o.GoInstall == nil {
		o.GoInstall = func(ctx context.Context, binDir, sourcePackage string) error {
			goBin, err := resolveGo(ctx, o.CaddyCacheDir, o.EnsureGo)
			if err != nil {
				return err
			}
			if goBin == "" {
				return fmt.Errorf("go toolchain not found")
			}
			return runGoInstall(ctx, goBin, binDir, sourcePackage)
		}
	}
	if o.BuildCaddy == nil {
		o.BuildCaddy = func(ctx context.Context, outPath string) error {
			goBin, err := resolveGo(ctx, o.CaddyCacheDir, o.EnsureGo)
			if err != nil {
				return err
			}
			if goBin == "" {
				return fmt.Errorf("go toolchain not found")
			}
			return runCaddyNaiveBuild(ctx, goBin, o.CaddyCacheDir, outPath)
		}
	}
	if o.EnsureGo == nil {
		tc := NewGoToolchain(o.CaddyCacheDir)
		o.EnsureGo = tc.Ensure
	}
	if o.LookPath == nil {
		o.LookPath = exec.LookPath
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// InstallAll installs every runtime in the catalog and returns per-runtime
// results. It does not stop at the first failure: each runtime is independent,
// so a single broken upstream release should not block the others.
func InstallAll(ctx context.Context, opts Options) []Result {
	opts = opts.withDefaults()
	return installRuntimes(ctx, opts, Catalog(opts.Arch))
}

func installRuntimes(ctx context.Context, opts Options, runtimes []Runtime) []Result {
	results := make([]Result, 0, len(runtimes))
	for _, runtime := range runtimes {
		results = append(results, Install(ctx, opts, runtime))
	}
	return results
}

// Install acquires and installs a single runtime binary.
func Install(ctx context.Context, opts Options, runtime Runtime) Result {
	opts = opts.withDefaults()
	result := Result{Name: runtime.Name, Binary: runtime.Binary, Method: runtime.Method}
	if err := os.MkdirAll(opts.BinDir, 0o755); err != nil {
		result.Err = fmt.Errorf("create bin dir: %w", err)
		return result
	}
	switch runtime.Method {
	case MethodRawBinary:
		path, version, err := installFromRelease(ctx, opts, runtime, false)
		result.Path, result.Version, result.Err = path, version, err
	case MethodArchive:
		path, version, err := installFromRelease(ctx, opts, runtime, true)
		result.Path, result.Version, result.Err = path, version, err
	case MethodGoInstall:
		goBin, err := resolveGo(ctx, opts.CaddyCacheDir, opts.EnsureGo)
		if err != nil {
			result.Err = err
			return result
		}
		if goBin == "" {
			result.Skipped = true
			result.SkipReason = "go toolchain not found; install Go to build olcrtc from source, then run: veil runtime install --only olcrtc"
			return result
		}
		path, err := installFromSource(ctx, opts, runtime)
		result.Path, result.Err = path, err
	case MethodCaddyNaive:
		goBin, err := resolveGo(ctx, opts.CaddyCacheDir, opts.EnsureGo)
		if err != nil {
			result.Err = err
			return result
		}
		if goBin == "" {
			result.Skipped = true
			result.SkipReason = "go toolchain not found; install Go to build Caddy with forward_proxy, then run: veil runtime install --only naiveproxy"
			return result
		}
		path, err := installCaddyNaive(ctx, opts, runtime)
		result.Path, result.Err = path, err
	default:
		result.Err = fmt.Errorf("unsupported method %q", runtime.Method)
	}
	result.Installed = result.Err == nil
	return result
}

func installFromRelease(ctx context.Context, opts Options, runtime Runtime, archive bool) (string, string, error) {
	release, err := opts.FetchRelease(ctx, runtime.Repo)
	if err != nil {
		return "", "", fmt.Errorf("resolve %s release: %w", runtime.Repo, err)
	}
	asset, ok := findAsset(release.Assets, runtime.AssetMatch)
	if !ok {
		return "", "", fmt.Errorf("release %s has no asset for linux/%s", release.TagName, opts.Arch)
	}
	body, err := opts.Download(ctx, asset.BrowserDownloadURL)
	if err != nil {
		return "", "", fmt.Errorf("download %s: %w", asset.Name, err)
	}
	if runtime.ChecksumMatch != nil {
		checksumAsset, ok := findAsset(release.Assets, runtime.ChecksumMatch)
		if ok {
			checksums, err := opts.Download(ctx, checksumAsset.BrowserDownloadURL)
			if err != nil {
				return "", "", fmt.Errorf("download checksums: %w", err)
			}
			if err := VerifyChecksum(body, asset.Name, runtime.Binary, checksums); err != nil {
				return "", "", err
			}
		}
	}
	payload := body
	if archive {
		payload, err = ExtractArchiveBinary(body, runtime.Binary)
		if err != nil {
			return "", "", err
		}
	}
	path := filepath.Join(opts.BinDir, runtime.Binary)
	if err := writeBinaryAtomic(path, payload); err != nil {
		return "", "", err
	}
	return path, release.TagName, nil
}

func installFromSource(ctx context.Context, opts Options, runtime Runtime) (string, error) {
	if err := opts.GoInstall(ctx, opts.BinDir, runtime.SourcePackage); err != nil {
		return "", err
	}
	return filepath.Join(opts.BinDir, runtime.Binary), nil
}

func installCaddyNaive(ctx context.Context, opts Options, runtime Runtime) (string, error) {
	path := filepath.Join(opts.BinDir, runtime.Binary)
	if err := opts.BuildCaddy(ctx, path); err != nil {
		return "", err
	}
	return path, nil
}

// runCaddyNaiveBuild builds a Caddy binary with the klzgrad/forwardproxy
// (naive) fork. It creates a self-contained Go module in cacheDir/build-caddy,
// pins Caddy v2.10.0 and the naive forwardproxy fork via a replace directive,
// and compiles a static binary to outPath.
func runCaddyNaiveBuild(ctx context.Context, goBin, cacheDir, outPath string) error {
	if resolved, err := exec.LookPath(goBin); err == nil {
		goBin = resolved
	}
	buildDir := filepath.Join(cacheDir, "build-caddy")
	// Ensure a clean build dir so the build is idempotent: a leftover
	// go.mod from a previous run makes `go mod init` fail, breaking
	// re-runs of `veil runtime install`.
	if err := os.RemoveAll(buildDir); err != nil {
		return fmt.Errorf("clean caddy build dir: %w", err)
	}
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return fmt.Errorf("create caddy build dir: %w", err)
	}

	// Write main.go — imports caddy and the naive forwardproxy module.
	mainGo := filepath.Join(buildDir, "main.go")
	if err := os.WriteFile(mainGo, []byte(`package main

import (
	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	_ "github.com/caddyserver/forwardproxy"
)

func main() {
	caddycmd.Main()
}
`), 0o644); err != nil {
		return fmt.Errorf("write main.go: %w", err)
	}

	// Initialize module, pin caddy and the naive forwardproxy fork.
	cmds := [][]string{
		{goBin, "mod", "init", "caddy"},
		{goBin, "mod", "edit", "-require", "github.com/caddyserver/caddy/v2@v2.11.4"},
		{goBin, "mod", "edit", "-replace", "github.com/caddyserver/forwardproxy=github.com/klzgrad/forwardproxy@d62c80d3dd2c706b6b87579844d2397bddd18317"},
		{goBin, "mod", "tidy"},
	}
	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = buildDir
		cmd.Env = goBuildEnv(goBin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
		}
	}

	// Build the binary.
	buildArgs := []string{goBin, "build", "-o", outPath, "-ldflags=-s -w", "-trimpath", "."}
	cmd := exec.CommandContext(ctx, buildArgs[0], buildArgs[1:]...)
	cmd.Dir = buildDir
	cmd.Env = append(goBuildEnv(goBin), "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("caddy build: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// atomicFile is the subset of *os.File used by writeBinaryAtomic. It is
// implemented by *os.File in production and by test doubles in tests.
type atomicFile interface {
	io.Writer
	Name() string
	Chmod(mode os.FileMode) error
	Close() error
}

// writeBinaryAtomicCreateTemp is swapped in tests to exercise error paths.
var writeBinaryAtomicCreateTemp = func(dir, pattern string) (atomicFile, error) {
	return os.CreateTemp(dir, pattern)
}

func writeBinaryAtomic(path string, body []byte) error {
	dir := filepath.Dir(path)
	tmp, err := writeBinaryAtomicCreateTemp(dir, ".veil-runtime-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// findAsset returns the first asset accepted by match.
func findAsset(assets []Asset, match func(string) bool) (Asset, bool) {
	if match == nil {
		return Asset{}, false
	}
	candidates := make([]Asset, 0, len(assets))
	for _, asset := range assets {
		if match(asset.Name) {
			candidates = append(candidates, asset)
		}
	}
	if len(candidates) == 0 {
		return Asset{}, false
	}
	// Deterministic selection: shortest name wins, then lexical. This favors the
	// plain asset (e.g. "sing-box-...-linux-amd64.tar.gz") over decorated
	// variants when the matcher is intentionally permissive.
	sort.Slice(candidates, func(i, j int) bool {
		if len(candidates[i].Name) != len(candidates[j].Name) {
			return len(candidates[i].Name) < len(candidates[j].Name)
		}
		return candidates[i].Name < candidates[j].Name
	})
	return candidates[0], true
}

// ExtractArchiveBinary returns the named binary's bytes from a .tar.gz archive,
// matching on the base filename so archives that nest the binary in a versioned
// directory (sing-box) and those that don't (mieru) both work.
func ExtractArchiveBinary(body []byte, binary string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) == binary {
			const maxBinary = 200 * 1024 * 1024
			return io.ReadAll(io.LimitReader(tr, maxBinary))
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", binary)
}

// VerifyChecksum confirms body's checksum matches the value recorded for the
// asset. Upstream checksum files reference either the asset filename (mieru,
// caddy) or a build path ending in the binary name (hysteria's hashes.txt), so
// both forms are accepted. The digest algorithm is selected by the recorded
// hex length: 64 chars -> SHA-256 (mieru, hysteria), 128 chars -> SHA-512
// (caddy).
func VerifyChecksum(body []byte, assetName, binary string, checksums []byte) error {
	want := extractChecksum(string(checksums), assetName, binary)
	if want == "" {
		return fmt.Errorf("no checksum recorded for %s", assetName)
	}
	var got string
	switch len(want) {
	case 64:
		sum := sha256.Sum256(body)
		got = hex.EncodeToString(sum[:])
	case 128:
		sum := sha512.Sum512(body)
		got = hex.EncodeToString(sum[:])
	default:
		return fmt.Errorf("unsupported checksum length %d for %s", len(want), assetName)
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", assetName, want, got)
	}
	return nil
}

func extractChecksum(checksums, assetName, binary string) string {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		hash := fields[0]
		file := fields[len(fields)-1]
		base := file
		if idx := strings.LastIndexAny(file, "/\\"); idx != -1 {
			base = file[idx+1:]
		}
		if file == assetName || base == assetName || (binary != "" && base == binary) {
			return hash
		}
	}
	return ""
}

func fetchLatestRelease(ctx context.Context, client *http.Client, repo string) (*Release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "veil")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			return fetchLatestReleaseWeb(ctx, client, repo)
		}
		return nil, fmt.Errorf("GitHub API %s: %s", repo, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	return parseRelease(body)
}

// fetchLatestReleaseWeb is a fallback for GitHub API rate limits. It follows
// the /releases/latest redirect to discover the tag, then scrapes the
// expanded_assets page for asset names and download URLs.
func fetchLatestReleaseWeb(ctx context.Context, client *http.Client, repo string) (*Release, error) {
	tag, err := resolveLatestTagWeb(ctx, client, repo)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://github.com/%s/releases/expanded_assets/%s", repo, url.PathEscape(tag))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "veil")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub web %s expanded assets: %s", repo, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	assets, err := parseExpandedAssets(string(body), tag, repo)
	if err != nil {
		return nil, err
	}
	return &Release{TagName: tag, Assets: assets}, nil
}

// resolveLatestTagWeb follows the GitHub /releases/latest redirect and returns
// the URL-encoded tag name (e.g. "app%2Fv2.9.2" or "v3.34.0").
func resolveLatestTagWeb(ctx context.Context, client *http.Client, repo string) (string, error) {
	latestURL := fmt.Sprintf("https://github.com/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, latestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "veil")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" && resp.Request != nil && resp.Request.URL != nil {
		loc = resp.Request.URL.String()
	}
	if loc == "" {
		return "", fmt.Errorf("GitHub web %s latest: no redirect location", repo)
	}
	locURL, err := url.Parse(loc)
	if err != nil {
		return "", fmt.Errorf("GitHub web %s latest: parse location %q: %w", repo, loc, err)
	}
	parts := strings.Split(strings.Trim(locURL.Path, "/"), "/")
	// Expected path: owner/repo/releases/tag/<tag>
	if len(parts) < 5 || parts[2] != "releases" || parts[3] != "tag" {
		return "", fmt.Errorf("GitHub web %s latest: unexpected location path %q", repo, locURL.Path)
	}
	// The tag is everything after /tag/; join it back in case the tag contains slashes.
	return path.Join(parts[4:]...), nil
}

// parseExpandedAssets extracts asset names and download URLs from the GitHub
// expanded_assets HTML page. Asset paths are relative ("/owner/repo/releases/...").
func parseExpandedAssets(html, tag, repo string) ([]Asset, error) {
	// Match hrefs that point at a release download for this tag.
	// Example: href="/apernet/hysteria/releases/download/app%2Fv2.9.2/hysteria-linux-amd64"
	pattern := fmt.Sprintf(`href="/(%s/releases/download/%s/([^"]+))"`, regexp.QuoteMeta(repo), regexp.QuoteMeta(url.PathEscape(tag)))
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	var assets []Asset
	for _, m := range re.FindAllStringSubmatch(html, -1) {
		name := m[2]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		assets = append(assets, Asset{
			Name:               name,
			BrowserDownloadURL: "https://github.com/" + m[1],
		})
	}
	if len(assets) == 0 {
		return nil, fmt.Errorf("GitHub web %s release %s: no assets found", repo, tag)
	}
	return assets, nil
}

func downloadURL(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "veil")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	const maxDownload = 200 * 1024 * 1024
	return io.ReadAll(io.LimitReader(resp.Body, maxDownload))
}

func runGoInstall(ctx context.Context, goBin, binDir, sourcePackage string) error {
	cmd := exec.CommandContext(ctx, goBin, "install", sourcePackage)
	cmd.Env = append(goBuildEnv(goBin), "GOBIN="+binDir, "CGO_ENABLED=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(out))
		if trimmed != "" {
			return fmt.Errorf("go install %s: %s: %w", sourcePackage, trimmed, err)
		}
		return fmt.Errorf("go install %s: %w", sourcePackage, err)
	}
	return nil
}
