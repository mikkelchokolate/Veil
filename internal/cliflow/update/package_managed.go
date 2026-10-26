package update

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/syspath"
)

// packageManagedMarkers are written by the OS package's postinstall. The marker
// is the fast, manager-agnostic signal that this binary belongs to a package,
// whose units and hooks only the package manager refreshes (#1424).
var packageManagedMarkers = []string{"/etc/veil/.package-managed"}

// packageQueryRunner runs a package-database probe. Tests replace it.
var packageQueryRunner = func(name string, args ...string) ([]byte, error) {
	return syspath.Command(name, args...).CombinedOutput()
}

// DetectPackageManagedBinary reports the package that owns binaryPath, or false
// when the binary is not package-managed. Markers cover packages built with the
// current postinstall; the database probes catch hosts installed by an older
// package, which the marker cannot identify (#1424).
func DetectPackageManagedBinary(binaryPath string) (string, bool) {
	for _, marker := range packageManagedMarkers {
		body, err := os.ReadFile(marker)
		if err != nil {
			continue
		}
		if owner := firstLine(string(body)); owner != "" {
			return owner, true
		}
		return filepath.Base(marker), true
	}
	if strings.TrimSpace(binaryPath) == "" {
		return "", false
	}
	for _, probe := range [][]string{
		{"dpkg-query", "-S", binaryPath},
		{"rpm", "-qf", binaryPath},
		{"apk", "info", "--who-owns", binaryPath},
	} {
		out, err := packageQueryRunner(probe[0], probe[1:]...)
		if err != nil {
			continue
		}
		if owner := firstLine(string(out)); owner != "" {
			return owner, true
		}
	}
	return "", false
}

func firstLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "no path found") {
			return line
		}
	}
	return ""
}
