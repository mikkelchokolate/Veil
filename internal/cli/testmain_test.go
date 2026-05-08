package cli

import (
	"os"
	"testing"

	"github.com/veil-panel/veil/internal/service"
)

func TestMain(m *testing.M) {
	installSystemdRunFunc = func([]service.SystemdAction) error { return nil }
	commandLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	os.Exit(m.Run())
}
