package installer

import (
	"os"
	"testing"
)

// Package tests run in arbitrary environments (root CI containers, dev
// machines): default to the non-root path so no test depends on real
// veil/veil-proxy accounts or host chown privileges. Ownership-contract tests
// override effectiveUID/lookupGroup/chownPath/chmodPath themselves.
func TestMain(m *testing.M) {
	origUID := effectiveUID
	effectiveUID = func() int { return 1000 }
	code := m.Run()
	effectiveUID = origUID
	os.Exit(code)
}
