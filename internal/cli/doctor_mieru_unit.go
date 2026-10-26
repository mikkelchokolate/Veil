package cli

import (
	"context"
	"strings"
	"time"

	doctorflow "github.com/mikkelchokolate/Veil/internal/cliflow/doctor"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/syspath"
)

// mieruUnitFallbackState says whether the loaded veil-mieru.service carries
// the #1288 DNS fallback hook. `veil update` swaps the binary without
// re-rendering install-managed units, so doctor must not promise the
// fallback when the unit on disk predates it.
type mieruUnitFallbackState int

const (
	mieruUnitFallbackUnknown mieruUnitFallbackState = iota
	mieruUnitFallbackInstalled
	mieruUnitFallbackMissing
)

// doctorMieruUnitFallbackProbe asks systemd for the loaded unit. Tests
// replace it.
var doctorMieruUnitFallbackProbe = probeMieruUnitFallbackState

// probeMieruUnitFallbackState asks the trusted systemctl about the loaded unit.
// The probe runs as root from `veil doctor`, so it resolves systemctl through
// syspath rather than the caller's PATH (#1490).
func probeMieruUnitFallbackState() mieruUnitFallbackState {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := syspath.CommandContext(ctx, "systemctl", "show", "veil-mieru.service", "--property=LoadState,BindReadOnlyPaths").Output()
	if err != nil {
		return mieruUnitFallbackUnknown
	}
	return parseMieruUnitFallbackState(string(out))
}

// parseMieruUnitFallbackState reads `systemctl show` output. A unit that is
// not loaded (no Mieru on this host, or no systemd) is unknown: nothing
// runs yet, and the next install renders the current unit.
func parseMieruUnitFallbackState(show string) mieruUnitFallbackState {
	loaded := false
	binds := ""
	for _, line := range strings.Split(show, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "LoadState":
			loaded = value == "loaded"
		case "BindReadOnlyPaths":
			binds = value
		}
	}
	if !loaded {
		return mieruUnitFallbackUnknown
	}
	if strings.Contains(binds, renderer.MieruResolvConfPath+":/etc/resolv.conf") {
		return mieruUnitFallbackInstalled
	}
	return mieruUnitFallbackMissing
}

// withMieruUnitFallbackState drops the promised Mieru fallback when the
// loaded unit lacks the hook, so doctor says "domain lookups fail" with a
// `veil repair` hint instead.
func withMieruUnitFallbackState(check doctorflow.EgressDNSCheck, state mieruUnitFallbackState) doctorflow.EgressDNSCheck {
	if state == mieruUnitFallbackMissing && len(check.MieruNameservers) > 0 {
		check.MieruNameservers = nil
		check.MieruUnitOutdated = true
	}
	return check
}
