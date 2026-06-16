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
	"runtime"
	"time"
	"strings"
)

const defaultGoVersion = "1.25.0"

var defaultGoSHA256 = map[string]string{
	"linux-amd64": "2852af0cb20a13139b3448992e69b868e50ed0f8a1e5940ee1de9e19a123b613",
	"linux-arm64": "05de75d6994a2783699815ee553bd5a9327d8b79991de36e38b66862782f54ae",
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
		return fmt.Errorf("Go download checksum mismatch")
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

// resolveGo returns the path to a go binary, provisioning one into CacheDir if
// the system PATH has none. It returns ("", nil) when no toolchain is available
// and provisioning is disabled (EnsureGo nil).
func resolveGo(ctx context.Context, cacheDir string, ensureGo func(context.Context) (string, error)) (string, error) {
	if p, err := exec.LookPath("go"); err == nil {
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
	if !hasGonosumdb {
		filtered = append(filtered, "GONOSUMDB=*")
	}
	if !hasGoproxy {
		filtered = append(filtered, "GOPROXY=https://proxy.golang.org,direct")
	}
	return filtered
}
