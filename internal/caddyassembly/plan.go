package caddyassembly

import (
	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/model"
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
}

type CaddyRenderPlan struct {
	Servers        map[bindregistry.BindKey]CaddyBindOwner
	ACMEChallenges map[bindregistry.BindKey]AcmeChallengeOwner
	Domains        map[string]CaddyDomainCertSpec
}

func BuildRenderPlan(
	settings model.Settings,
	inbounds []model.Inbound,
	challengeBinds map[bindregistry.BindKey]AcmeChallengeOwner,
) (CaddyRenderPlan, map[bindregistry.BindKey]bindregistry.BindOwner, error) {
	owners := make(map[bindregistry.BindKey]bindregistry.BindOwner)
	servers := make(map[bindregistry.BindKey]CaddyBindOwner)

	if settings.PanelAccess == "caddy" && settings.PanelDomain != "" {
		key := bindregistry.BindKey{Address: "0.0.0.0", Port: settings.PanelPublicPort, Network: bindregistry.ListenTCP}
		owners[key] = bindregistry.BindOwner{Kind: bindregistry.BindOwnerPanelCaddy, ServiceName: "veil-caddy.service"}
		servers[key] = CaddyBindOwner{Kind: CaddyOwnerPanel, Domain: settings.PanelDomain}
	}

	for _, inb := range inbounds {
		if inb.Protocol != "naiveproxy" || !inb.Enabled {
			continue
		}
		transport := naiveTransport(inb)
		port := naivePublicPort(settings, inb)
		domain := naiveDomain(inb)
		addNaiveBinds(transport, port, domain, inb.Name, owners, servers)
	}

	domains, err := ResolveDomainCertSpecs(settings, inbounds)
	if err != nil {
		return CaddyRenderPlan{}, nil, err
	}

	return CaddyRenderPlan{
		Servers:        servers,
		ACMEChallenges: challengeBinds,
		Domains:        domains,
	}, owners, nil
}

func addNaiveBinds(transport string, port int, domain, name string, owners map[bindregistry.BindKey]bindregistry.BindOwner, servers map[bindregistry.BindKey]CaddyBindOwner) {
	if transport == "tcp" || transport == "dual" {
		key := bindregistry.BindKey{Address: "0.0.0.0", Port: port, Network: bindregistry.ListenTCP}
		owners[key] = bindregistry.BindOwner{Kind: bindregistry.BindOwnerNaive, ServiceName: "veil-caddy.service", InboundName: name}
		servers[key] = CaddyBindOwner{Kind: CaddyOwnerNaive, Domain: domain, InboundName: name}
	}
	if transport == "quic" || transport == "dual" {
		key := bindregistry.BindKey{Address: "0.0.0.0", Port: port, Network: bindregistry.ListenUDP}
		owners[key] = bindregistry.BindOwner{Kind: bindregistry.BindOwnerNaive, ServiceName: "veil-caddy.service", InboundName: name}
		servers[key] = CaddyBindOwner{Kind: CaddyOwnerNaive, Domain: domain, InboundName: name}
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

// naiveDomain mirrors the naiveproxy plugin helper.
func naiveDomain(inbound model.Inbound) string {
	return stringField(inbound.ProtocolFields, "domain")
}
