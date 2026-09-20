package warp

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
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
	if err := validateSocksListen(warp.SocksListen); err != nil {
		return err
	}
	if err := validateEndpoint(warp.Endpoint); err != nil {
		return err
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

// validateSocksListen requires a loopback IP literal. sing-box emits an
// unauthenticated SOCKS listener on this address, so any non-loopback value
// would expose an open proxy to the network (#358). Hostnames are rejected:
// the bind must be deterministic, not resolver-dependent.
func validateSocksListen(listen string) error {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return nil // normalized to 127.0.0.1 by SetDefaults and the renderer
	}
	addr, err := netip.ParseAddr(listen)
	if err != nil {
		return errors.New("WARP SOCKS listen must be a loopback IP literal")
	}
	if !addr.IsLoopback() {
		return errors.New("WARP SOCKS listen must be a loopback address")
	}
	return nil
}

// validateEndpoint applies the same host:port shape check the renderer
// enforces so a bad endpoint fails at persist time, not at render/apply
// (#579).
func validateEndpoint(endpoint string) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil // defaults to engage.cloudflareclient.com:2408 downstream
	}
	_, portText, err := net.SplitHostPort(endpoint)
	if err != nil {
		return fmt.Errorf("WARP endpoint must be host:port: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return errors.New("WARP endpoint port must be between 1 and 65535")
	}
	return nil
}
