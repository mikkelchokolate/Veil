package cli

import (
	"context"
	"net"
)

type staticDNSResolver struct {
	ips []net.IP
	err error
}

func (r staticDNSResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return r.ips, r.err
}
