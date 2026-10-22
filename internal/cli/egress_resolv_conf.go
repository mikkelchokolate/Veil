package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/spf13/cobra"
)

// egressResolvConfProbe is the host probe behind `veil egress-resolv-conf`.
// Tests replace it.
var egressResolvConfProbe = renderer.ProbeEgressDNS

// newEgressResolvConfCommand is run by veil-mieru as root before mita starts
// (ExecStartPre=+, see renderer.MieruResolvConfPath). It writes the public
// fallback resolv.conf the unit bind-mounts over /etc/resolv.conf when no
// host nameserver is reachable under the egress filter, and removes it
// otherwise, so the choice follows the host's DNS at every start (#1288).
func newEgressResolvConfCommand() *cobra.Command {
	output := renderer.MieruResolvConfPath
	cmd := &cobra.Command{
		Use:    "egress-resolv-conf",
		Short:  "Write the fallback resolv.conf for protocol units under the egress filter",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeEgressResolvConf(cmd.OutOrStdout(), output, egressResolvConfProbe())
		},
	}
	cmd.Flags().StringVar(&output, "output", output, "path of the fallback resolv.conf")
	return cmd
}

func writeEgressResolvConf(out io.Writer, output string, report renderer.EgressDNSReport) error {
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return fmt.Errorf("--output must be a clean absolute path, got %q", output)
	}
	body, ok := renderer.MieruResolvConf(report)
	if !ok {
		// Remove a link or file without following it; the unit then keeps
		// the host resolver.
		if err := os.Remove(output); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove fallback resolv.conf: %w", err)
		}
		fmt.Fprintf(out, "no fallback resolv.conf needed or possible (%s); keeping /etc/resolv.conf\n", egressResolvConfKeepReason(report))
		return nil
	}
	// The runtime directory belongs to the unit's service user, so never
	// open the destination by name: write a fresh O_EXCL temp file and
	// rename it into place, which replaces a planted link instead of
	// writing through it.
	tmp, err := os.CreateTemp(filepath.Dir(output), ".resolv.conf.*")
	if err != nil {
		return fmt.Errorf("create fallback resolv.conf: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write fallback resolv.conf: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod fallback resolv.conf: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync fallback resolv.conf: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close fallback resolv.conf: %w", err)
	}
	if err := os.Rename(tmpName, output); err != nil {
		return fmt.Errorf("publish fallback resolv.conf: %w", err)
	}
	fmt.Fprintf(out, "no host nameserver is reachable under the egress filter; resolving through %s (#1288)\n", strings.Join(report.MieruNameservers, ", "))
	return nil
}

// egressResolvConfKeepReason names why the host resolv.conf is kept, using
// the same distinctions as `veil doctor`.
func egressResolvConfKeepReason(report renderer.EgressDNSReport) string {
	switch {
	case report.ReadError != "":
		return "cannot read /etc/resolv.conf: " + report.ReadError
	case report.Reachable:
		return "a host nameserver is reachable under the egress filter"
	case report.ResolvConfMissing:
		return "/etc/resolv.conf is missing"
	default:
		return "no fallback nameservers"
	}
}
