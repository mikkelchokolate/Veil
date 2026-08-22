package client

import (
	"fmt"

	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// InboundSnapshot is the minimal inbound shape the subscription builder needs
// to render client links. It is satisfied by model.Inbound via a thin adapter
// so the client package does not depend on the whole management state.
type InboundSnapshot struct {
	Name           string
	Protocol       string
	Transport      string
	Port           int
	Enabled        bool
	Password       string
	ProtocolFields map[string]any
}

// SubscriptionRenderer builds the link set for ONE client from its bindings
// and credentials, reusing the clientaccess per-protocol link builders so URI
// formatting stays consistent with the rest of the panel.
type SubscriptionRenderer struct {
	repo     *Repository
	creds    *CredentialStore
	settings clientaccess.Settings
}

// NewSubscriptionRenderer builds a renderer with no global settings (per-client
// protocols that need a domain will skip links until settings are provided via
// WithSettings).
func NewSubscriptionRenderer(repo *Repository, creds *CredentialStore) *SubscriptionRenderer {
	return &SubscriptionRenderer{repo: repo, creds: creds}
}

// WithSettings returns a copy of the renderer that renders against the given
// global settings (domain etc.). Protocols like Mieru require a settings domain
// to emit a client config; hysteria2/naiveproxy resolve the domain per-inbound.
func (r *SubscriptionRenderer) WithSettings(s clientaccess.Settings) *SubscriptionRenderer {
	cp := *r
	cp.settings = s
	return &cp
}

// LinksForClient renders client links for a client from its enabled bindings
// maps binding.InboundID -> the live inbound snapshot. Bindings whose inbound
// is missing, disabled, or whose credential cannot be revealed are skipped so
// a single broken binding does not poison the whole subscription.
func (r *SubscriptionRenderer) LinksForClient(c Client, resolve func(inboundID string) (InboundSnapshot, bool)) ([]model.ClientLink, error) {
	bindings, err := r.repo.BindingsForClient(c.ID)
	if err != nil {
		return nil, fmt.Errorf("client: subscription bindings: %w", err)
	}
	plaintext := make(map[string]string, len(bindings))
	for _, binding := range bindings {
		cred, err := r.creds.ActiveForBinding(binding.ID, "password")
		if err != nil {
			continue
		}
		value, err := r.creds.Reveal(cred.ID)
		if err == nil {
			plaintext[binding.ID] = value
		}
	}
	return r.LinksForSnapshot(c, bindings, plaintext, resolve)
}

// LinksForSnapshot renders exclusively from immutable binding and credential
// material supplied by the caller.
func (r *SubscriptionRenderer) LinksForSnapshot(c Client, bindings []Binding, plaintext map[string]string, resolve func(inboundID string) (InboundSnapshot, bool)) ([]model.ClientLink, error) {
	registry := clientaccess.NewClientAccessProtocolRegistry()
	var out []model.ClientLink
	for _, b := range bindings {
		if !b.Enabled {
			continue
		}
		snap, ok := resolve(b.InboundID)
		if !ok || !snap.Enabled {
			continue
		}
		password, ok := plaintext[b.ID]
		if !ok {
			continue
		}
		identity := b.RuntimeIdentity
		if identity == "" {
			return nil, fmt.Errorf("client: binding %s has no runtime identity", b.ID)
		}
		inbound := snapshotToInbound(snap)
		cc := clientaccess.ClientCredential{Name: c.Name, Username: identity, Password: password}
		links := registry.BuildLinks(r.settings, inbound, []clientaccess.ClientCredential{cc})
		out = append(out, links...)
	}
	return out, nil
}

func snapshotToInbound(s InboundSnapshot) model.Inbound {
	return model.Inbound{
		Name:           s.Name,
		Protocol:       s.Protocol,
		Transport:      s.Transport,
		Port:           s.Port,
		Enabled:        s.Enabled,
		Password:       s.Password,
		ProtocolFields: s.ProtocolFields,
	}
}
