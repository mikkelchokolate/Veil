package runtimeinstall

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultGoVersion = "1.26.4"

var defaultGoSHA256 = map[string]string{
	"linux-amd64": "1153d3d50e0ac764b447adfe05c2bcf08e889d42a02e0fe0259bd47f6733ad7f",
	"linux-arm64": "ef758ae7c6cf9267c9c0ef080b8965f453d89ab2d25d9eb22de4405925238768",
}

// goVersionRE extracts "1.26.4" from strings like "go version go1.26.4 linux/amd64"
// or a plain "1.26.4" version argument.
var goVersionRE = regexp.MustCompile(`(?:^|[^0-9.])(\d+)\.(\d+)(?:\.(\d+))?`)

type goVersion struct {
	Major, Minor, Patch int
}

func parseGoVersion(s string) (goVersion, bool) {
	m := goVersionRE.FindStringSubmatch(s)
	if m == nil {
		return goVersion{}, false
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch := 0
	if m[3] != "" {
		patch, _ = strconv.Atoi(m[3])
	}
	return goVersion{Major: major, Minor: minor, Patch: patch}, true
}

func (v goVersion) atLeast(other goVersion) bool {
	if v.Major != other.Major {
		return v.Major > other.Major
	}
	if v.Minor != other.Minor {
		return v.Minor > other.Minor
	}
	return v.Patch >= other.Patch
}

// knownGoBins returns a list of likely Go binary paths, including the one on
// PATH, common installation prefixes, and versioned /usr/lib/go-* packages.
func knownGoBins() []string {
	seen := map[string]struct{}{}
	candidates := []string{}
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		candidates = append(candidates, p)
	}
	if p, err := exec.LookPath("go"); err == nil {
		add(p)
	}
	add("/usr/local/go/bin/go")
	add("/opt/go/bin/go")
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, "go", "bin", "go"))
	}
	if goroot := os.Getenv("GOROOT"); goroot != "" {
		add(filepath.Join(goroot, "bin", "go"))
	}
	matches, _ := filepath.Glob("/usr/lib/go-*/bin/go")
	for _, m := range matches {
		add(m)
	}
	return candidates
}

// findBestGo searches common Go installation locations and returns the newest
// binary whose version is at least minVersion. If none qualifies, it returns
// ("", nil) so the caller can provision a toolchain.
func findBestGo(minVersion string) (string, error) {
	want, ok := parseGoVersion(minVersion)
	if !ok {
		return "", fmt.Errorf("invalid minimum Go version %q", minVersion)
	}
	type found struct {
		path    string
		version goVersion
	}
	var founds []found
	for _, candidate := range knownGoBins() {
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		// Probe with a clean environment: a parent Go 1.26+ process may have
		// set GOROOT to its own toolchain path (via go.mod toolchain
		// auto-download), which would make a 1.22 system go binary report the
		// newer version while still running 1.22 code. That breaks version
		// selection and leads to "requires go >= 1.25 (running go 1.22)" errors.
		cmd := exec.Command(candidate, "version")
		cmd.Env = goLocalEnv()
		out, err := cmd.CombinedOutput()
		if err != nil {
			continue
		}
		v, ok := parseGoVersion(string(out))
		if !ok {
			continue
		}
		founds = append(founds, found{path: candidate, version: v})
	}
	if len(founds) == 0 {
		return "", nil
	}
	sort.Slice(founds, func(i, j int) bool {
		vi, vj := founds[i].version, founds[j].version
		if vi.Major != vj.Major {
			return vi.Major > vj.Major
		}
		if vi.Minor != vj.Minor {
			return vi.Minor > vj.Minor
		}
		return vi.Patch > vj.Patch
	})
	best := founds[0]
	if !best.version.atLeast(want) {
		return "", nil
	}
	return best.path, nil
}

// goLocalEnv returns the current environment with Go-specific variables
// stripped and GOTOOLCHAIN=local appended. This prevents a parent Go process
// (or a go.mod in the current working directory) from auto-switching to a
// different toolchain, which would make an older system go binary report a
// newer version while still running older code.
func goLocalEnv() []string {
	var out []string
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GOROOT=") || strings.HasPrefix(e, "GOPATH=") ||
			strings.HasPrefix(e, "GOMODCACHE=") || strings.HasPrefix(e, "GOBIN=") ||
			strings.HasPrefix(e, "GOCACHE=") || strings.HasPrefix(e, "GOENV=") ||
			strings.HasPrefix(e, "GOFLAGS=") || strings.HasPrefix(e, "GONOSUMDB=") ||
			strings.HasPrefix(e, "GOSUMDB=") || strings.HasPrefix(e, "GOPRIVATE=") ||
			strings.HasPrefix(e, "GONOSUMCHECK=") || strings.HasPrefix(e, "GOPROXY=") ||
			strings.HasPrefix(e, "GOTOOLCHAIN=") {
			continue
		}
		out = append(out, e)
	}
	out = append(out, "GOTOOLCHAIN=local")
	return out
}

// GoToolchain manages a self-contained Go installation cached under CacheDir/go.
type GoToolchain struct {
	CacheDir string
	Version  string
	client   *http.Client
}

func NewGoToolchain(cacheDir string) *GoToolchain {
	return &GoToolchain{
		CacheDir: cacheDir,
		Version:  defaultGoVersion,
		client:   &http.Client{Timeout: 120 * time.Second},
	}
}

