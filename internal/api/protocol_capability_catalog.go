package api

type ProtocolCapability struct {
	Protocol               string
	DisplayName            string
	Transports             []string
	FirewallService        string
	RequiresCaddy          bool
	GeneratedConfigPath    string
	GeneratedConfigSuffix  string
	ApplyAction            string
	RuntimeUnit            string
	ValidateInboundRender  bool
	RequiresRenderSettings bool
	RequiresCaddySettings  bool
	ValidationName         string
	ValidationCommand      func(string) []string
}

type ProtocolCapabilityCatalog struct {
	capabilities []ProtocolCapability
}

func NewProtocolCapabilityCatalog() ProtocolCapabilityCatalog {
	runtimes := NewManagedRuntimeCatalog()
	naiveAction, _ := runtimes.ApplyAction("naiveproxy")
	hysteriaAction, _ := runtimes.ApplyAction("hysteria2")
	mieruAction, _ := runtimes.ApplyAction("mieru")
	return ProtocolCapabilityCatalog{capabilities: []ProtocolCapability{
		{
			Protocol:               "naiveproxy",
			DisplayName:            "NaiveProxy",
			Transports:             []string{"tcp"},
			FirewallService:        "Veil NaiveProxy",
			RequiresCaddy:          true,
			GeneratedConfigPath:    "/etc/veil/generated/caddy/Caddyfile",
			GeneratedConfigSuffix:  "/generated/caddy/Caddyfile",
			ApplyAction:            naiveAction,
			RuntimeUnit:            "veil-naive.service",
			ValidateInboundRender:  true,
			RequiresRenderSettings: true,
			RequiresCaddySettings:  true,
			ValidationName:         "caddy",
			ValidationCommand:      func(path string) []string { return []string{"caddy", "validate", "--config", path} },
		},
		{
			Protocol:               "hysteria2",
			DisplayName:            "Hysteria2",
			Transports:             []string{"udp"},
			FirewallService:        "Veil Hysteria2",
			GeneratedConfigPath:    "/etc/veil/generated/hysteria2/server.yaml",
			GeneratedConfigSuffix:  "/generated/hysteria2/server.yaml",
			ApplyAction:            hysteriaAction,
			RuntimeUnit:            "veil-hysteria2.service",
			ValidateInboundRender:  true,
			RequiresRenderSettings: true,
			ValidationName:         "hysteria2",
			ValidationCommand:      func(path string) []string { return []string{"hysteria", "server", "--config", path, "--check"} },
		},
		{
			Protocol:              "mieru",
			DisplayName:           "Mieru",
			Transports:            []string{"tcp", "udp"},
			FirewallService:       "Veil Mieru",
			GeneratedConfigPath:   "/etc/veil/generated/mieru/server_config.json",
			GeneratedConfigSuffix: "/generated/mieru/server_config.json",
			ApplyAction:           mieruAction,
			RuntimeUnit:           "veil-mieru.service",
			ValidateInboundRender: true,
			ValidationName:        "mieru",
			ValidationCommand:     func(path string) []string { return []string{"mieru", "check", "-c", path} },
		},
	}}
}

func (c ProtocolCapabilityCatalog) All() []ProtocolCapability {
	out := make([]ProtocolCapability, len(c.capabilities))
	for i, capability := range c.capabilities {
		capability.Transports = append([]string(nil), capability.Transports...)
		out[i] = capability
	}
	return out
}

func (c ProtocolCapabilityCatalog) ForProtocol(protocol string) (ProtocolCapability, bool) {
	for _, capability := range c.capabilities {
		if capability.Protocol == protocol {
			capability.Transports = append([]string(nil), capability.Transports...)
			return capability, true
		}
	}
	return ProtocolCapability{}, false
}
