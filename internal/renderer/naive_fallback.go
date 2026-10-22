package renderer

import (
	"path/filepath"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/hostenv"
)

// NaiveDefaultFallbackRoot is the packaged web root Caddy serves for naive
// fallback. It lives under /etc/veil because the caddy unit runs as
// veil-proxy with /var/lib/veil in InaccessiblePaths: a var-lib root would
// be unreachable (audit #497). Custom --etc-dir installs resolve their own
// <etc>/www tree through naiveFallbackBase/hostenv.EtcDir instead.
const NaiveDefaultFallbackRoot = "/etc/veil/www"

// naiveFallbackBase resolves the fallback web root for this process:
// <etc>/www where <etc> honors VEIL_ETC_DIR/VEIL_LIVE_ROOT/VEIL_KEY_PATH and
// defaults to /etc/veil.
func naiveFallbackBase() string {
	return filepath.ToSlash(filepath.Join(hostenv.EtcDir(), "www"))
}

// naiveFallbackRootAllowedUnder reports whether root is exactly base or a
// subdirectory of it. Both arguments are expected to be slash-normalized
// absolute paths. resolveNaiveFallbackRoot (caddyjson.go) uses it to keep
// the fallback root inside the managed <etc>/www tree and away from the etc
// root itself or siblings like panel/, which hold keys readable by
// veil-proxy (issues #618, #634).
func naiveFallbackRootAllowedUnder(root, base string) bool {
	return root == base || strings.HasPrefix(root, base+"/")
}
