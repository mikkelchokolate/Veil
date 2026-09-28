package hysteria2

import (
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/generatedconfig"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	"github.com/mikkelchokolate/Veil/internal/runtimeports"
)

// RenderConfig generates one Hysteria2 config per enabled inbound.
func (Plugin) RenderConfig(input generatedconfig.ProtocolRenderInput) ([]generatedconfig.GeneratedConfigArtifact, bool, error) {
	if len(input.Inbounds) == 0 {
		return nil, false, nil
	}
	var artifacts []generatedconfig.GeneratedConfigArtifact
	for _, inbound := range input.Inbounds {
		access, err := clientaccess.BuildClientAccess(input.Settings, inbound)
		if err != nil {
			return nil, false, err
		}
		if len(access.Hysteria2Users()) == 0 && inbound.HadClientProfiles() {
			// Profiles exist but none are enabled, were suppressed
			// post-migration, or the normalized client store still has
			// bindings whose credentials are all revoked, expired, depleted
			// or disabled: do not revive the inbound password (issues #1098,
			// #1117). Skipping the artifact orphans the live YAML, so
			// promotion removes it and the veil-hysteria2@<name> instance is
			// stopped — the inbound fails fully closed.
			continue
		}
		body, err := renderHysteria2(input.Settings, inbound, input.Warp, input.Rules, input.Paths)
		if err != nil {
			return nil, false, err
		}
		subpath := "hysteria2/" + inbound.Name + ".yaml"
		artifacts = append(artifacts, generatedconfig.GeneratedConfigArtifact{
			Path: input.Paths.Generated(subpath),
			Body: body,
		})
	}
	return artifacts, len(artifacts) > 0, nil
}

// ArtifactSpec returns the artifact metadata for Hysteria2 configs.
func (Plugin) ArtifactSpec() generatedconfig.ArtifactSpec {
	return generatedconfig.ArtifactSpec{
		Subpath:        generatedconfig.Hysteria2ConfigSubpath,
		ValidationName: "hysteria2",
	}
}

func renderHysteria2(settings model.Settings, inbound model.Inbound, warp model.WarpConfig, rules []model.RoutingRule, paths generatedconfig.Paths) (string, error) {
	password := hysteria2Password(settings, inbound)
	access, err := clientaccess.BuildClientAccess(settings, inbound)
	if err != nil {
		return "", err
	}
	url := masqueradeURL(settings, inbound)
	domain := model.ResolveInboundDomain(inbound, settings)
	hystConfig := renderer.Hysteria2Config{
		ListenPort:         inbound.Port,
		Domain:             domain,
		Password:           password,
		Users:              access.Hysteria2Users(),
		MasqueradeURL:      url,
		TrafficStatsListen: runtimeports.Hysteria2TrafficStatsAddress(inbound.Port),
		TrafficStatsSecret: TrafficStatsSecret(settings, inbound),
	}
	// A client carrying deviceLimit/ipLimit cannot be enforced by the static
	// userpass/password modes — those authenticate the secret and nothing
	// else. The moment any admitted runtime credential is limited the config
	// switches to the internal HTTP auth callback, which performs the same
	// credential check plus the per-client admission limits (#1173). The
	// callback shares one panel-side listener across inbounds; per-inbound
	// scoping and the daemon-facing secret ride in the URL path.
	if runtimeCredentialsLimited(inbound.RuntimeCredentials) {
		hystConfig.HTTPAuthURL = HTTPAuthURL(settings, inbound)
	}
	// Use Caddy-managed certificates whenever the inbound has its own domain
	// (Caddy is already required for it) or when the panel itself uses Caddy.
	if domain != "" && (settings.PanelAccess == "caddy" || model.InboundDomain(inbound) != "") {
		hystConfig.CertPath = paths.CertPath(domain)
		hystConfig.KeyPath = paths.KeyPath(domain)
	} else {
		hystConfig.CertPath = paths.PanelCertPath()
		hystConfig.KeyPath = paths.PanelKeyPath()
	}
	// The egress ACL is emitted for every Hysteria2 config (with or without
	// WARP), so routing rules and the geo databases are wired unconditionally —
	// a user rule naming "warp" degrades to "direct" when no upstream exists
	// rather than silently dropping the ACL entirely (#1095).
	if warp.Enabled {
		socksPort := warp.SocksPort
		if socksPort == 0 {
			socksPort = 40000
		}
		hystConfig.Upstream = net.JoinHostPort(warp.SocksDialAddr(), strconv.Itoa(socksPort))
	}
	hystConfig.GeoIPPath = routingDatPath(paths, "geoip.dat")
	hystConfig.GeoSitePath = routingDatPath(paths, "geosite.dat")
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		hystConfig.RoutingRules = append(hystConfig.RoutingRules, renderer.Hysteria2RoutingRule{
			Match:    rule.Match,
			Outbound: rule.Outbound,
		})
	}
	return renderer.RenderHysteria2(hystConfig)
}

// routingDatPath resolves the geoip.dat/geosite.dat path embedded in the
// generated Hysteria2 YAML. The file is staged under
// <applyRoot>/generated/rules and then PROMOTED to
// <liveRoot>/rules/<name> before services reload, so the rendered config must
// reference the live location — embedding the staging path leaves the running
// server pointing at a file it may not be able to read (issue #1132).
// When no live root is configured the staging path is used (tests and
// pre-promotion renders). An empty string means the dat is absent everywhere
// and must not be referenced.
func routingDatPath(paths generatedconfig.Paths, name string) string {
	// The live location is <liveRoot>/rules/<name> when a real live root is
	// configured; in single-root compat mode (tests, previews) the generated
	// tree IS the live location.
	live := ""
	if paths.LiveRoot != "" && paths.LiveRoot != paths.ApplyRoot {
		live = filepath.Join(paths.LiveRoot, "rules", name)
	} else {
		live = paths.Generated("rules/" + name)
	}
	staged := paths.Generated("rules/" + name)
	if regularFileExists(live) {
		return live
	}
	if regularFileExists(staged) {
		// Promotion copies rules/*.dat to the live root before reload, so the
		// generated YAML must name the destination, not the source (#1132).
		return live
	}
	// The dat exists nowhere: emit no reference rather than a path that can
	// never load — acl.geoip pointing at a missing file fails config load.
	return ""
}

func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// runtimeCredentialsLimited reports whether any normalized runtime
// credential carries a connection limit. Legacy embedded profiles cannot
// express limits, so only RuntimeCredentials are consulted.
func runtimeCredentialsLimited(creds []model.RuntimeCredential) bool {
	for _, cred := range creds {
		if cred.DeviceLimit != nil || cred.IPLimit != nil {
			return true
		}
	}
	return false
}
