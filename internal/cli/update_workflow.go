package cli

import (
	"fmt"
	"os"

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

	return restartUpdatedVeil(cmd, currentPath, backupPath, opts)
}
