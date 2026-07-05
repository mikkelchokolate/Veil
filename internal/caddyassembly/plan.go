package caddyassembly

import (
	"github.com/mikkelchokolate/Veil/internal/bindregistry"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/protocols/naiveproxy"
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
		transport := naiveproxy.NaiveTransport(inb)
		port := naiveproxy.NaivePublicPort(settings, inb)
		domain := naiveproxy.NaiveDomain(inb)
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
