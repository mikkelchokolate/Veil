package runtimeports

import (
	"path/filepath"
	"strings"
	"testing"
)

// #1276: the managed Caddy admin API is unauthenticated, so it must be an
// owner-only Unix socket rather than a loopback TCP port any local uid can
// dial. The socket lives in veil-caddy.service's StateDirectory=caddy so the
// unit can create it under ProtectSystem=strict and sibling veil-proxy units
// can mask the whole tree.
func TestCaddyAdminListenIsOwnerOnlyUnixSocket(t *testing.T) {
	if CaddyAdminSocketPath != "/var/lib/caddy/admin.sock" {
		t.Fatalf("CaddyAdminSocketPath = %q", CaddyAdminSocketPath)
	}
	if filepath.Dir(CaddyAdminSocketPath) != CaddyStateDir {
		t.Fatalf("admin socket %q must live directly in the caddy StateDirectory %q", CaddyAdminSocketPath, CaddyStateDir)
	}
	listen := CaddyAdminListen()
	if listen != "unix//var/lib/caddy/admin.sock|0200" {
		t.Fatalf("CaddyAdminListen() = %q", listen)
	}
	if strings.Contains(listen, "127.") || strings.Contains(listen, ":2019") {
		t.Fatalf("admin listen %q must not be a TCP address", listen)
	}
	if got := CaddyAdminEndpoint(); got != "unix//var/lib/caddy/admin.sock" {
		t.Fatalf("CaddyAdminEndpoint() = %q", got)
	}
}
