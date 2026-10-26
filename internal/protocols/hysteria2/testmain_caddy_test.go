package hysteria2

import (
	"os"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/testutil/fakecaddy"
)

// TestMain keeps the Caddy capability probe hermetic: it resolves `caddy`
// through the trusted system directories (#1458), so the package installs a
// stub instead of depending on the host or CI PATH carrying a real binary.
func TestMain(m *testing.M) {
	cleanup := fakecaddy.MustInstall()
	code := m.Run()
	cleanup()
	os.Exit(code)
}
