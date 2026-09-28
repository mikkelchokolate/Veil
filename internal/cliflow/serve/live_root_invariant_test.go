package serve

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mikkelchokolate/Veil/internal/privileged"
)

// The Hysteria2 renderer embeds <liveRoot>/rules/geoip.dat style references in
// generated YAML (issue #1132), trusting that apply promotion delivers the
// staged rules/*.dat files into that same live root. The panel resolves its
// live root via Environment.LiveRoot; the privileged helper resolves its
// promotion destination via privileged.DefaultPolicy().GeneratedRoot. Those
// two derivations MUST agree for identical env — when they diverge (as a
// hand-rolled harness policy once did), rendered configs point at files that
// never exist and the daemon fails to load.
func TestLiveRootMatchesHelperGeneratedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		// The panel's Windows default (ProgramData\Veil\live) intentionally
		// differs from hostenv-based etc resolution; Windows serve is dev-only
		// and never runs the privileged promotion path this invariant guards.
		t.Skip("windows panel live root uses ProgramData, not hostenv derivation")
	}
	dir := t.TempDir()
	t.Setenv("VEIL_ETC_DIR", "")
	t.Setenv("VEIL_VAR_DIR", "")
	t.Setenv("VEIL_LIVE_ROOT", "")
	t.Setenv("VEIL_APPLY_ROOT", "")
	t.Setenv("VEIL_STATE_PATH", "")
	t.Setenv("VEIL_KEY_PATH", filepath.Join(dir, "state.key"))

	panelLive, source := (Environment{}).LiveRoot("")
	if source != "default" {
		t.Fatalf("LiveRoot source = %q, want default derivation", source)
	}
	helperGenerated := privileged.DefaultPolicy().GeneratedRoot
	if panelLive != helperGenerated {
		t.Fatalf("live-root divergence: panel=%q helper=%q — acl.geoip/acl.geosite references would point at files promotion never writes", panelLive, helperGenerated)
	}

	// An explicit VEIL_LIVE_ROOT must also flow into both sides identically.
	explicit := filepath.Join(dir, "custom-live")
	t.Setenv("VEIL_LIVE_ROOT", explicit)
	panelLive, _ = (Environment{}).LiveRoot("")
	helperGenerated = privileged.DefaultPolicy().GeneratedRoot
	if panelLive != explicit || helperGenerated != explicit {
		t.Fatalf("VEIL_LIVE_ROOT=%q not honored identically: panel=%q helper=%q", explicit, panelLive, helperGenerated)
	}
}
