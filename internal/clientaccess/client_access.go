package clientaccess

import "github.com/mikkelchokolate/Veil/internal/renderer"

type ClientAccess struct {
	settings    Settings
	inbound     Inbound
	credentials []ClientCredential
}

// ClientAccessOption customizes how client access is built.
type ClientAccessOption func(*clientAccessConfig)

type clientAccessConfig struct {
	extra []ClientCredential
}

// WithCredentials merges additional credentials (typically resolved from the
// normalized Client+Binding+Credential store) into the access model. The
// normalized store is the single source of truth: a normalized credential
// overrides a legacy inbound-embedded profile with the same username.
func WithCredentials(extra []ClientCredential) ClientAccessOption {
	return func(c *clientAccessConfig) { c.extra = extra }
}

func BuildClientAccess(settings Settings, inbound Inbound, opts ...ClientAccessOption) (ClientAccess, error) {
	credentials, err := BuildClientCredentials(inbound)
	if err != nil {
		return ClientAccess{}, err
	}
	var cfg clientAccessConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	extra := cfg.extra
	// Auto-merge any runtime credentials resolved from the normalized client
	// store into this inbound (renderer populates Inbound.RuntimeCredentials).
	for _, rc := range inbound.RuntimeCredentials {
		extra = append(extra, ClientCredential{Name: rc.Name, Username: rc.Username, Password: rc.Password})
	}
	credentials = mergeCredentials(credentials, extra)
	return ClientAccess{settings: settings, inbound: inbound, credentials: credentials}, nil
}

// mergeCredentials returns base credentials with extra credentials merged in.
// An extra credential replaces a base credential with the same username
// (normalized store wins); otherwise it is appended.
func mergeCredentials(base, extra []ClientCredential) []ClientCredential {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]bool, len(extra))
	for _, e := range extra {
		seen[e.Username] = true
	}
	out := make([]ClientCredential, 0, len(base)+len(extra))
	for _, b := range base {
		if !seen[b.Username] {
			out = append(out, b)
		}
	}
	out = append(out, extra...)
	return out
}

func (a ClientAccess) ClientLinks() []ClientLink {
	return NewClientAccessProtocolRegistry().BuildLinks(a.settings, a.inbound, a.credentials)
}

func (a ClientAccess) NaiveUsers() []renderer.NaiveUser {
	users := make([]renderer.NaiveUser, 0, len(a.credentials))
	for _, credential := range a.credentials {
		users = append(users, renderer.NaiveUser{Username: credential.Username, Password: credential.Password})
	}
	return users
}

func (a ClientAccess) Hysteria2Users() []renderer.Hysteria2User {
	users := make([]renderer.Hysteria2User, 0, len(a.credentials))
	for _, credential := range a.credentials {
		users = append(users, renderer.Hysteria2User{Username: credential.Username, Password: credential.Password})
	}
	return users
}