// Ensure returns the path to the go binary, downloading and installing it into
// the cache if necessary.
func (gt *GoToolchain) Ensure(ctx context.Context) (string, error) {
	goDir := filepath.Join(gt.CacheDir, "go"+gt.Version)
	goBin := filepath.Join(goDir, "bin", "go")
	if runtime.GOOS == "windows" {
		goBin += ".exe"
	}
	if _, err := os.Stat(goBin); err == nil {
		return goBin, nil
	}
	if err := os.MkdirAll(filepath.Dir(goDir), 0o755); err != nil {
		return "", err
	}
	if err := gt.downloadAndExtract(ctx, goDir); err != nil {
		return "", err
	}
	return goBin, nil
}

func (gt *GoToolchain) downloadAndExtract(ctx context.Context, goDir string) error {
	platform := runtime.GOOS + "-" + runtime.GOARCH
	wantHash, ok := defaultGoSHA256[platform]
	if !ok {
		return fmt.Errorf("no prebuilt Go %s for %s", gt.Version, platform)
	}

	url := fmt.Sprintf("https://go.dev/dl/go%s.%s.tar.gz", gt.Version, platform)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := gt.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download Go: HTTP %s", resp.Status)
	}

	tmpDir := filepath.Join(filepath.Dir(goDir), ".tmp-"+gt.Version)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tmpFile := filepath.Join(tmpDir, "go.tar.gz")
	f, err := os.Create(tmpFile)
	if err != nil {
		return err
	}
	defer f.Close()

	hasher := sha256.New()
	tee := io.TeeReader(resp.Body, hasher)
	if _, err := io.Copy(f, tee); err != nil {
		f.Close()
		return fmt.Errorf("download Go: %w", err)
	}
	f.Close()

	if hex.EncodeToString(hasher.Sum(nil)) != wantHash {
		return fmt.Errorf("go download checksum mismatch")
	}

	f2, err := os.Open(tmpFile)
	if err != nil {
		return err
	}
	defer f2.Close()

	gz, err := gzip.NewReader(f2)
	if err != nil {
		return fmt.Errorf("gzip Go: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar Go: %w", err)
		}
		rel := strings.TrimPrefix(hdr.Name, "go/")
		if rel == "" {
			continue
		}
		rel = filepath.ToSlash(rel)
		if !filepath.IsLocal(rel) {
			return fmt.Errorf("tar Go: invalid path %q", hdr.Name)
		}
		target := filepath.Join(goDir, rel)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, io.LimitReader(tr, hdr.Size)); err != nil {
				out.Close()
				return err
			}
			out.Close()
		}
	}
	return nil
}

// resolveGo returns the path to a go binary that satisfies the pinned Go
// version requirement. It first checks common installation locations, then
// provisions a toolchain into CacheDir if no suitable system Go is found. It
// returns ("", nil) when no toolchain is available and provisioning is
// disabled (EnsureGo nil).
func resolveGo(ctx context.Context, cacheDir string, ensureGo func(context.Context) (string, error)) (string, error) {
	if p, err := findBestGo(defaultGoVersion); err != nil {
		return "", err
	} else if p != "" {
		return p, nil
	}
	if ensureGo != nil {
		return ensureGo(ctx)
	}
	return "", nil
}

// goBuildEnv returns the environment for running a go build with the given go
// binary. Sets GOROOT to the directory two levels above the go binary (the
// standard layout), and ensures the binary's directory is in PATH.
func goBuildEnv(goBin string) []string {
	env := os.Environ()
	goroot := filepath.Dir(filepath.Dir(goBin))
	goBinDir := filepath.Dir(goBin)
	filtered := make([]string, 0, len(env)+5)
	hasGoroot := false
	hasGobin := false
	hasPath := false
	hasGonosumdb := false
	hasGoproxy := false
	preservePrefixes := []string{"GONOSUMDB=", "GOSUMDB=", "GOPRIVATE=", "GONOSUMCHECK=", "GOFLAGS=", "GOPROXY=", "GOMODCACHE="}
	for _, e := range env {
		preserved := false
		for _, pfx := range preservePrefixes {
			if strings.HasPrefix(e, pfx) {
				filtered = append(filtered, e)
				if pfx == "GONOSUMDB=" {
					hasGonosumdb = true
				}
				if pfx == "GOPROXY=" {
					hasGoproxy = true
				}
				preserved = true
				break
			}
		}
		if preserved {
			continue
		}
		switch {
		case strings.HasPrefix(e, "GOROOT="):
			filtered = append(filtered, "GOROOT="+goroot)
			hasGoroot = true
		case strings.HasPrefix(e, "GOBIN="):
			filtered = append(filtered, "GOBIN="+goBinDir)
			hasGobin = true
		case strings.HasPrefix(e, "PATH=") || strings.HasPrefix(e, "Path="):
			filtered = append(filtered, e+":"+goBinDir)
			hasPath = true
		case strings.HasPrefix(e, "GOTOOLCHAIN="):
			// Ignore inherited toolchain policy; we explicitly pin below.
		default:
			filtered = append(filtered, e)
		}
	}
	if !hasGoroot {
		filtered = append(filtered, "GOROOT="+goroot)
	}
	if !hasGobin {
		filtered = append(filtered, "GOBIN="+goBinDir)
	}
	if !hasPath {
		filtered = append(filtered, "PATH="+os.Getenv("PATH")+":"+goBinDir)
	}
	// Force the selected go binary to use its own toolchain. Without this, a
	// go.mod in the current working directory can trigger GOTOOLCHAIN=auto to
	// switch to a different toolchain, breaking version detection and builds.
	filtered = append(filtered, "GOTOOLCHAIN=local")
	if !hasGonosumdb {
		filtered = append(filtered, "GONOSUMDB=*")
	}
	if !hasGoproxy {
		filtered = append(filtered, "GOPROXY=https://proxy.golang.org,direct")
	}
	return filtered
}
