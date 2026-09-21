package generatedconfig

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/hostenv"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
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

func (r InboundRenderer) RenderNaive(inbound Inbound, includePanel bool) (string, error) {
	var buf strings.Builder
	buf.WriteString("{\n  order forward_proxy before file_server\n  servers {\n    protocols h1 h2\n  }\n}\n\n")

	password := naivePassword(r.settings, inbound)
	access, err := clientaccess.BuildClientAccess(r.settings, inbound)
	if err != nil {
		return "", err
	}
	username := naiveUsername(r.settings, inbound)
	root := fallbackRoot(r.settings, inbound)
	naiveConfig := renderer.NaiveConfig{
		Domain:       model.ResolveInboundDomain(inbound, r.settings),
		Email:        model.ResolveInboundEmail(inbound, r.settings),
		ListenPort:   inbound.Port,
		Username:     username,
		Password:     password,
		Users:        access.NaiveUsers(),
		FallbackRoot: root,
		// The render paths' etc root (derived from the configured live root)
		// is authoritative for the fallback tree: a custom --etc-dir install
		// serves its own <etc>/www, not the packaged one (issue #634). When
		// the paths carry no etc root the renderer resolves the host's.
		FallbackBase: r.fallbackBase(),
	}
	if r.warp.Enabled {
		socksPort := r.warp.SocksPort
		if socksPort == 0 {
			socksPort = 40000
		}
		naiveConfig.Upstream = "socks5://" + net.JoinHostPort(r.warp.SocksDialAddr(), strconv.Itoa(socksPort))
	}
	if includePanel && r.settings.PanelAccess == "caddy" {
		if route, ok, err := panelCaddyRoute(r.settings); err == nil && ok {
			naiveConfig.PanelPort = route.Port
			naiveConfig.WebBasePath = route.WebBasePath
		}
	}
	block, err := renderer.RenderNaiveCaddyfile(naiveConfig)
	if err != nil {
		return "", err
	}
	if idx := strings.Index(block, "\n:"); idx != -1 {
		block = block[idx+1:]
	} else if idx := strings.Index(block, "\n"+r.settings.Domain); idx != -1 {
		block = block[idx+1:]
	}
	buf.WriteString(block)
	buf.WriteString("\n")
	return buf.String(), nil
}

func (r InboundRenderer) RenderPanelStandalone() (string, error) {
	if r.settings.PanelAccess != "caddy" {
		return "", nil
	}
	route, ok, err := panelCaddyRoute(r.settings)
	if err != nil || !ok {
		return "", err
	}
	var buf strings.Builder
	buf.WriteString("{\n  order forward_proxy before file_server\n  servers {\n    protocols h1 h2\n  }\n}\n\n")

	panelBlock, err := renderer.RenderPanelCaddyfile(renderer.PanelCaddyConfig{
		Domain:      r.settings.Domain,
		Email:       r.settings.Email,
		PanelPort:   route.Port,
		WebBasePath: route.WebBasePath,
	})
	if err != nil {
		return "", err
	}
	buf.WriteString(panelBlock)
	buf.WriteString("\n")
	return buf.String(), nil
}

func (r InboundRenderer) RenderHysteria2(inbound Inbound) (string, error) {
	password := hysteria2Password(r.settings, inbound)
	access, err := clientaccess.BuildClientAccess(r.settings, inbound)
	if err != nil {
		return "", err
	}
	url := masqueradeURL(r.settings, inbound)
	domain := model.ResolveInboundDomain(inbound, r.settings)
	hystConfig := renderer.Hysteria2Config{
		ListenPort:    inbound.Port,
		Domain:        domain,
		Password:      password,
		Users:         access.Hysteria2Users(),
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

func RenderNaiveInbound(settings Settings, inbound Inbound, warp WarpConfig, includePanel bool) (string, error) {
	return NewInboundRenderer(settings, Paths{}, warp).RenderNaive(inbound, includePanel)
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

// fallbackBase resolves the managed naive fallback web root base from the
// configured render paths. Empty means "resolve from the host environment".
func (r InboundRenderer) fallbackBase() string {
	if etcRoot := strings.TrimSpace(r.paths.etcRoot()); etcRoot != "" {
		return filepath.Join(etcRoot, "www")
	}
	return ""
}

type caddyRoute struct {
	Port        int
	WebBasePath string
}

func panelCaddyRoute(settings Settings) (caddyRoute, bool, error) {
	if settings.PanelAccess != "caddy" {
		return caddyRoute{}, false, nil
	}
	webBasePath := veilsettings.NormalizeWebBasePath(settings.WebBasePath)
	if webBasePath == "" {
		return caddyRoute{}, false, fmt.Errorf("webBasePath is required for caddy Panel access")
	}
	_, portText, err := net.SplitHostPort(settings.PanelListen)
	if err != nil {
		return caddyRoute{}, false, fmt.Errorf("panelListen must be host:port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return caddyRoute{}, false, fmt.Errorf("panelListen must be host:port")
	}
	return caddyRoute{Port: port, WebBasePath: webBasePath}, true, nil
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

func naiveUsername(settings Settings, inbound Inbound) string {
	return model.EffectiveProtocolString(inbound, settings, "naiveUsername", inbound.NaiveUsername, settings.NaiveUsername)
}

func naivePassword(settings Settings, inbound Inbound) string {
	return model.EffectiveProtocolPassword(inbound, settings, "naivePassword", inbound.NaivePassword, settings.NaivePassword)
}

func fallbackRoot(settings Settings, inbound Inbound) string {
	root := protocolString(inbound.ProtocolFields, "fallbackRoot", "")
	if root == "" {
		root = inbound.FallbackRoot
	}
	if root == "" {
		root = protocolString(settings.ProtocolFields, "fallbackRoot", "")
	}
	if root == "" {
		root = settings.FallbackRoot
	}
	return root
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
