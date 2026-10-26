package update

import (
	"fmt"
	"io"
	"os"

	versionflow "github.com/mikkelchokolate/Veil/internal/cliflow/version"
	"github.com/mikkelchokolate/Veil/internal/releaseverify"
	"github.com/mikkelchokolate/Veil/internal/renderer"
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
	WebBasePath    string
}

type WorkflowDependencies struct {
	FetchRelease             func() (*Release, error)
	DownloadAsset            func(string) ([]byte, error)
	VerifyReleaseEvidence    func(releaseverify.Evidence) error
	SourceUpdateCommit       func(current, tag string) (string, error)
	Executable               func() (string, error)
	ReplaceBinaryFromArchive func(currentPath string, archive []byte, yes bool) (string, error)
	RestartUpdated           func(currentPath string, backupPath string, opts WorkflowOptions) error
	// PackageManaged reports the owning OS package when the binary must be
	// upgraded by the package manager instead; nil uses the production probe
	// (#1424).
	PackageManaged func(binaryPath string) (string, bool)
	// DefaultExecutablePath is the fallback install path used when Executable
	// fails. Empty means the production default (/usr/local/bin/veil). Tests
	// must inject an isolated path — a unit test must never write to
	// /usr/local/bin.
	DefaultExecutablePath string
}

func RunWorkflow(opts WorkflowOptions, out io.Writer, deps WorkflowDependencies) error {
	if deps.FetchRelease == nil {
		return fmt.Errorf("update release fetcher is not configured")
	}
	if deps.DownloadAsset == nil {
		return fmt.Errorf("update asset downloader is not configured")
	}
	if deps.VerifyReleaseEvidence == nil {
		deps.VerifyReleaseEvidence = releaseverify.Verify
	}
	if deps.Executable == nil {
		deps.Executable = os.Executable
	}
	if deps.ReplaceBinaryFromArchive == nil {
		deps.ReplaceBinaryFromArchive = ReplaceBinaryFromArchive
	}
	if deps.PackageManaged == nil {
		deps.PackageManaged = DetectPackageManagedBinary
	}
	currentPath := ""
	if path, err := deps.Executable(); err == nil && path != "" {
		currentPath = path
	} else if deps.DefaultExecutablePath != "" {
		currentPath = deps.DefaultExecutablePath
	} else {
		currentPath = "/usr/local/bin/veil"
	}
	// A package-managed binary must be replaced by the package manager: the
	// updater would refresh the executable alone and leave the old package's
	// units and postinstall migrations in place (#1424).
	if owner, managed := deps.PackageManaged(currentPath); managed {
		return fmt.Errorf("this Veil binary belongs to %s; upgrade it with the package manager (apt/dnf/apk) instead of `veil update` so the packaged units and install hooks are refreshed", owner)
	}

	// 1. Fetch latest release metadata
	release, err := deps.FetchRelease()
	if err != nil {
		return fmt.Errorf("fetch latest release: %w", err)
	}
	fmt.Fprintf(out, "Latest release: %s\n", release.TagName)

	// Non-semver builds need either a verified forward ancestry proof or an
	// explicit override; Compare alone would silently downgrade newer sources.
	sourceCommit := ""
	if !versionflow.IsReleaseVersion(opts.CurrentVersion) {
		if !opts.Force && versionflow.SourceBuildCommit(opts.CurrentVersion) != "" && deps.SourceUpdateCommit != nil {
			sourceCommit, err = deps.SourceUpdateCommit(opts.CurrentVersion, release.TagName)
			if err != nil {
				return fmt.Errorf("cannot prove a safe source-to-release update (use --force to override): %w", err)
			}
			if !fullCommit(sourceCommit) {
				return fmt.Errorf("source-to-release update proof returned an invalid commit identity")
			}
			fmt.Fprintf(out, "Updating %s → %s (release contains the running source commit)\n", opts.CurrentVersion, release.TagName)
		} else {
			fmt.Fprintf(out, "Current version (%s) is not a release build; installing %s may be a downgrade.\n", opts.CurrentVersion, release.TagName)
			if !opts.Force {
				fmt.Fprintln(out, "Use --force to install it anyway.")
				return nil
			}
			fmt.Fprintf(out, "Updating %s → %s (forced)\n", opts.CurrentVersion, release.TagName)
		}
	} else {
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
	}

	assetName := AssetName()
	fmt.Fprintf(out, "Downloading %s...\n", assetName)
	fmt.Fprintf(out, "Downloading checksums.txt...\n")
	asset, err := NewReleaseAssetsWithVerifier(release, deps.DownloadAsset, func(evidence releaseverify.Evidence) error {
		evidence.SourceCommit = sourceCommit
		return deps.VerifyReleaseEvidence(evidence)
	}).DownloadVerifiedArchive()
	if err != nil {
		return err
	}
	archive := asset.Body
	fmt.Fprintln(out, "Checksum verified.")

	if opts.DryRun {
		fmt.Fprintln(out, "Dry run: would extract and replace the binary.")
		return nil
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
