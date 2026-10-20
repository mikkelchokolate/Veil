package runtimeports

import "path"

const (
	// CaddyAdminPort is the loopback TCP port the managed Caddy admin API
	// listened on before #1276. The admin API now binds a Unix socket
	// (CaddyAdminSocketPath), but the port stays reserved: a Caddy started
	// from a pre-#1276 config keeps the legacy TCP listener until its next
	// restart, and any wildcard/public TCP listener on the same numeric port
	// would also claim the loopback address and prevent that Caddy from
	// starting.
	CaddyAdminPort = 2019

	// CaddyAdminHost is the loopback address the pre-#1276 admin.listen
	// bound. It lives in the reserved 127.42.0.0/16 band — deliberately not
	// 127.0.0.1 — because veil-warp.service's IPAddressAllow covers 127.0.0.1
	// for ingress-source reasons and systemd's filter does not distinguish
	// directions, which gave the most network-exposed unit a dial path to the
	// unauthenticated admin API (#1213). The band stays outside every
	// sandboxed unit's allow list for legacy listeners.
	CaddyAdminHost = "127.42.0.1"

	// CaddyStateDir is veil-caddy.service's StateDirectory=caddy. systemd
	// always resolves StateDirectory names under /var/lib, independent of the
	// install's --var-dir.
	CaddyStateDir = "/var/lib/caddy"

	// CaddyAdminSocketPath is the Unix socket the managed Caddy admin API
	// binds (#1276). The API is unauthenticated: a loopback TCP listener let
	// any local uid dump the running config (forward_proxy credentials) or
	// POST /load a config serving root:veil-proxy key material. The socket
	// is created by Caddy (User=veil-proxy) with mode 0200, so only
	// veil-proxy and root (the privileged helper) can connect; it sits in
	// Caddy's own StateDirectory, the one tree the strict-sandboxed unit can
	// write, which sibling veil-proxy units mask via InaccessiblePaths.
	CaddyAdminSocketPath = CaddyStateDir + "/admin.sock"

	// caddyAdminSocketMode is the permission Caddy applies to the socket
	// file. connect(2) on a Unix socket needs only write permission.
	caddyAdminSocketMode = "0200"
)

// CaddyAdminListen is the admin.listen value rendered into the managed Caddy
// JSON config: Caddy's "unix/<path>|<mode>" network-address syntax.
func CaddyAdminListen() string {
	return CaddyAdminEndpoint() + "|" + caddyAdminSocketMode
}

// CaddyAdminEndpoint is the Caddy-style "unix/<path>" endpoint clients dial
// for the managed admin API.
func CaddyAdminEndpoint() string {
	return "unix/" + path.Clean(CaddyAdminSocketPath)
}
