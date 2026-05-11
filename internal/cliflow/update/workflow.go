package update

import (
	"fmt"
	"io"
	"os"

	versionflow "github.com/veil-panel/veil/internal/cliflow/version"
	"github.com/veil-panel/veil/internal/renderer"
)

type WorkflowOptions struct {
	CurrentVersion string
	Yes            bool
	DryRun         bool
	Force          bool
	Restart        bool
	Staged         bool
	Listen         string
	AuthToken      string
}

type WorkflowDependencies struct {
	FetchRelease             func() (*Release, error)
	DownloadAsset            func(string) ([]byte, error)
	Executable               func() (string, error)
	ReplaceBinaryFromArchive func(currentPath string, archive []byte, yes bool) (string, error)
	RestartUpdated           func(currentPath string, backupPath string, opts WorkflowOptions) error
}

func RunWorkflow(opts WorkflowOptions, out io.Writer, deps WorkflowDependencies) error {
	if deps.FetchRelease == nil {
		return fmt.Errorf("update release fetcher is not configured")
	}
	if deps.DownloadAsset == nil {
		return fmt.Errorf("update asset downloader is not configured")
	}
	if deps.Executable == nil {
		deps.Executable = os.Executable
	}
	if deps.ReplaceBinaryFromArchive == nil {
		deps.ReplaceBinaryFromArchive = ReplaceBinaryFromArchive
	}

	// 1. Fetch latest release metadata
	release, err := deps.FetchRelease()
	if err != nil {
		return fmt.Errorf("fetch latest release: %w", err)
	}
	fmt.Fprintf(out, "Latest release: %s\n", release.TagName)

	// 2. Compare versions
	cmp := versionflow.Compare(opts.CurrentVersion, release.TagName)
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

	assetName := AssetName()
	fmt.Fprintf(out, "Downloading %s...\n", assetName)
	fmt.Fprintf(out, "Downloading checksums.txt...\n")
	asset, err := NewReleaseAssets(release, deps.DownloadAsset).DownloadVerifiedArchive()
	if err != nil {
		return err
	}
	archive := asset.Body
	fmt.Fprintln(out, "Checksum verified.")

	if opts.DryRun {
		fmt.Fprintln(out, "Dry run: would extract and replace the binary.")
		return nil
	}

	currentPath, err := deps.Executable()
	if err != nil {
		currentPath = "/usr/local/bin/veil"
	}
	backupPath := currentPath + ".backup"
	fmt.Fprintln(out, "Extracting binary...")
	fmt.Fprintf(out, "Current binary: %s\n", currentPath)
	fmt.Fprintf(out, "Backing up to %s...\n", backupPath)
	fmt.Fprintf(out, "Installing to %s...\n", currentPath)
	backupPath, err = deps.ReplaceBinaryFromArchive(currentPath, archive, opts.Yes)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Updated to %s.\n", release.TagName)
	if !opts.Restart && !opts.Staged {
		fmt.Fprintln(out, "Restart the veil service to apply the update:")
		fmt.Fprintln(out, "  systemctl restart "+renderer.UnitVeil)
		return nil
	}
	if deps.RestartUpdated == nil {
		return fmt.Errorf("update restart flow is not configured")
	}
	return deps.RestartUpdated(currentPath, backupPath, opts)
}
