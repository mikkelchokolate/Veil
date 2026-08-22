package caddyassembly

import (
	"fmt"
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
	Kind         CaddyBindOwnerKind
	Domain       string
	PanelDomain  string // Set when a naive inbound shares the bind with panel access
	InboundName  string
	Transport    string           // Inbound transport; used only for Naive owner
	BackendPort  int              // Panel backend port parsed from PanelListen; used only for Panel owner (or merged naive owner)
	WebBasePath  string           // Normalized panel web base path; used only for Panel owner (or merged naive owner)
	NaiveUsers   []CaddyNaiveUser // Only for naive owner
	FallbackRoot string           // Only for naive owner
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
		panelPublicPort := settings.PanelPublicPort
		if panelPublicPort == 0 {
			panelPublicPort = 443
		}
		if panelDomain != "" {
			key := bindregistry.BindKey{Address: "0.0.0.0", Port: panelPublicPort, Network: bindregistry.ListenTCP}
			if err := setOwner(owners, key, bindregistry.BindOwner{Kind: bindregistry.BindOwnerPanelCaddy, ServiceName: "veil-caddy.service"}); err != nil {
				return CaddyRenderPlan{}, nil, err
			}
			if err := setServer(servers, key, CaddyBindOwner{
				Kind:        CaddyOwnerPanel,
				Domain:      panelDomain,
				BackendPort: panelBackendPort(settings.PanelListen),
				WebBasePath: veilsettings.NormalizeWebBasePath(settings.WebBasePath),
			}); err != nil {
				return CaddyRenderPlan{}, nil, err
			}
		}
	}

	for _, inb := range inbounds {
		if inb.Protocol != "naiveproxy" || !inb.Enabled {
			continue
		}
		transport := naiveTransport(inb)
		if transport != "tcp" {
			return CaddyRenderPlan{}, nil, fmt.Errorf("naive inbound %q uses unsupported transport %q (only tcp is supported)", inb.Name, transport)
		}
		port := naivePublicPort(settings, inb)
		domain := naiveDomain(inb, settings)
		users := naiveUsers(inb, settings)
		fallbackRoot := naiveFallbackRoot(inb, settings)
		if err := addNaiveBinds(transport, port, domain, inb.Name, users, fallbackRoot, owners, servers); err != nil {
			return CaddyRenderPlan{}, nil, err
		}
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

// BuildFinalRenderPlan constructs the complete Caddy render plan including ACME
// challenge binds. It first builds the server-owner map, plans challenge binds
// based on those owners, merges challenge owners into the global owner map, and
// returns the final plan along with any validation issues raised by challenge
// planning.
func BuildFinalRenderPlan(
	settings model.Settings,
	inbounds []model.Inbound,
) (CaddyRenderPlan, map[bindregistry.BindKey]bindregistry.BindOwner, []model.ValidationIssue, error) {
	plan, owners, err := BuildRenderPlan(settings, inbounds, nil)
	if err != nil {
		return CaddyRenderPlan{}, nil, nil, err
	}

	challengeBinds, issues := PlanAcmeChallengeBinds(settings.AcmeChallengeMode, plan.Domains, owners)
	for key := range challengeBinds {
		owners[key] = bindregistry.BindOwner{Kind: bindregistry.BindOwnerAcmeChallenge, ServiceName: "veil-caddy.service"}
	}
	plan.ACMEChallenges = challengeBinds
	return plan, owners, issues, nil
}

func addNaiveBinds(transport string, port int, domain, name string, users []CaddyNaiveUser, fallbackRoot string, owners map[bindregistry.BindKey]bindregistry.BindOwner, servers map[bindregistry.BindKey]CaddyBindOwner) error {
	if transport == "tcp" || transport == "dual" {
		key := bindregistry.BindKey{Address: "0.0.0.0", Port: port, Network: bindregistry.ListenTCP}
		if err := mergeNaiveBind(key, domain, name, transport, users, fallbackRoot, owners, servers); err != nil {
			return err
		}
	}
	if transport == "quic" || transport == "dual" {
		key := bindregistry.BindKey{Address: "0.0.0.0", Port: port, Network: bindregistry.ListenUDP}
		if err := setOwner(owners, key, bindregistry.BindOwner{Kind: bindregistry.BindOwnerNaive, ServiceName: "veil-caddy.service", InboundName: name}); err != nil {
			return err
		}
		if err := setServer(servers, key, CaddyBindOwner{Kind: CaddyOwnerNaive, Domain: domain, InboundName: name, Transport: transport, NaiveUsers: users, FallbackRoot: fallbackRoot}); err != nil {
			return err
		}
	}
	return nil
}

func mergeNaiveBind(key bindregistry.BindKey, domain, name, transport string, users []CaddyNaiveUser, fallbackRoot string, owners map[bindregistry.BindKey]bindregistry.BindOwner, servers map[bindregistry.BindKey]CaddyBindOwner) error {
	newOwner := bindregistry.BindOwner{Kind: bindregistry.BindOwnerNaive, ServiceName: "veil-caddy.service", InboundName: name}
	newServer := CaddyBindOwner{Kind: CaddyOwnerNaive, Domain: domain, InboundName: name, Transport: transport, NaiveUsers: users, FallbackRoot: fallbackRoot}

	existingOwner, ownerExists := owners[key]
	existingServer, serverExists := servers[key]
	if !ownerExists {
		owners[key] = newOwner
		servers[key] = newServer
		return nil
	}

	if existingOwner.Kind == bindregistry.BindOwnerPanelCaddy && serverExists && existingServer.Kind == CaddyOwnerPanel {
		newServer.PanelDomain = existingServer.Domain
		newServer.BackendPort = existingServer.BackendPort
		newServer.WebBasePath = existingServer.WebBasePath
		owners[key] = newOwner
		servers[key] = newServer
		return nil
	}

	if existingOwner == newOwner {
		return nil
	}
	return fmt.Errorf("%s %s:%d is already owned by %s %s", key.Network, key.Address, key.Port, existingOwner.Kind, existingOwner.InboundName)
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
	return model.ResolveInboundDomain(inbound, settings)
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
	runtimeUsers := make(map[string]CaddyNaiveUser, len(inbound.RuntimeCredentials))
	for _, credential := range inbound.RuntimeCredentials {
		username := strings.TrimSpace(credential.Username)
		password := strings.TrimSpace(credential.Password)
		if username != "" && password != "" {
			runtimeUsers[username] = CaddyNaiveUser{Username: username, Password: password}
		}
	}
	for _, p := range inbound.Profiles {
		if !p.Enabled || strings.TrimSpace(p.Username) == "" || strings.TrimSpace(p.Password) == "" {
			continue
		}
		if _, replaced := runtimeUsers[p.Username]; !replaced {
			users = append(users, CaddyNaiveUser{Username: p.Username, Password: p.Password})
		}
	}
	for _, credential := range inbound.RuntimeCredentials {
		if user, ok := runtimeUsers[strings.TrimSpace(credential.Username)]; ok {
			users = append(users, user)
			delete(runtimeUsers, user.Username)
		}
	}
	if len(users) > 0 {
		return users
	}
	username := stringField(inbound.ProtocolFields, "naiveUsername")
	if username == "" {
		username = strings.TrimSpace(inbound.NaiveUsername)
	}
	if username == "" {
		username = stringField(settings.ProtocolFields, "naiveUsername")
	}
	if username == "" {
		username = strings.TrimSpace(settings.NaiveUsername)
	}
	if username == "" {
		username = model.DefaultNaiveUsername
	}
	password := strings.TrimSpace(inbound.Password)
	if password == "" {
		password = stringField(inbound.ProtocolFields, "naivePassword")
	}
	if password == "" {
		password = strings.TrimSpace(inbound.NaivePassword)
	}
	if password == "" {
		password = stringField(settings.ProtocolFields, "naivePassword")
	}
	if password == "" {
		password = settings.NaivePassword
	}
	if username != "" && password != "" {
		return []CaddyNaiveUser{{Username: username, Password: password}}
	}
	return nil
}

func setOwner(owners map[bindregistry.BindKey]bindregistry.BindOwner, key bindregistry.BindKey, owner bindregistry.BindOwner) error {
	if existing, ok := owners[key]; ok && existing != owner {
		return fmt.Errorf("%s %s:%d is already owned by %s %s", key.Network, key.Address, key.Port, existing.Kind, existing.InboundName)
	}
	owners[key] = owner
	return nil
}

func setServer(servers map[bindregistry.BindKey]CaddyBindOwner, key bindregistry.BindKey, server CaddyBindOwner) error {
	if existing, ok := servers[key]; ok {
		if existing.Kind != server.Kind || existing.InboundName != server.InboundName {
			return fmt.Errorf("%s %s:%d already has a %s server", key.Network, key.Address, key.Port, existing.Kind)
		}
	}
	servers[key] = server
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
