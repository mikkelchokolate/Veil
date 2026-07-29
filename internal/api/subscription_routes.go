package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	cl, links, applied, desired, err := s.appliedSubscription(tok.ClientID)
	if err != nil {
		if errors.Is(err, client.ErrNotFound) {
			writeNotFound(w)
		} else {
			writeError(w, "applied subscription unavailable", http.StatusServiceUnavailable)
		}
		return
	}
	configurationState := "applied"
	if desired != applied {
		configurationState = "stale"
	}
	w.Header().Set("X-Veil-Configuration-State", configurationState)
	w.Header().Set("X-Veil-Applied-Revision", strconv.FormatUint(applied, 10))
	w.Header().Set("X-Veil-Desired-Revision", strconv.FormatUint(desired, 10))
	s.writeSubscription(w, r, cl, links)
}

func (s *managementState) appliedSubscription(clientID string) (client.View, []model.ClientLink, uint64, uint64, error) {
	if s.applyRevisions == nil || s.applySnapshots == nil {
		return client.View{}, nil, 0, 0, errors.New("applied snapshot store unavailable")
	}
	revisions, err := s.applyRevisions.Get()
	if err != nil {
		return client.View{}, nil, 0, 0, err
	}
	if revisions.Applied == 0 {
		return client.View{}, nil, 0, revisions.Desired, errors.New("no applied revision")
	}
	payload, err := s.applySnapshots.Load(revisions.Applied)
	if err != nil {
		return client.View{}, nil, 0, revisions.Desired, err
	}
	var snapshot managementSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return client.View{}, nil, 0, revisions.Desired, fmt.Errorf("decode applied subscription snapshot: %w", err)
	}
	if err := s.decryptSnapshot(&snapshot); err != nil {
		return client.View{}, nil, 0, revisions.Desired, err
	}
	var row *model.ClientSnapshot
	for i := range snapshot.Clients {
		if snapshot.Clients[i].ID == clientID {
			row = &snapshot.Clients[i]
			break
		}
	}
	if row == nil {
		return client.View{}, nil, revisions.Applied, revisions.Desired, client.ErrNotFound
	}
	current := client.Client{ID: row.ID, Name: row.Name, Email: row.Email, Enabled: row.Enabled, GroupID: row.GroupID,
		QuotaBytes: row.QuotaBytes, QuotaResetPolicy: row.QuotaResetPolicy, QuotaResetAt: row.QuotaResetAt,
		ExpiresAt: row.ExpiresAt, DeviceLimit: row.DeviceLimit, Notes: row.Notes, Depleted: row.Depleted,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, Version: row.Version}
	bindings := make([]client.Binding, 0)
	inboundIDs := make([]string, 0)
	for _, binding := range snapshot.Bindings {
		if binding.ClientID != clientID {
			continue
		}
		bindings = append(bindings, client.Binding{ID: binding.ID, ClientID: binding.ClientID, InboundID: binding.InboundID,
			RuntimeIdentity: binding.RuntimeIdentity, Enabled: binding.Enabled, ProtocolSettings: binding.ProtocolSettings,
			CreatedAt: binding.CreatedAt, UpdatedAt: binding.UpdatedAt, Version: binding.Version})
		inboundIDs = append(inboundIDs, binding.InboundID)
	}
	plaintext := make(map[string]string, len(snapshot.Credentials))
	for _, credential := range snapshot.Credentials {
		if credential.Kind != "password" {
			continue
		}
		value, err := s.clientCreds.RevealEncrypted(credential.EncryptedValue)
		if err != nil {
			return client.View{}, nil, revisions.Applied, revisions.Desired, err
		}
		plaintext[credential.BindingID] = value
	}
	inbounds := make(map[string]client.InboundSnapshot, len(snapshot.Inbounds))
	for _, inbound := range snapshot.Inbounds {
		inbounds[inbound.Name] = client.InboundSnapshot{Name: inbound.Name, Protocol: inbound.Protocol,
			Transport: inbound.Transport, Port: inbound.Port, Enabled: inbound.Enabled,
			Password: inbound.Password, ProtocolFields: inbound.ProtocolFields}
	}
	view := client.View{Client: current, Status: client.ComputeStatus(current, time.Now(), false, false, len(bindings) == 0), InboundIDs: inboundIDs, HasCreds: len(plaintext) > 0}
	links := []model.ClientLink{}
	if view.Status == client.StatusActive {
		renderer := s.subRenderer.WithSettings(clientaccess.Settings{Domain: snapshot.Settings.Domain})
		links, err = renderer.LinksForSnapshot(current, bindings, plaintext, func(inboundID string) (client.InboundSnapshot, bool) {
			value, ok := inbounds[inboundID]
			return value, ok
		})
		if err != nil {
			return client.View{}, nil, revisions.Applied, revisions.Desired, err
		}
	}
	return view, links, revisions.Applied, revisions.Desired, nil
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
	var upload, download int64
	if s.trafficStore != nil {
		upload, download, _ = s.trafficStore.TotalsForClient(cl.ID)
	}
	meta := clientaccess.SubscriptionMetaHeaders{
		Upload:                     upload,
		Download:                   download,
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
	token, err := s.tokenStore.Get(tokenID)
	if err != nil || token.ClientID != clientID {
		writeNotFound(w)
		return
	}
	if action == "rotate" && r.Method == http.MethodPost {
		var req struct {
			ExpiresAt patchField[int64] `json:"expiresAt"`
		}
		if r.ContentLength != 0 && !decodeJSONRequest(w, r, &req) {
			return
		}
		var expiry *int64
		if req.ExpiresAt.Present && !req.ExpiresAt.Null {
			value := req.ExpiresAt.Value
			expiry = &value
		}
		if token.ExpiresAt != nil && time.Now().UTC().Unix() >= *token.ExpiresAt &&
			(!req.ExpiresAt.Present || req.ExpiresAt.Null || expiry == nil || *expiry <= time.Now().UTC().Unix()) {
			writeError(w, "rotating an expired token requires a new future expiry", http.StatusBadRequest)
			return
		}
		issued, err := s.tokenStore.RotateWithExpiry(tokenID, expiry, req.ExpiresAt.Present)
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
