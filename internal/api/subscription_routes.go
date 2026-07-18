package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/model"
)

// registerSubscriptionRoutes wires the public token-based subscription
// delivery endpoint (/s/{token}) and the authenticated token-management
// endpoints under /api/v1/clients/{id}/tokens.
func (s *managementState) registerSubscriptionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/s/", s.handlePublicSubscription)
}

// handlePublicSubscription serves a client's subscription artifact addressed
// by an unguessable token. It is intentionally OUTSIDE /api/ so the auth
// middleware does not require a session; the token itself is the capability.
//
// Path: /s/{token}[?format=base64|raw]
func (s *managementState) handlePublicSubscription(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, http.MethodGet, http.MethodHead)
		return
	}
	if s.tokenStore == nil || s.subRenderer == nil {
		writeError(w, "subscription store unavailable", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/s/")
	// Strip any sub-path (defensive: only the token segment is used).
	token := rest
	if i := strings.Index(rest, "/"); i >= 0 {
		token = rest[:i]
	}
	if token == "" {
		writeNotFound(w)
		return
	}
	tok, err := s.tokenStore.LookupByPlaintext(token)
	if err != nil {
		writeError(w, "subscription lookup failed", http.StatusInternalServerError)
		return
	}
	if tok == nil {
		writeNotFound(w) // unknown/disabled/revoked/expired -> indistinguishable 404
		return
	}
	cl, err := s.clientService.Get(tok.ClientID)
	if err != nil {
		writeNotFound(w)
		return
	}
	// A disabled, expired, or quota-depleted client gets an empty subscription
	// (the client app shows zero nodes) rather than an error, so revoking
	// access via the client record propagates cleanly.
	links := []model.ClientLink{}
	if cl.Status == client.StatusActive {
		renderer := s.subRenderer.WithSettings(clientaccess.Settings{Domain: s.settings.Domain})
		links, err = renderer.LinksForClient(cl.Client, s.resolveInboundSnapshot)
		if err != nil {
			writeError(w, "subscription render failed", http.StatusInternalServerError)
			return
		}
	}
	s.writeSubscription(w, r, cl, links)
}

// writeSubscription renders links into the requested subscription format with
// the metadata headers proxy clients consume.
func (s *managementState) writeSubscription(w http.ResponseWriter, r *http.Request, cl client.View, links []model.ClientLink) {
	format := r.URL.Query().Get("format")
	response := model.ClientLinksResponse{Links: links, Count: len(links)}
	subscription, err := clientaccess.BuildClientSubscription(response, format)
	if err != nil {
		writeError(w, err.Error(), http.StatusBadRequest)
		return
	}
	clientaccess.NewClientSubscriptionDeliveryHeaders(subscription).Apply(w.Header())
	meta := clientaccess.SubscriptionMetaHeaders{
		Upload:                     0,
		Download:                   0,
		Total:                      cl.QuotaBytes,
		Expire:                     cl.ExpiresAt,
		ProfileUpdateIntervalHours: subscriptionUpdateIntervalHours(),
		ProfileTitle:               subscriptionProfileTitle(cl),
	}
	meta.Apply(w.Header())
	// Optional HTML landing for browsers that open the link directly.
	if wantsHTML(r) {
		s.writeSubscriptionHTML(w, cl, links)
		return
	}
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(subscription.Body))
}

// resolveInboundSnapshot maps a binding's inbound ID to the live inbound
// snapshot used for link rendering.
func (s *managementState) resolveInboundSnapshot(inboundID string) (client.InboundSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, in := range s.inbounds {
		if in.Name == inboundID {
			return client.InboundSnapshot{
				Name:           in.Name,
				Protocol:       in.Protocol,
				Transport:      in.Transport,
				Port:           in.Port,
				Enabled:        in.Enabled,
				Password:       in.Password,
				ProtocolFields: in.ProtocolFields,
			}, true
		}
	}
	return client.InboundSnapshot{}, false
}

// --- authenticated token management ---

func (s *managementState) handleV1ClientTokens(w http.ResponseWriter, r *http.Request, clientID string) {
	if s.tokenStore == nil {
		writeError(w, "token store unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		tokens, err := s.tokenStore.ListForClient(clientID)
		if err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"items": tokens})
	case http.MethodPost:
		var req struct {
			Label     string `json:"label"`
			ExpiresAt *int64 `json:"expiresAt"`
		}
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		issued, err := s.tokenStore.Issue(clientID, req.Label, req.ExpiresAt)
		if err != nil {
			writeError(w, err.Error(), http.StatusInternalServerError)
			return
		}
		issued.URL = s.subscriptionURLFor(issued.Plaintext)
		s.logUserAction(r, "issue_subscription_token", clientID, true, issued.Token.Prefix)
		writeJSONStatus(w, http.StatusCreated, issued)
	default:
		methodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *managementState) handleV1ClientTokenByID(w http.ResponseWriter, r *http.Request, clientID, tokenID, action string) {
	if s.tokenStore == nil {
		writeError(w, "token store unavailable", http.StatusServiceUnavailable)
		return
	}
	if action == "rotate" && r.Method == http.MethodPost {
		issued, err := s.tokenStore.Rotate(tokenID)
		if err != nil {
			s.writeV1ClientError(w, err)
			return
		}
		issued.URL = s.subscriptionURLFor(issued.Plaintext)
		s.logUserAction(r, "rotate_subscription_token", clientID, true, issued.Token.Prefix)
		writeJSON(w, issued)
		return
	}
	if action == "" && r.Method == http.MethodDelete {
		if err := s.tokenStore.Revoke(tokenID); err != nil {
			s.writeV1ClientError(w, err)
			return
		}
		s.logUserAction(r, "revoke_subscription_token", clientID, true, tokenID)
		writeJSON(w, map[string]string{"id": tokenID})
		return
	}
	writeNotFound(w)
}

// subscriptionURLFor builds the absolute subscription URL for a plaintext
// token using the panel's public base. The host is best-effort: clients copy
// the URL from the panel, which knows its own origin.
func (s *managementState) subscriptionURLFor(plaintext string) string {
	s.mu.Lock()
	domain := ""
	if s.settings.Domain != "" {
		domain = s.settings.Domain
	}
	s.mu.Unlock()
	if domain == "" {
		return "/s/" + plaintext
	}
	scheme := "https"
	return scheme + "://" + domain + "/s/" + plaintext
}

func wantsHTML(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return r.URL.Query().Get("format") == "" &&
		strings.Contains(accept, "text/html") &&
		!strings.Contains(accept, "application/json")
}

func subscriptionUpdateIntervalHours() int { return 24 }

func subscriptionProfileTitle(cl client.View) string {
	if cl.Name != "" {
		return cl.Name
	}
	return "Veil"
}

var _ = json.Marshal
