package caddyassembly

import (
	"net"
	"strconv"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/model"
	veilsettings "github.com/mikkelchokolate/Veil/internal/settings"
)

type CaddyBindOwnerKind string

const (
	CaddyOwnerPanel CaddyBindOwnerKind = "panel"
	CaddyOwnerNaive CaddyBindOwnerKind = "naive"
)

type CaddyBindOwner struct {
	Kind        CaddyBindOwnerKind
	Domain      string
	InboundName string
	Transport   string           // Inbound transport; used only for Naive owner
	BackendPort int              // Panel backend port parsed from PanelListen; used only for Panel owner
	WebBasePath string           // Normalized panel web base path; used only for Panel owner
	NaiveUsers  []CaddyNaiveUser // Only for naive owner
	FallbackRoot string          // Only for naive owner
}

// CaddyNaiveUser is a minimal credential pair for the forward_proxy handler.
type CaddyNaiveUser struct {
	Username string
	Password string
}

type CaddyRenderPlan struct {
	Servers              map[bindregistry.BindKey]CaddyBindOwner
	ACMEChallenges       map[bindregistry.BindKey]AcmeChallengeOwner
	Domains              map[string]CaddyDomainCertSpec
	DefaultChallengeMode string
}

func BuildRenderPlan(
	settings model.Settings,
	inbounds []model.Inbound,
	challengeBinds map[bindregistry.BindKey]AcmeChallengeOwner,
) (CaddyRenderPlan, map[bindregistry.BindKey]bindregistry.BindOwner, error) {
	owners := make(map[bindregistry.BindKey]bindregistry.BindOwner)
	servers := make(map[bindregistry.BindKey]CaddyBindOwner)

	if settings.PanelAccess == "caddy" {
		panelDomain := settings.PanelDomain
		if panelDomain == "" {
			panelDomain = settings.Domain
		}
		if panelDomain != "" {
			key := bindregistry.BindKey{Address: "0.0.0.0", Port: settings.PanelPublicPort, Network: bindregistry.ListenTCP}
			owners[key] = bindregistry.BindOwner{Kind: bindregistry.BindOwnerPanelCaddy, ServiceName: "veil-caddy.service"}
			servers[key] = CaddyBindOwner{
				Kind:        CaddyOwnerPanel,
				Domain:      panelDomain,
				BackendPort: panelBackendPort(settings.PanelListen),
				WebBasePath: veilsettings.NormalizeWebBasePath(settings.WebBasePath),
			}
		}
	}

	for _, inb := range inbounds {
		if inb.Protocol != "naiveproxy" || !inb.Enabled {
			continue
		}
		transport := naiveTransport(inb)
		port := naivePublicPort(settings, inb)
		domain := naiveDomain(inb, settings)
		users := naiveUsers(inb, settings)
		fallbackRoot := naiveFallbackRoot(inb, settings)
		addNaiveBinds(transport, port, domain, inb.Name, users, fallbackRoot, owners, servers)
	}

	domains, err := ResolveDomainCertSpecs(settings, inbounds)
	if err != nil {
		return CaddyRenderPlan{}, nil, err
	}

	return CaddyRenderPlan{
		Servers:              servers,
		ACMEChallenges:       challengeBinds,
		Domains:              domains,
		DefaultChallengeMode: settings.AcmeChallengeMode,
	}, owners, nil
}

func addNaiveBinds(transport string, port int, domain, name string, users []CaddyNaiveUser, fallbackRoot string, owners map[bindregistry.BindKey]bindregistry.BindOwner, servers map[bindregistry.BindKey]CaddyBindOwner) {
	if transport == "tcp" || transport == "dual" {
		key := bindregistry.BindKey{Address: "0.0.0.0", Port: port, Network: bindregistry.ListenTCP}
		owners[key] = bindregistry.BindOwner{Kind: bindregistry.BindOwnerNaive, ServiceName: "veil-caddy.service", InboundName: name}
		servers[key] = CaddyBindOwner{Kind: CaddyOwnerNaive, Domain: domain, InboundName: name, Transport: transport, NaiveUsers: users, FallbackRoot: fallbackRoot}
	}
	if transport == "quic" || transport == "dual" {
		key := bindregistry.BindKey{Address: "0.0.0.0", Port: port, Network: bindregistry.ListenUDP}
		owners[key] = bindregistry.BindOwner{Kind: bindregistry.BindOwnerNaive, ServiceName: "veil-caddy.service", InboundName: name}
		servers[key] = CaddyBindOwner{Kind: CaddyOwnerNaive, Domain: domain, InboundName: name, Transport: transport, NaiveUsers: users, FallbackRoot: fallbackRoot}
	}
}

// naiveTransport mirrors the behavior of the naiveproxy plugin helper so that
// caddyassembly can remain independent of the protocol package and avoid an
// import cycle with the renderer.
func naiveTransport(inbound model.Inbound) string {
	t := stringField(inbound.ProtocolFields, "transport")
	if t == "" {
		return "tcp"
	}
	return t
}

// naivePublicPort mirrors the naiveproxy plugin helper.
func naivePublicPort(settings model.Settings, inbound model.Inbound) int {
	if v, ok := inbound.ProtocolFields["publicPort"]; ok {
		if n, ok := v.(float64); ok {
			return int(n)
		}
		if n, ok := v.(int); ok {
			return n
		}
	}
	if inbound.Port != 0 {
		return inbound.Port
	}
	if settings.DefaultInboundPublicPort != 0 {
		return settings.DefaultInboundPublicPort
	}
	return 443
}

// naiveDomain mirrors the naiveproxy plugin helper, falling back to the global
// settings domain so existing state that stores the domain at the top level
// continues to render.
func naiveDomain(inbound model.Inbound, settings model.Settings) string {
	if d := stringField(inbound.ProtocolFields, "domain"); d != "" {
		return d
	}
	return settings.Domain
}

// naiveFallbackRoot mirrors the naiveproxy plugin helper to avoid an import
// cycle with the renderer. It falls back to the built-in default web root.
func naiveFallbackRoot(inbound model.Inbound, settings model.Settings) string {
	if root := stringField(inbound.ProtocolFields, "fallbackRoot"); root != "" {
		return root
	}
	if inbound.FallbackRoot != "" {
		return inbound.FallbackRoot
	}
	if settings.FallbackRoot != "" {
		return settings.FallbackRoot
	}
	return "/var/lib/veil/www"
}

func naiveUsers(inbound model.Inbound, settings model.Settings) []CaddyNaiveUser {
	var users []CaddyNaiveUser
	for _, p := range inbound.Profiles {
		if !p.Enabled || strings.TrimSpace(p.Username) == "" || strings.TrimSpace(p.Password) == "" {
			continue
		}
		users = append(users, CaddyNaiveUser{Username: p.Username, Password: p.Password})
	}
	if len(users) > 0 {
		return users
	}
	username := stringField(inbound.ProtocolFields, "naiveUsername")
	if username == "" {
		username = settings.NaiveUsername
	}
	password := stringField(inbound.ProtocolFields, "naivePassword")
	if password == "" {
		password = settings.NaivePassword
	}
	if username != "" && password != "" {
		return []CaddyNaiveUser{{Username: username, Password: password}}
	}
	return nil
}

func panelBackendPort(panelListen string) int {
	_, portText, err := net.SplitHostPort(panelListen)
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}
