package cli

import (
	"os"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/firewall"
	"github.com/mikkelchokolate/Veil/internal/hostaccess"
	"github.com/mikkelchokolate/Veil/internal/service"
)

func TestMain(m *testing.M) {
	installSystemdRunFunc = func([]service.SystemdAction) error { return nil }
	installPrepareHostFunc = func(hostaccess.Paths) error { return nil }
	installFirewallApplyFunc = func([]firewall.Rule) error { return nil }
	commandLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	os.Exit(m.Run())
}
