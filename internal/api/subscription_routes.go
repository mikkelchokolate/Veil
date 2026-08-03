package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/clientaccess"
	"github.com/mikkelchokolate/Veil/internal/model"
)

type subscriptionRateBucket struct {
	window int64
	count  int
}

type subscriptionRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]subscriptionRateBucket
}

func (l *subscriptionRateLimiter) allow(token, remote string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = make(map[string]subscriptionRateBucket)
	}
	window := now.Unix() / 60
	digest := sha256.Sum256([]byte(token))
	tokenKey := "token:" + hex.EncodeToString(digest[:])
	host, _, err := net.SplitHostPort(strings.TrimSpace(remote))
	if err != nil {
		host = strings.TrimSpace(remote)
	}
	sourceKey := "source:" + host
	limits := map[string]int{tokenKey: 60, sourceKey: 300}
	updates := make(map[string]subscriptionRateBucket, len(limits))
	for key, limit := range limits {
		bucket := l.buckets[key]
		if bucket.window != window {
			bucket = subscriptionRateBucket{window: window}
		}
		if bucket.count >= limit {
			return false
		}
		bucket.count++
		updates[key] = bucket
	}
	for key, bucket := range updates {
		l.buckets[key] = bucket
	}
	if len(l.buckets) > 8192 {
		for key, bucket := range l.buckets {
			if bucket.window < window {
				delete(l.buckets, key)
			}
		}
		for key := range l.buckets {
			if len(l.buckets) <= 8192 {
				break
			}
			delete(l.buckets, key)
		}
	}
	return true
}

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
	s.mu.Lock()
	runtimeUnknown := s.runtimeVerificationUnknown || s.clientSubsystemStopping
	s.mu.Unlock()
	if runtimeUnknown {
		w.Header().Set("X-Veil-Configuration-State", "recovering")
		writeError(w, "applied runtime is recovering", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/s/")
	if rest == "" || strings.Contains(rest, "/") {
		writeNotFound(w)
		return
	}
	token := rest
	if !s.subscriptionLimiter.allow(token, r.RemoteAddr, time.Now()) {
		writeError(w, "subscription rate limit exceeded", http.StatusTooManyRequests)
		return
	}
	var tok *client.SubscriptionToken
	var err error
	if r.Method == http.MethodHead {
		tok, err = s.tokenStore.LookupReadOnly(token)
	} else {
		tok, err = s.tokenStore.LookupByPlaintext(token)
	}
	if err != nil {
		writeError(w, "subscription lookup failed", http.StatusInternalServerError)
		return
	}
	if tok == nil {
		writeNotFound(w) // unknown/disabled/revoked/expired -> indistinguishable 404
		return
	}
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
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
	quotaPeriodGeneration := int64(0)
	if cl.Client.QuotaResetAt != nil {
		quotaPeriodGeneration = *cl.Client.QuotaResetAt
	}
	w.Header().Set("X-Veil-Quota-Period-Generation", strconv.FormatInt(quotaPeriodGeneration, 10))
	var trafficGeneration int64
	if s.db != nil {
		_ = s.db.QueryRow(`SELECT COALESCE(MAX(bucket_start),0) FROM traffic_buckets WHERE client_id=?`, cl.Client.ID).Scan(&trafficGeneration)
	}
	w.Header().Set("X-Veil-Traffic-Observation-Generation", strconv.FormatInt(trafficGeneration, 10))
	s.writeSubscription(w, r, cl, links)
}

func (s *managementState) appliedClientProjection(revision uint64, payload []byte, clientID string) (managementSnapshot, error) {
	s.appliedProjectionMu.Lock()
	defer s.appliedProjectionMu.Unlock()
	if s.appliedProjectionRevision != revision || s.appliedProjections == nil {
		var full managementSnapshot
		if err := json.Unmarshal(payload, &full); err != nil {
			return managementSnapshot{}, fmt.Errorf("decode applied subscription snapshot: %w", err)
		}
		projections := make(map[string]managementSnapshot, len(full.Clients))
		inboundIDs := make(map[string]map[string]struct{}, len(full.Clients))
		bindingIDs := make(map[string]map[string]struct{}, len(full.Clients))
		for _, row := range full.Clients {
			projections[row.ID] = managementSnapshot{
				SchemaVersion: full.SchemaVersion, EffectiveAt: full.EffectiveAt, Setup: full.Setup,
				Settings: full.Settings, Rules: full.Rules, RoutingPreset: full.RoutingPreset,
				RoutingSource: full.RoutingSource, Warp: full.Warp, Clients: []model.ClientSnapshot{row},
			}
			inboundIDs[row.ID] = make(map[string]struct{})
			bindingIDs[row.ID] = make(map[string]struct{})
		}
		for _, binding := range full.Bindings {
			projection, ok := projections[binding.ClientID]
			if !ok {
				continue
			}
			projection.Bindings = append(projection.Bindings, binding)
			projections[binding.ClientID] = projection
			inboundIDs[binding.ClientID][binding.InboundID] = struct{}{}
			bindingIDs[binding.ClientID][binding.ID] = struct{}{}
		}
		for clientID, projection := range projections {
			for _, inbound := range full.Inbounds {
				if _, ok := inboundIDs[clientID][inbound.Name]; ok {
					projection.Inbounds = append(projection.Inbounds, inbound)
				}
			}
			for _, credential := range full.Credentials {
				if _, ok := bindingIDs[clientID][credential.BindingID]; ok {
					projection.Credentials = append(projection.Credentials, credential)
				}
			}
			projections[clientID] = projection
		}
		s.appliedProjectionRevision = revision
		s.appliedProjections = projections
	}
	projection, ok := s.appliedProjections[clientID]
	if !ok {
		return managementSnapshot{}, client.ErrNotFound
	}
	return projection, nil
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
	snapshot, err := s.appliedClientProjection(revisions.Applied, payload, clientID)
	if err != nil {
		return client.View{}, nil, revisions.Applied, revisions.Desired, err
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
	bindingIDs := make(map[string]struct{})
	inboundIDs := make([]string, 0)
	for _, binding := range snapshot.Bindings {
		if binding.ClientID != clientID {
			continue
		}
		bindings = append(bindings, client.Binding{ID: binding.ID, ClientID: binding.ClientID, InboundID: binding.InboundID,
			RuntimeIdentity: binding.RuntimeIdentity, Enabled: binding.Enabled, ProtocolSettings: binding.ProtocolSettings,
			CreatedAt: binding.CreatedAt, UpdatedAt: binding.UpdatedAt, Version: binding.Version})
		bindingIDs[binding.ID] = struct{}{}
		inboundIDs = append(inboundIDs, binding.InboundID)
	}
	selected := make(map[string]model.CredentialSnapshot, len(bindingIDs))
	for _, credential := range snapshot.Credentials {
		if credential.Kind != "password" {
			continue
		}
		if _, ok := bindingIDs[credential.BindingID]; !ok {
			continue
		}
		previous, ok := selected[credential.BindingID]
		if !ok || credential.CredentialVersion > previous.CredentialVersion {
			selected[credential.BindingID] = credential
		}
	}
	plaintext := make(map[string]string, len(selected))
	for bindingID, credential := range selected {
		value, err := s.clientCreds.RevealEncrypted(credential.EncryptedValue)
		if err != nil {
			return client.View{}, nil, revisions.Applied, revisions.Desired, err
		}
		plaintext[bindingID] = value
	}
	inbounds := make(map[string]client.InboundSnapshot, len(snapshot.Inbounds))
	for _, inbound := range snapshot.Inbounds {
		inbounds[inbound.Name] = client.InboundSnapshot{Name: inbound.Name, Protocol: inbound.Protocol,
			Transport: inbound.Transport, Port: inbound.Port, Enabled: inbound.Enabled,
			Password: inbound.Password, ProtocolFields: inbound.ProtocolFields}
	}
	effectiveAt := snapshot.EffectiveAt
	if effectiveAt == 0 {
		effectiveAt, _ = s.applySnapshots.EffectiveAt(revisions.Applied)
	}
	view := client.View{Client: current, Status: client.ComputeStatus(current, time.Unix(effectiveAt, 0).UTC(), false, false, len(bindings) == 0), InboundIDs: inboundIDs, HasCreds: len(plaintext) > 0}
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
	trafficState := "unsupported"
	if s.trafficStore != nil {
		var trafficErr error
		upload, download, trafficErr = s.trafficStore.TotalsForClient(cl.ID)
		if trafficErr != nil {
			trafficState = "unavailable"
		} else {
			trafficState = "observed"
		}
	}
	w.Header().Set("X-Veil-Traffic-State", trafficState)
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
		issued, err := s.tokenStore.IssueBy(clientID, req.Label, actorFromRequest(r), req.ExpiresAt)
		if err != nil {
			if errors.Is(err, client.ErrNotFound) {
				writeNotFound(w)
			} else if errors.Is(err, client.ErrValidation) {
				writeError(w, err.Error(), http.StatusBadRequest)
			} else {
				writeError(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		issued.URL = s.subscriptionURLFor(issued.Plaintext)
		s.logUserAction(r, "issue_subscription_token", clientID, true, issued.Token.Prefix)
		markIdempotencySecretResponse(w, issued.Token.ID, uint64(issued.Token.CreatedAt))
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
		markIdempotencySecretResponse(w, issued.Token.ID, uint64(issued.Token.CreatedAt))
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
