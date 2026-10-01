package runtimeports

import (
	"net"
	"strconv"
)

const (
	// CaddyAdminPort is the loopback TCP port used by the managed Caddy JSON
	// configuration. Any wildcard/public TCP listener on the same numeric port
	// would also claim the loopback address and prevent Caddy from starting.
	CaddyAdminPort = 2019

	// CaddyAdminHost is the loopback address admin.listen binds. It lives in
	// the reserved 127.42.0.0/16 band — deliberately not 127.0.0.1 — because
	// veil-warp.service's IPAddressAllow covers 127.0.0.1 for ingress-source
	// reasons and systemd's filter does not distinguish directions, which
	// gave the most network-exposed unit a dial path to the unauthenticated
	// admin API (#1213).
	CaddyAdminHost = "127.42.0.1"
)

func CaddyAdminAddress() string {
	return net.JoinHostPort(CaddyAdminHost, strconv.Itoa(CaddyAdminPort))
}

func CaddyAdminEndpoint() string {
	return "http://" + CaddyAdminAddress()
}
