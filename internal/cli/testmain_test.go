package cli

import (
	"os"
	"testing"

	"github.com/veil-panel/veil/internal/service"
)

func TestMain(m *testing.M) {
	installSystemdRunFunc = func([]service.SystemdAction) error { return nil }
	os.Exit(m.Run())
}
