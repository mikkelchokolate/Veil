package runtimeports

const (
	// CaddyAdminPort is the loopback TCP port used by the managed Caddy JSON
	// configuration (admin.listen = 127.0.0.1:2019). Any wildcard/public TCP
	// listener on the same numeric port would also claim the loopback address
	// and prevent Caddy from starting.
	CaddyAdminPort = 2019
)
