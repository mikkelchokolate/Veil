package generatedconfig

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"strconv"

	"github.com/veil-panel/veil/internal/clientaccess"
	"github.com/veil-panel/veil/internal/renderer"
	veilsettings "github.com/veil-panel/veil/internal/settings"
)

type InboundRenderer struct {
	settings Settings
	paths    Paths
}

func NewInboundRenderer(settings Settings, paths Paths) InboundRenderer {
	return InboundRenderer{settings: settings, paths: paths}
}

func (r InboundRenderer) RenderNaive(inbound Inbound) (string, error) {
	password := inbound.Password
	if password == "" {
		password = r.settings.NaivePassword
	}
	access, err := clientaccess.BuildClientAccess(r.settings, inbound)
	if err != nil {
		return "", err
	}
	naiveConfig := renderer.NaiveConfig{
		Domain:       r.settings.Domain,
		Email:        r.settings.Email,
		ListenPort:   inbound.Port,
		Username:     r.settings.NaiveUsername,
		Password:     password,
		Users:        access.NaiveUsers(),
		FallbackRoot: r.settings.FallbackRoot,
	}
	if route, ok, err := panelCaddyRoute(r.settings); err != nil {
		return "", err
	} else if ok {
		naiveConfig.PanelPort = route.Port
		naiveConfig.WebBasePath = route.WebBasePath
	}
	return renderer.RenderNaiveCaddyfile(naiveConfig)
}

func (r InboundRenderer) RenderHysteria2(inbound Inbound) (string, error) {
	password := inbound.Password
	if password == "" {
		password = r.settings.Hysteria2Password
	}
	access, err := clientaccess.BuildClientAccess(r.settings, inbound)
	if err != nil {
		return "", err
	}
	return renderer.RenderHysteria2(renderer.Hysteria2Config{
		ListenPort:    inbound.Port,
		Domain:        r.settings.Domain,
		Password:      password,
		Users:         access.Hysteria2Users(),
		MasqueradeURL: r.settings.MasqueradeURL,
	})
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
	transport := r.settings.OlcrtcTransport
	if transport == "" {
		transport = inbound.Transport
	}
	return renderer.RenderOlcrtc(renderer.OlcrtcConfig{
		Auth:      r.settings.OlcrtcAuth,
		RoomID:    r.settings.OlcrtcRoomID,
		Key:       password,
		Transport: transport,
		DNS:       r.settings.Domain,
	})
}

func RenderNaiveInbound(settings Settings, inbound Inbound) (string, error) {
	return NewInboundRenderer(settings, Paths{}).RenderNaive(inbound)
}

func RenderHysteria2Inbound(settings Settings, inbound Inbound) (string, error) {
	return NewInboundRenderer(settings, Paths{}).RenderHysteria2(inbound)
}

func RenderOlcrtcInbound(settings Settings, inbound Inbound) (string, error) {
	return NewInboundRenderer(settings, Paths{}).RenderOlcrtc(inbound)
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
