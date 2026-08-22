package clientaddr

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type contextKey struct{}

func WithContext(request *http.Request, address string) *http.Request {
	return request.WithContext(context.WithValue(request.Context(), contextKey{}, address))
}

func FromContext(request *http.Request) (string, bool) {
	address, ok := request.Context().Value(contextKey{}).(string)
	return address, ok && address != ""
}

// Resolver implements the documented trusted-side X-Forwarded-For model.
// RemoteAddr is always authoritative unless it belongs to an explicitly
// configured trusted proxy prefix.
type Resolver struct {
	trusted []netip.Prefix
}

func New(trustedCIDRs []string) (Resolver, error) {
	resolver := Resolver{}
	for _, raw := range trustedCIDRs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return Resolver{}, fmt.Errorf("invalid trusted proxy CIDR %q: %w", raw, err)
		}
		resolver.trusted = append(resolver.trusted, prefix.Masked())
	}
	return resolver, nil
}

func (r Resolver) Resolve(request *http.Request) (string, error) {
	remote, err := parseRemoteAddr(request.RemoteAddr)
	if err != nil {
		return "", err
	}
	if !r.isTrusted(remote) {
		return remote.String(), nil
	}
	raw := strings.TrimSpace(request.Header.Get("X-Forwarded-For"))
	if raw == "" {
		return remote.String(), nil
	}
	parts := strings.Split(raw, ",")
	chain := make([]netip.Addr, 0, len(parts)+1)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		address, err := netip.ParseAddr(part)
		if err != nil {
			return "", fmt.Errorf("malformed X-Forwarded-For entry %q", part)
		}
		chain = append(chain, address.Unmap())
	}
	chain = append(chain, remote)
	for i := len(chain) - 1; i >= 0; i-- {
		if !r.isTrusted(chain[i]) {
			return chain[i].String(), nil
		}
	}
	return chain[0].String(), nil
}

func (r Resolver) isTrusted(address netip.Addr) bool {
	for _, prefix := range r.trusted {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func parseRemoteAddr(raw string) (netip.Addr, error) {
	host := raw
	if parsedHost, _, err := net.SplitHostPort(raw); err == nil {
		host = parsedHost
	}
	address, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("invalid RemoteAddr %q: %w", raw, err)
	}
	return address.Unmap(), nil
}
