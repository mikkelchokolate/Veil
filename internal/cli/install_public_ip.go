package cli

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/veil-panel/veil/internal/hostenv"
)

func resolveInstallPublicIP(ctx context.Context, value string) (net.IP, error) {
	if value == "" {
		return nil, nil
	}
	if value == "auto" {
		detectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return hostenv.DetectPublicIP(detectCtx, installPublicIPClient, installPublicIPEndpoints)
	}
	parsed := net.ParseIP(value)
	if parsed == nil {
		return nil, fmt.Errorf("--public-ip must be a valid IPv4 or IPv6 address, or auto")
	}
	return parsed, nil
}
