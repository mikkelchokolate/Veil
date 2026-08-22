package api

import "github.com/mikkelchokolate/Veil/internal/model"

// viewerSettingsMetadata is deliberately allowlist-based. In particular it
// has no password/auth fields and no arbitrary protocolFields map, because a
// future plugin field must not become viewer-readable by accident.
type viewerSettingsMetadata struct {
	PanelListen              string `json:"panelListen"`
	PanelAccess              string `json:"panelAccess,omitempty"`
	WebBasePath              string `json:"webBasePath,omitempty"`
	Mode                     string `json:"mode"`
	Domain                   string `json:"domain,omitempty"`
	Email                    string `json:"email,omitempty"`
	Hysteria2Insecure        bool   `json:"hysteria2Insecure,omitempty"`
	MasqueradeURL            string `json:"masqueradeURL,omitempty"`
	FallbackRoot             string `json:"fallbackRoot,omitempty"`
	OlcrtcTransport          string `json:"olcrtcTransport,omitempty"`
	PanelDomain              string `json:"panelDomain,omitempty"`
	PanelEmail               string `json:"panelEmail,omitempty"`
	PanelPublicPort          int    `json:"panelPublicPort,omitempty"`
	DefaultAcmeEmail         string `json:"defaultAcmeEmail,omitempty"`
	DefaultInboundPublicPort int    `json:"defaultInboundPublicPort,omitempty"`
	AcmeChallengeMode        string `json:"acmeChallengeMode,omitempty"`
	FirewallManagement       *bool  `json:"firewallManagement,omitempty"`
}

func newViewerSettingsMetadata(settings model.Settings) viewerSettingsMetadata {
	var firewall *bool
	if settings.FirewallManagement != nil {
		value := *settings.FirewallManagement
		firewall = &value
	}
	return viewerSettingsMetadata{
		PanelListen: settings.PanelListen, PanelAccess: settings.PanelAccess,
		WebBasePath: settings.WebBasePath, Mode: settings.Mode,
		Domain: settings.Domain, Email: settings.Email,
		Hysteria2Insecure: settings.Hysteria2Insecure,
		MasqueradeURL:     settings.MasqueradeURL, FallbackRoot: settings.FallbackRoot,
		OlcrtcTransport: settings.OlcrtcTransport,
		PanelDomain:     settings.PanelDomain, PanelEmail: settings.PanelEmail,
		PanelPublicPort:          settings.PanelPublicPort,
		DefaultAcmeEmail:         settings.DefaultAcmeEmail,
		DefaultInboundPublicPort: settings.DefaultInboundPublicPort,
		AcmeChallengeMode:        settings.AcmeChallengeMode,
		FirewallManagement:       firewall,
	}
}

type viewerWarpMetadata struct {
	Enabled       bool   `json:"enabled"`
	Endpoint      string `json:"endpoint"`
	LocalAddress  string `json:"localAddress,omitempty"`
	PeerPublicKey string `json:"peerPublicKey,omitempty"`
	Reserved      []int  `json:"reserved,omitempty"`
	SocksListen   string `json:"socksListen,omitempty"`
	SocksPort     int    `json:"socksPort,omitempty"`
	MTU           int    `json:"mtu,omitempty"`
}

func newViewerWarpMetadata(warp model.WarpConfig) viewerWarpMetadata {
	return viewerWarpMetadata{
		Enabled: warp.Enabled, Endpoint: warp.Endpoint,
		LocalAddress: warp.LocalAddress, PeerPublicKey: warp.PeerPublicKey,
		Reserved:    append([]int(nil), warp.Reserved...),
		SocksListen: warp.SocksListen, SocksPort: warp.SocksPort, MTU: warp.MTU,
	}
}
