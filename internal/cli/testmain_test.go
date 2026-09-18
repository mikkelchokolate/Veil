package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/hostaccess"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestMain(m *testing.M) {
	installSystemdRunFunc = func([]service.SystemdAction) error { return nil }
	installPrepareHostFunc = func(hostaccess.Paths) error { return nil }
	installFirewallApplyFunc = func([]firewall.Rule) error { return nil }
	installWaitPanelReadyFunc = func(*cobra.Command, installer.RURecommendedProfile, ruRecommendedInstallOptions) error {
		return nil
	}
	installVerifyCaddyRouteFunc = func(*cobra.Command, installer.RURecommendedProfile) error { return nil }
	commandLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	backupSystemdDir = filepath.Join(os.TempDir(), "veil-cli-test-systemd")
	backupSystemctlRun = func(args ...string) error {
		if len(args) > 0 && args[0] == "is-active" {
			return fmt.Errorf("inactive")
		}
		return nil
	}
	os.Exit(m.Run())
}
