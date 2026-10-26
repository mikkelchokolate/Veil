// Package fakecaddy installs a hermetic `caddy` stub in the trusted system
// directories for a test process.
//
// Since #1458 the capability probe resolves the binary through
// internal/syspath, so it deliberately ignores the ambient PATH. Packages that
// render Caddy JSON therefore cannot hand themselves a fake caddy through PATH
// any more; they call Install from their TestMain so the tests stay hermetic on
// hosts (and CI shards) that do not carry a real Caddy binary.
package fakecaddy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mikkelchokolate/Veil/internal/syspath"
)

// modules is what a packaged caddy with the forward_proxy handler and HTTP/3
// reports. Keep it in sync with scripts/ci/test.sh's runtime check.
const modules = `[{"module_name":"http"},{"module_name":"http.handlers.forward_proxy"}]`

const script = "#!/bin/sh\nprintf '%s' '" + modules + "'\n"

// Install prepends a directory holding the stub to syspath.TrustedDirs and
// returns a cleanup that restores the list and removes the directory. Call it
// from TestMain and run the cleanup before os.Exit.
func Install() (cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "veil-fakecaddy-")
	if err != nil {
		return nil, fmt.Errorf("create fake caddy dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "caddy"), []byte(script), 0o755); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("write fake caddy: %w", err)
	}
	original := syspath.TrustedDirs
	syspath.TrustedDirs = append([]string{dir}, original...)
	return func() {
		syspath.TrustedDirs = original
		_ = os.RemoveAll(dir)
	}, nil
}

// MustInstall is Install for TestMain bodies that cannot return an error.
func MustInstall() func() {
	cleanup, err := Install()
	if err != nil {
		panic(err)
	}
	return cleanup
}
