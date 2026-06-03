package generatedconfig

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/renderer"
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

type InboundRenderer struct {
	settings Settings
	paths    Paths
	warp     WarpConfig
}

func NewInboundRenderer(settings Settings, paths Paths, warp WarpConfig) InboundRenderer {
	return InboundRenderer{settings: settings, paths: paths, warp: warp}
}

func (r InboundRenderer) RenderNaive(inbound Inbound) (string, error) {
	return r.RenderNaiveSet([]Inbound{inbound})
}

func (r InboundRenderer) RenderNaiveSet(inbounds []Inbound) (string, error) {
	if len(inbounds) == 0 {
		return "", nil
	}
	var buf strings.Builder
	buf.WriteString("{\n  order forward_proxy before file_server\n  servers {\n    protocols h1 h2\n  }\n}\n\n")

	hasInboundOn443 := false
	for _, inbound := range inbounds {
		if inbound.Port == 443 {
			hasInboundOn443 = true
			break
		}
	}

	for _, inbound := range inbounds {
		password := inbound.Password
		if password == "" {
			password = inbound.NaivePassword
			if password == "" {
				password = r.settings.NaivePassword
			}
		}
		access, err := clientaccess.BuildClientAccess(r.settings, inbound)
		if err != nil {
			return "", err
		}
		username := inbound.NaiveUsername
		if username == "" {
			username = r.settings.NaiveUsername
		}
		fallbackRoot := inbound.FallbackRoot
		if fallbackRoot == "" {
			fallbackRoot = r.settings.FallbackRoot
		}
		naiveConfig := renderer.NaiveConfig{
			Domain:       r.settings.Domain,
			Email:        r.settings.Email,
			ListenPort:   inbound.Port,
			Username:     username,
			Password:     password,
			Users:        access.NaiveUsers(),
			FallbackRoot: fallbackRoot,
		}
		if r.warp.Enabled {
			socksPort := r.warp.SocksPort
			if socksPort == 0 {
				socksPort = 40000
			}
			naiveConfig.Upstream = fmt.Sprintf("socks5://127.0.0.1:%d", socksPort)
		}
		if r.settings.PanelAccess == "caddy" {
			if inbound.Port == 443 || (!hasInboundOn443 && buf.Len() <= 75) {
				if route, ok, err := panelCaddyRoute(r.settings); err == nil && ok {
					naiveConfig.PanelPort = route.Port
					naiveConfig.WebBasePath = route.WebBasePath
				}
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
	}

	if r.settings.PanelAccess == "caddy" && !hasInboundOn443 {
		if route, ok, err := panelCaddyRoute(r.settings); err == nil && ok {
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
		}
	}
	return buf.String(), nil
}

func (r InboundRenderer) RenderHysteria2(inbound Inbound) (string, error) {
	password := inbound.Password
	if password == "" {
		password = inbound.Hysteria2Password
		if password == "" {
			password = r.settings.Hysteria2Password
		}
	}
	access, err := clientaccess.BuildClientAccess(r.settings, inbound)
	if err != nil {
		return "", err
	}
	masqueradeURL := inbound.MasqueradeURL
	if masqueradeURL == "" {
		masqueradeURL = r.settings.MasqueradeURL
	}
	hystConfig := renderer.Hysteria2Config{
		ListenPort:    inbound.Port,
		Domain:        r.settings.Domain,
		Password:      password,
		Users:         access.Hysteria2Users(),
		MasqueradeURL: masqueradeURL,
	}
	if r.warp.Enabled {
		socksPort := r.warp.SocksPort
		if socksPort == 0 {
			socksPort = 40000
		}
		hystConfig.Upstream = fmt.Sprintf("127.0.0.1:%d", socksPort)
	}
	return renderer.RenderHysteria2(hystConfig)
}

func (r InboundRenderer) RenderOlcrtc(inbound Inbound) (string, error) {
	password := inbound.Password
	if password == "" {
		bytes := make([]byte, 32)
		if _, err := rand.Read(bytes); err != nil {
			return "", err
		}
		password = hex.EncodeToString(bytes)
	}
	transport := inbound.OlcrtcTransport
	if transport == "" {
		transport = r.settings.OlcrtcTransport
		if transport == "" {
			transport = inbound.Transport
		}
	}
	auth := inbound.OlcrtcAuth
	if auth == "" {
		auth = r.settings.OlcrtcAuth
	}
	roomID := inbound.OlcrtcRoomID
	if roomID == "" {
		roomID = r.settings.OlcrtcRoomID
	}
	return renderer.RenderOlcrtc(renderer.OlcrtcConfig{
		Auth:      auth,
		RoomID:    roomID,
		Key:       password,
		Transport: transport,
		DNS:       r.settings.Domain,
	})
}

func RenderNaiveInbound(settings Settings, inbound Inbound, warp WarpConfig) (string, error) {
	return NewInboundRenderer(settings, Paths{}, warp).RenderNaive(inbound)
}

func RenderHysteria2Inbound(settings Settings, inbound Inbound, warp WarpConfig) (string, error) {
	return NewInboundRenderer(settings, Paths{}, warp).RenderHysteria2(inbound)
}

func RenderOlcrtcInbound(settings Settings, inbound Inbound, warp WarpConfig) (string, error) {
	return NewInboundRenderer(settings, Paths{}, warp).RenderOlcrtc(inbound)
}

func (r InboundRenderer) Paths() Paths {
	return r.paths
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
