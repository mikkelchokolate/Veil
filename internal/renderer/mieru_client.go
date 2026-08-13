package renderer

import (
	"encoding/json"
	"hash/fnv"
	"net"
	"strings"
)

type MieruClientConfig struct {
	ProfileName   string
	DomainName    string
	PortBindings  []MieruPortBinding
	User          MieruUser
	Socks5Port    int
	HTTPProxyPort int
	RPCPort       int
}

type mieruClientConfigJSON struct {
	ActiveProfile string                   `json:"activeProfile"`
	Profiles      []mieruClientProfileJSON `json:"profiles"`
	Socks5Port    int                      `json:"socks5Port"`
	HTTPProxyPort int                      `json:"httpProxyPort,omitempty"`
	RPCPort       int                      `json:"rpcPort"`
}

type mieruClientProfileJSON struct {
	ProfileName string              `json:"profileName"`
	User        mieruUserJSON       `json:"user"`
	Servers     []mieruClientServer `json:"servers"`
}

type mieruClientServer struct {
	IPAddress    string                 `json:"ipAddress,omitempty"`
	DomainName   string                 `json:"domainName,omitempty"`
	PortBindings []mieruPortBindingJSON `json:"portBindings"`
}

func RenderMieruClient(cfg MieruClientConfig) (string, error) {
	server := mieruClientServer{PortBindings: mieruClientPortBindings(cfg.PortBindings)}
	endpoint := strings.TrimSpace(cfg.DomainName)
	if ip := net.ParseIP(strings.Trim(endpoint, "[]")); ip != nil {
		server.IPAddress = ip.String()
	} else {
		server.DomainName = endpoint
	}
	body := mieruClientConfigJSON{
		ActiveProfile: cfg.ProfileName,
		Profiles: []mieruClientProfileJSON{{
			ProfileName: cfg.ProfileName,
			User:        mieruUserJSON{Name: cfg.User.Name, Password: cfg.User.Password},
			Servers:     []mieruClientServer{server},
		}},
		// The upstream mieru client rejects socks5Port < 1 at apply/run time
		// ("socks5 port number 0 is invalid"), so a zero value must be
		// replaced with a deterministic port in [1024, 65535] derived from
		// the profile. Deterministic (not a free-port probe) keeps generated
		// configs stable and renderers side-effect free; the client listens on
		// 127.0.0.1 only, so collisions are cosmetic for the operator.
		Socks5Port:    cfg.Socks5Port,
		HTTPProxyPort: cfg.HTTPProxyPort,
		RPCPort:       cfg.RPCPort,
	}
	if body.Socks5Port < 1 {
		body.Socks5Port = mieruDefaultSocks5Port(cfg.ProfileName, cfg.User.Name, endpoint)
	}
	if body.RPCPort < 0 {
		body.RPCPort = 0
	}
	encoded, err := json.MarshalIndent(body, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded) + "\n", nil
}

// mieruDefaultSocks5Port derives a stable local SOCKS5 port for the client
// config when the caller did not provide one. FNV-1a over the profile identity
// maps into [1024, 65535]; the port is deterministic across renders so that
// config diffs stay empty and subscriptions are stable.
func mieruDefaultSocks5Port(profileName, username, endpoint string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(profileName + "\x00" + username + "\x00" + endpoint))
	return 1024 + int(h.Sum32()%(65535-1024+1))
}

func mieruClientPortBindings(bindings []MieruPortBinding) []mieruPortBindingJSON {
	out := make([]mieruPortBindingJSON, 0, len(bindings))
	for _, binding := range bindings {
		out = append(out, mieruPortBindingJSON{Port: binding.Port, Protocol: normalizeMieruProtocol(binding.Protocol)})
	}
	return out
}
