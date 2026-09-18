package cli

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/spf13/cobra"
)

// Regression coverage for the post-install caddy-leg gate (audit #304): an
// install that reports success while veil-caddy.service is dead or nothing
// listens on :443 produces a panel URL nobody can reach.

type stubConn struct{ net.Conn }

func (stubConn) Close() error { return nil }

func stubCaddySeams(t *testing.T, unitErr, dialErr error) {
	t.Helper()
	oldUnit, oldDial := installCaddyUnitActiveFunc, caddyRouteDialer
	oldTimeout := installPanelReadyTimeout
	installCaddyUnitActiveFunc = func(context.Context) error { return unitErr }
	caddyRouteDialer = func(context.Context, string, string) (net.Conn, error) {
		if dialErr != nil {
			return nil, dialErr
		}
		return stubConn{}, nil
	}
	installPanelReadyTimeout = 300 * time.Millisecond
	t.Cleanup(func() {
		installCaddyUnitActiveFunc, caddyRouteDialer = oldUnit, oldDial
		installPanelReadyTimeout = oldTimeout
	})
}

func TestVerifyInstalledCaddyRouteFailsWhenUnitInactive(t *testing.T) {
	stubCaddySeams(t, errors.New("inactive"), nil)
	err := verifyInstalledCaddyRoute(nil, installer.RURecommendedProfile{InstallPanelCaddy: true})
	if err == nil || !strings.Contains(err.Error(), "veil-caddy.service is not active") {
		t.Fatalf("expected inactive-unit failure, got %v", err)
	}
}

func TestVerifyInstalledCaddyRouteFailsWhenPort443Closed(t *testing.T) {
	stubCaddySeams(t, nil, errors.New("connection refused"))
	err := verifyInstalledCaddyRoute(nil, installer.RURecommendedProfile{InstallPanelCaddy: true})
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1:443") {
		t.Fatalf("expected listener failure, got %v", err)
	}
}

func TestVerifyInstalledCaddyRouteWarnsOnPendingCertAndDNSMismatch(t *testing.T) {
	stubCaddySeams(t, nil, nil)
	oldProbe, oldLookup, oldIP := caddyPublicRouteProbeFunc, caddyDomainLookupFunc, installPublicIPResolveFunc
	caddyPublicRouteProbeFunc = func(context.Context, string, string) error {
		return errors.New("tls: no certificate")
	}
	caddyDomainLookupFunc = func(context.Context, string) ([]string, error) {
		return []string{"203.0.113.9"}, nil
	}
	installPublicIPResolveFunc = func(context.Context) (net.IP, error) {
		return net.ParseIP("198.51.100.7"), nil
	}
	t.Cleanup(func() {
		caddyPublicRouteProbeFunc, caddyDomainLookupFunc, installPublicIPResolveFunc = oldProbe, oldLookup, oldIP
	})

	cmd := &cobra.Command{}
	errBuf := &strings.Builder{}
	cmd.SetErr(errBuf)
	err := verifyInstalledCaddyRoute(cmd, installer.RURecommendedProfile{
		InstallPanelCaddy: true,
		Domain:            "panel.example.com",
		WebBasePath:       "/s3cret/",
	})
	if err != nil {
		t.Fatalf("warnings must not fail the install: %v", err)
	}
	out := errBuf.String()
	if !strings.Contains(out, "not serving TLS") {
		t.Fatalf("expected pending-certificate warning, got:\n%s", out)
	}
	if !strings.Contains(out, "203.0.113.9") || !strings.Contains(out, "198.51.100.7") {
		t.Fatalf("expected DNS-vs-public-IP warning, got:\n%s", out)
	}
}

func TestVerifyInstalledCaddyRouteHealthyPath(t *testing.T) {
	stubCaddySeams(t, nil, nil)
	oldProbe, oldLookup, oldIP := caddyPublicRouteProbeFunc, caddyDomainLookupFunc, installPublicIPResolveFunc
	caddyPublicRouteProbeFunc = func(context.Context, string, string) error { return nil }
	caddyDomainLookupFunc = func(context.Context, string) ([]string, error) { return []string{"198.51.100.7"}, nil }
	installPublicIPResolveFunc = func(context.Context) (net.IP, error) { return net.ParseIP("198.51.100.7"), nil }
	t.Cleanup(func() {
		caddyPublicRouteProbeFunc, caddyDomainLookupFunc, installPublicIPResolveFunc = oldProbe, oldLookup, oldIP
	})

	cmd := &cobra.Command{}
	errBuf := &strings.Builder{}
	cmd.SetErr(errBuf)
	if err := verifyInstalledCaddyRoute(cmd, installer.RURecommendedProfile{
		InstallPanelCaddy: true,
		Domain:            "panel.example.com",
		WebBasePath:       "/s3cret/",
	}); err != nil {
		t.Fatalf("healthy caddy leg must pass: %v", err)
	}
	if errBuf.Len() != 0 {
		t.Fatalf("healthy leg must not warn, got:\n%s", errBuf.String())
	}
}
