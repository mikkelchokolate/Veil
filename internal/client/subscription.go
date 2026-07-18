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
	repo  *Repository
	creds *CredentialStore
}

func NewSubscriptionRenderer(repo *Repository, creds *CredentialStore) *SubscriptionRenderer {
	return &SubscriptionRenderer{repo: repo, creds: creds}
}

// LinkForClient renders the client links for a client given a resolver that
// maps binding.InboundID -> the live inbound snapshot. Bindings whose inbound
// is missing, disabled, or whose credential cannot be revealed are skipped so
// a single broken binding does not poison the whole subscription.
func (r *SubscriptionRenderer) LinksForClient(c Client, resolve func(inboundID string) (InboundSnapshot, bool)) ([]model.ClientLink, error) {
	bindings, err := r.repo.BindingsForClient(c.ID)
	if err != nil {
		return nil, fmt.Errorf("client: subscription bindings: %w", err)
	}
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
		cred, err := r.creds.ActiveForBinding(b.ID, "password")
		if err != nil {
			continue // no credential for this binding -> skip silently
		}
		plaintext, err := r.creds.Reveal(cred.ID)
		if err != nil {
			continue
		}
		inbound := snapshotToInbound(snap)
		cc := clientaccess.ClientCredential{
			Name:     c.Name,
			Username: c.Name,
			Password: plaintext,
		}
		links := registry.BuildLinks(model.Settings{}, inbound, []clientaccess.ClientCredential{cc})
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
