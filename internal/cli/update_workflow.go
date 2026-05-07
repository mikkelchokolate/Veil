package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var updateReleaseFetcher = fetchLatestRelease
var updateAssetDownloader = downloadAsset

type updateWorkflowOptions struct {
	CurrentVersion string
	Yes            bool
	DryRun         bool
	Force          bool
	Restart        bool
	Staged         bool
	Listen         string
	AuthToken      string
}

func runUpdateWorkflow(cmd *cobra.Command, opts updateWorkflowOptions) error {
	out := cmd.OutOrStdout()

	// 1. Fetch latest release metadata
	release, err := updateReleaseFetcher()
	if err != nil {
		return fmt.Errorf("fetch latest release: %w", err)
	}
	fmt.Fprintf(out, "Latest release: %s\n", release.TagName)

	// 2. Compare versions
	cmp := compareVersions(opts.CurrentVersion, release.TagName)
	switch {
	case cmp > 0:
		fmt.Fprintf(out, "Current version (%s) is newer than latest release (%s).\n", opts.CurrentVersion, release.TagName)
		if !opts.Force {
			fmt.Fprintln(out, "Use --force to reinstall anyway.")
			return nil
		}
	case cmp == 0:
		fmt.Fprintf(out, "Veil is already at the latest version (%s).\n", opts.CurrentVersion)
		if !opts.Force {
			fmt.Fprintln(out, "Use --force to reinstall anyway.")
			return nil
		}
	default:
		fmt.Fprintf(out, "Updating %s → %s\n", opts.CurrentVersion, release.TagName)
	}

	assetName := updateAssetName()
	fmt.Fprintf(out, "Downloading %s...\n", assetName)
	fmt.Fprintf(out, "Downloading checksums.txt...\n")
	_, archive, err := downloadVerifiedUpdateAsset(release)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "Checksum verified.")

	if opts.DryRun {
		fmt.Fprintln(out, "Dry run: would extract and replace the binary.")
		return nil
	}

	currentPath, err := os.Executable()
	if err != nil {
		currentPath = "/usr/local/bin/veil"
	}
	backupPath := currentPath + ".backup"
	fmt.Fprintln(out, "Extracting binary...")
	fmt.Fprintf(out, "Current binary: %s\n", currentPath)
	fmt.Fprintf(out, "Backing up to %s...\n", backupPath)
	fmt.Fprintf(out, "Installing to %s...\n", currentPath)
	backupPath, err = replaceVeilBinaryFromArchive(currentPath, archive, opts.Yes)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Updated to %s.\n", release.TagName)
	if !opts.Restart && !opts.Staged {
		fmt.Fprintln(out, "Restart the veil service to apply the update:")
		fmt.Fprintln(out, "  systemctl restart veil.service")
		return nil
	}

	// 10. Restart service and health check (with optional staged rollback)
	addr := resolveStatusListen(opts.Listen)
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	token, _ := resolveServeAuthToken(opts.AuthToken)

	fmt.Fprintln(out, "Restarting veil.service...")
	if err := runSystemctlRestart("veil.service"); err != nil {
		if opts.Staged {
			fmt.Fprintf(out, "Restart failed, rolling back to previous binary...\n")
			if rollbackErr := rollbackBinary(backupPath, currentPath); rollbackErr != nil {
				return fmt.Errorf("restart failed and rollback also failed: restart: %w; rollback: %v", err, rollbackErr)
			}
			fmt.Fprintln(out, "Rolled back to previous binary.")
			return fmt.Errorf("restart failed, rolled back: %w", err)
		}
		return fmt.Errorf("restart failed (binary updated, rollback with: mv %s %s): %w", backupPath, currentPath, err)
	}
	fmt.Fprintln(out, "Service restarted. Running health check...")

	if err := waitForHealthy(addr, token, 10*time.Second); err != nil {
		if opts.Staged {
			fmt.Fprintf(out, "Health check failed, rolling back to previous binary...\n")
			if rollbackErr := rollbackBinary(backupPath, currentPath); rollbackErr != nil {
				return fmt.Errorf("health check failed and rollback also failed: health: %w; rollback: %v", err, rollbackErr)
			}
			// Restart again after rollback to get the old binary running
			if restartErr := runSystemctlRestart("veil.service"); restartErr != nil {
				fmt.Fprintf(out, "Warning: rollback binary installed but restart failed: %v\n", restartErr)
			}
			fmt.Fprintln(out, "Rolled back to previous binary.")
			return fmt.Errorf("health check failed, rolled back: %w", err)
		}
		return fmt.Errorf("health check failed after restart (binary updated, rollback with: mv %s %s): %w", backupPath, currentPath, err)
	}
	fmt.Fprintf(out, "Service healthy. Update complete.\n")
	return nil
}
