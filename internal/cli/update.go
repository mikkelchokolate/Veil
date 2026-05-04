package cli

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	updateRepoOwner = "mikkelchokolate"
	updateRepoName  = "Veil"
	updateTimeout   = 5 * time.Minute
)

var updateHTTPClient = &http.Client{Timeout: 30 * time.Second}

func newUpdateCommand(version string) *cobra.Command {
	var yes bool
	var dryRun bool
	var force bool
	var restart bool
	var listen string
	var authToken string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Download and install the latest Veil release",
		Long: `Update downloads the latest Veil release from GitHub, verifies its SHA256
checksum, backs up the current binary, and replaces it.

Use --dry-run to preview without making changes.
Use --force to reinstall even if the current version is already the latest.
Use --restart to restart the veil service and perform a health check after update.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			// 1. Fetch latest release metadata
			release, err := fetchLatestRelease()
			if err != nil {
				return fmt.Errorf("fetch latest release: %w", err)
			}
			fmt.Fprintf(out, "Latest release: %s\n", release.TagName)

			// 2. Compare versions
			cmp := compareVersions(version, release.TagName)
			switch {
			case cmp > 0:
				fmt.Fprintf(out, "Current version (%s) is newer than latest release (%s).\n", version, release.TagName)
				if !force {
					fmt.Fprintln(out, "Use --force to reinstall anyway.")
					return nil
				}
			case cmp == 0:
				fmt.Fprintf(out, "Veil is already at the latest version (%s).\n", version)
				if !force {
					fmt.Fprintln(out, "Use --force to reinstall anyway.")
					return nil
				}
			default:
				fmt.Fprintf(out, "Updating %s → %s\n", version, release.TagName)
			}

			// 3. Find the correct asset for this platform
			assetName := updateAssetName()
			checksumsName := "checksums.txt"
			assetURL := findAssetURL(release.Assets, assetName)
			checksumsURL := findAssetURL(release.Assets, checksumsName)
			if assetURL == "" {
				return fmt.Errorf("release %s has no asset %s", release.TagName, assetName)
			}
			if checksumsURL == "" {
				return fmt.Errorf("release %s has no checksums asset", release.TagName)
			}

			// 4. Download archive and checksums
			fmt.Fprintf(out, "Downloading %s...\n", assetName)
			archive, err := downloadAsset(assetURL)
			if err != nil {
				return fmt.Errorf("download %s: %w", assetName, err)
			}

			fmt.Fprintf(out, "Downloading checksums.txt...\n")
			checksumsBody, err := downloadAsset(checksumsURL)
			if err != nil {
				return fmt.Errorf("download checksums: %w", err)
			}

			// 5. Verify archive checksum
			if err := verifyAssetChecksum(archive, assetName, string(checksumsBody)); err != nil {
				return fmt.Errorf("checksum verification failed: %w", err)
			}
			fmt.Fprintln(out, "Checksum verified.")

			if dryRun {
				fmt.Fprintln(out, "Dry run: would extract and replace the binary.")
				return nil
			}

			// 6. Extract the binary from the tar.gz
			fmt.Fprintln(out, "Extracting binary...")
			binary, err := extractVeilBinary(archive)
			if err != nil {
				return fmt.Errorf("extract binary: %w", err)
			}

			// 7. Find current binary path
			currentPath, err := os.Executable()
			if err != nil {
				currentPath = "/usr/local/bin/veil"
			}
			fmt.Fprintf(out, "Current binary: %s\n", currentPath)

			// 8. Backup current binary
			backupPath := currentPath + ".backup"
			fmt.Fprintf(out, "Backing up to %s...\n", backupPath)
			if err := copyFileData(currentPath, backupPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("backup: %w", err)
			}

			if !yes {
				return fmt.Errorf("update requires --yes to confirm replacing %s", currentPath)
			}

			// 9. Replace binary atomically
			fmt.Fprintf(out, "Installing to %s...\n", currentPath)
			if err := replaceBinaryAtomic(currentPath, binary); err != nil {
				return fmt.Errorf("replace binary: %w", err)
			}

			fmt.Fprintf(out, "Updated to %s.\n", release.TagName)
			if !restart {
				fmt.Fprintln(out, "Restart the veil service to apply the update:")
				fmt.Fprintln(out, "  systemctl restart veil.service")
				return nil
			}

			// 10. Restart service and health check
			fmt.Fprintln(out, "Restarting veil.service...")
			if err := runSystemctlRestart("veil.service"); err != nil {
				return fmt.Errorf("restart failed (binary updated, rollback with: mv %s %s): %w", backupPath, currentPath, err)
			}
			fmt.Fprintln(out, "Service restarted. Running health check...")

			addr := resolveStatusListen(listen)
			if !strings.Contains(addr, "://") {
				addr = "http://" + addr
			}
			token, _ := resolveServeAuthToken(authToken)
			if err := waitForHealthy(addr, token, 10*time.Second); err != nil {
				return fmt.Errorf("health check failed after restart (binary updated, rollback with: mv %s %s): %w", backupPath, currentPath, err)
			}
			fmt.Fprintf(out, "Service healthy. Update complete.\n")
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "confirm binary replacement")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview update without making changes")
	cmd.Flags().BoolVar(&force, "force", false, "reinstall even if already at latest version")
	cmd.Flags().BoolVar(&restart, "restart", false, "restart veil.service and health check after update")
	cmd.Flags().StringVar(&listen, "listen", "", "veil serve address for health check (default: 127.0.0.1:2096)")
	cmd.Flags().StringVar(&authToken, "auth-token", "", "API token for health check")
	return cmd
}

// runSystemctlRestart runs systemctl restart for the given unit.
var runSystemctlRestart = func(unit string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return execCommand(ctx, "systemctl", "restart", unit)
}

var execCommand = func(ctx context.Context, name string, args ...string) error {
	cmd := execCmd(ctx, name, args...)
	return cmd.Run()
}

var execCmd = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// waitForHealthy polls the /healthz endpoint until it returns 200 or times out.
func waitForHealthy(addr string, token string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr+"/healthz", nil)
		if err != nil {
			cancel()
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if token != "" {
			req.Header.Set("X-Veil-Token", token)
		}
		resp, err := updateHTTPClient.Do(req)
		cancel()
		if err == nil && resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}
		if resp != nil {
			resp.Body.Close()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("health check timed out after %v", timeout)
}

// updateAssetName returns the expected release asset name for the current platform.
func updateAssetName() string {
	os := runtime.GOOS
	arch := runtime.GOARCH
	switch arch {
	case "amd64", "x86_64":
		arch = "amd64"
	case "arm64", "aarch64":
		arch = "arm64"
	}
	return fmt.Sprintf("veil_%s_%s.tar.gz", os, arch)
}

// githubRelease represents a subset of the GitHub Release API response.
type githubRelease struct {
	TagName string          `json:"tag_name"`
	Assets  []githubAsset   `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// fetchLatestRelease queries the GitHub API for the latest release.
func fetchLatestRelease() (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", updateRepoOwner, updateRepoName)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "veil")
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("parse release JSON: %w", err)
	}
	return &release, nil
}

