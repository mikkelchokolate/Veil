package warp

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/model"
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

type Config = model.WarpConfig

func Redact(warp Config) Config {
	redacted := warp
	disclosure := veilsettings.NewCredentialDisclosure()
	redacted.PrivateKey = disclosure.Redact(redacted.PrivateKey)
	redacted.LicenseKey = disclosure.Redact(redacted.LicenseKey)
	return redacted
}

func PreserveRedacted(update, current Config) Config {
	disclosure := veilsettings.NewCredentialDisclosure()
	update.LicenseKey = disclosure.PreserveRedacted(update.LicenseKey, current.LicenseKey)
	update.PrivateKey = disclosure.PreserveRedacted(update.PrivateKey, current.PrivateKey)
	return update
}

func SetDefaults(warp *Config) {
	if warp.Endpoint == "" {
		warp.Endpoint = "engage.cloudflareclient.com:2408"
	}
	if warp.SocksListen == "" {
		warp.SocksListen = "127.0.0.1"
	}
	if warp.SocksPort == 0 {
		warp.SocksPort = 40000
	}
	if warp.MTU == 0 {
		warp.MTU = 1280
	}
}

// Validate rejects values that cannot represent a valid WireGuard/WARP config.
// This invariant belongs below the Panel so direct API and imported-state
// mutations cannot persist values the browser UI would reject.
func Validate(warp Config) error {
	if warp.SocksPort < 1 || warp.SocksPort > 65535 {
		return errors.New("WARP SOCKS port must be between 1 and 65535")
	}
	// The sing-box WARP inbound is an unauthenticated SOCKS listener and
	// every protocol upstream dials it on loopback — a non-loopback listen
	// address would expose an open proxy to the network without helping any
	// consumer (audit #358). Empty is fine: SetDefaults/rendering fill in
	// 127.0.0.1.
	if listen := strings.TrimSpace(warp.SocksListen); listen != "" {
		addr, err := netip.ParseAddr(listen)
		if err != nil || !addr.IsLoopback() {
			return fmt.Errorf("WARP SOCKS listen address must be a loopback IP, got %q", warp.SocksListen)
		}
	}
	if warp.MTU < 576 || warp.MTU > 9000 {
		return errors.New("WARP MTU must be between 576 and 9000")
	}
	if len(warp.Reserved) != 0 && len(warp.Reserved) != 3 {
		return errors.New("WARP reserved must contain exactly three bytes")
	}
	for index, value := range warp.Reserved {
		if value < 0 || value > 255 {
			return fmt.Errorf("WARP reserved byte %d must be between 0 and 255", index)
		}
	}
	return nil
}
