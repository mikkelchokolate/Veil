package generatedconfig

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/renderer"
)

// randRead is swapped in tests to exercise the olcRTC password generation error path.
var randRead = rand.Read

type InboundRenderer struct {
	settings Settings
	paths    Paths
	warp     WarpConfig
}

func NewInboundRenderer(settings Settings, paths Paths, warp WarpConfig) InboundRenderer {
	return InboundRenderer{settings: settings, paths: paths, warp: warp}
}

func (r InboundRenderer) RenderHysteria2(inbound Inbound) (string, error) {
	password := hysteria2Password(r.settings, inbound)
	access, err := clientaccess.BuildClientAccess(r.settings, inbound)
	if err != nil {
		return "", err
	}
	url := masqueradeURL(r.settings, inbound)
	domain := model.ResolveInboundDomain(inbound, r.settings)
	hyUsers := access.Hysteria2Users()
	if len(hyUsers) == 0 && (inbound.HadClientProfiles()) {
		// Credential-managed inbound with every client revoked must not fall
		// back to the shared inbound password (issue #1098). The sentinel
		// account renders a syntactically valid config that authenticates no
		// one.
		user, pass := model.RevokedClientCredential(r.settings, inbound)
		hyUsers = []renderer.Hysteria2User{{Username: user, Password: pass}}
	}
	hystConfig := renderer.Hysteria2Config{
		ListenPort:    inbound.Port,
		Domain:        domain,
		Password:      password,
		Users:         hyUsers,
		MasqueradeURL: url,
	}
	if domain != "" && (r.settings.PanelAccess == "caddy" || model.InboundDomain(inbound) != "") {
		hystConfig.CertPath = r.paths.CertPath(domain)
		hystConfig.KeyPath = r.paths.KeyPath(domain)
	} else {
		hystConfig.CertPath = r.paths.PanelCertPath()
		hystConfig.KeyPath = r.paths.PanelKeyPath()
	}
	if r.warp.Enabled {
		socksPort := r.warp.SocksPort
		if socksPort == 0 {
			socksPort = 40000
		}
		hystConfig.Upstream = net.JoinHostPort(r.warp.SocksDialAddr(), strconv.Itoa(socksPort))
	}
	return renderer.RenderHysteria2(hystConfig)
}

func (r InboundRenderer) RenderOlcrtc(inbound Inbound) (string, error) {
	password := inbound.Password
	if password == "" {
		bytes := make([]byte, 32)
		if _, err := randRead(bytes); err != nil {
			return "", err
		}
		password = hex.EncodeToString(bytes)
	}
	// olcRTC transport is a WebRTC channel type (datachannel, vp8channel, …),
	// never the inbound's L4 transport ("udp"). Leave empty so the renderer
	// defaults to "datachannel".
	auth := olcrtcAuth(r.settings, inbound)
	roomID := olcrtcRoomID(r.settings, inbound)
	transport := olcrtcTransport(r.settings, inbound)
	// net.dns is a DNS *resolver* (host:port), not our server domain. Leave
	// empty so the renderer defaults to a public resolver (1.1.1.1:53).
	return renderer.RenderOlcrtc(renderer.OlcrtcConfig{
		Auth:      auth,
		RoomID:    roomID,
		Key:       password,
		Transport: transport,
		DNS:       "",
	})
}

func RenderHysteria2Inbound(settings Settings, inbound Inbound, warp WarpConfig) (string, error) {
	return NewInboundRenderer(settings, NewPaths(hostenv.EtcDir()), warp).RenderHysteria2(inbound)
}

func RenderOlcrtcInbound(settings Settings, inbound Inbound, warp WarpConfig) (string, error) {
	return NewInboundRenderer(settings, Paths{}, warp).RenderOlcrtc(inbound)
}

func (r InboundRenderer) Paths() Paths {
	return r.paths
}

// protocolString reads a string value from a ProtocolFields map, falling back
// to the supplied default when the map is nil, the key is missing, or the value
// is not a string. The result is trimmed of surrounding whitespace.
func protocolString(m map[string]any, key, fallback string) string {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return strings.TrimSpace(s)
}

func hysteria2Password(settings Settings, inbound Inbound) string {
	return model.EffectiveProtocolPassword(inbound, settings, "hysteria2Password", inbound.Hysteria2Password, settings.Hysteria2Password)
}

func masqueradeURL(settings Settings, inbound Inbound) string {
	url := protocolString(inbound.ProtocolFields, "masqueradeURL", "")
	if url == "" {
		url = inbound.MasqueradeURL
	}
	if url == "" {
		url = protocolString(settings.ProtocolFields, "masqueradeURL", "")
	}
	if url == "" {
		url = settings.MasqueradeURL
	}
	return url
}

func olcrtcAuth(settings Settings, inbound Inbound) string {
	auth := protocolString(inbound.ProtocolFields, "olcrtcAuth", "")
	if auth == "" {
		auth = inbound.OlcrtcAuth
	}
	if auth == "" {
		auth = protocolString(settings.ProtocolFields, "olcrtcAuth", "")
	}
	if auth == "" {
		auth = settings.OlcrtcAuth
	}
	if auth == "" {
		auth = "jitsi"
	}
	return auth
}

func olcrtcTransport(settings Settings, inbound Inbound) string {
	transport := protocolString(inbound.ProtocolFields, "olcrtcTransport", "")
	if transport == "" {
		transport = inbound.OlcrtcTransport
	}
	if transport == "" {
		transport = protocolString(settings.ProtocolFields, "olcrtcTransport", "")
	}
	if transport == "" {
		transport = settings.OlcrtcTransport
	}
	if transport == "" {
		transport = "datachannel"
	}
	return transport
}

func olcrtcRoomID(settings Settings, inbound Inbound) string {
	room := protocolString(inbound.ProtocolFields, "olcrtcRoomID", "")
	if room == "" {
		room = inbound.OlcrtcRoomID
	}
	if room == "" {
		room = protocolString(settings.ProtocolFields, "olcrtcRoomID", "")
	}
	if room == "" {
		room = settings.OlcrtcRoomID
	}
	return room
}