// findAssetURL returns the download URL for the named asset.
func findAssetURL(assets []githubAsset, name string) string {
	for _, a := range assets {
		if a.Name == name {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

// downloadAsset downloads a URL and returns the body bytes.
func downloadAsset(url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "veil")
	resp, err := updateHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	const maxSize = 50 * 1024 * 1024 // 50 MB
	return io.ReadAll(io.LimitReader(resp.Body, maxSize))
}

// verifyAssetChecksum verifies that archive bytes match the expected SHA256 in checksumsText.
func verifyAssetChecksum(archive []byte, assetName, checksumsText string) error {
	expected := extractChecksumForFile(checksumsText, assetName)
	if expected == "" {
		return fmt.Errorf("no checksum found for %s", assetName)
	}
	actual := sha256.Sum256(archive)
	actualHex := hex.EncodeToString(actual[:])
	if !strings.EqualFold(actualHex, expected) {
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", expected, actualHex)
	}
	return nil
}

// extractChecksumForFile finds the SHA256 hex for a filename in checksums.txt output.
func extractChecksumForFile(checksumsText, filename string) string {
	for _, line := range strings.Split(checksumsText, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == filename && i > 0 {
				return fields[i-1]
			}
		}
		// Also try "filename" at end (sha256sum format: hash  filename)
		if len(fields) >= 2 && fields[len(fields)-1] == filename {
			return fields[0]
		}
	}
	return ""
}

// extractVeilBinary extracts the "veil" binary from a tar.gz archive.
func extractVeilBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		return nil, fmt.Errorf("gzip decompress: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar read: %w", err)
		}
		if hdr.Name == "veil" || hdr.Name == "./veil" {
			const maxBinSize = 100 * 1024 * 1024 // 100 MB
			return io.ReadAll(io.LimitReader(tr, maxBinSize))
		}
	}
	return nil, fmt.Errorf("veil binary not found in archive")
}

// copyFileData copies file contents (not permissions) from src to dst.
func copyFileData(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer dstFile.Close()
	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}
	return dstFile.Sync()
}

// replaceBinaryAtomic writes binary data to a temp file and renames it over dst.
func replaceBinaryAtomic(dst string, data []byte) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".veil-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, dst)
}
