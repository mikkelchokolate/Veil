package client

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// Service orchestrates client use-cases: CRUD, bindings, credentials, bulk
// operations, and the computed effective status. It owns transactions.
//
// Apply orchestration (revision bump + immutable snapshot + exactly one apply
// job) is owned by the HTTP layer (managementState), not by this service:
// a fire-and-forget notifier caused double-applies and mutations whose
// response carried no revision/job. Handlers call applyAfterClientMutation
// exactly once after a successful commit.
type Service struct {
	repo  *Repository
	creds *CredentialStore
	now   func() int64
	// inboundLookup resolves an inbound's protocol capabilities by inbound ID
	// (name) for the enriched binding read model. Optional; when nil the
	// view falls back to bare inboundIds.
	inboundLookup func(inboundID string) *BindingCapability
}

// WithInboundLookup attaches a resolver that maps an inbound ID to its
// protocol capabilities, enabling the enriched bindings read model on views.
func (s *Service) WithInboundLookup(fn func(inboundID string) *BindingCapability) *Service {
	s.inboundLookup = fn
	return s
}

// NewService builds the client service.
func NewService(repo *Repository, creds *CredentialStore) *Service {
	return &Service{repo: repo, creds: creds, now: nowUnix}
}

// View is the API-facing representation of a client with its computed status.
type View struct {
	Client
	Status     EffectiveStatus `json:"status"`
	InboundIDs []string        `json:"inboundIds,omitempty"`
	HasCreds   bool            `json:"hasCredentials"`
	// Bindings is the full binding read model: each bound inbound with its id,
	// enabled flag and the protocol capabilities relevant to a client binding.
	// Empty when no inbound lookup was attached to the service.
	Bindings []BindingView `json:"bindings,omitempty"`
}

// BindingView describes one client->inbound binding enriched with the
// protocol capabilities of the bound inbound, so the UI/API consumer knows
// what a client on this inbound supports (per-client credentials, transports).
type BindingView struct {
	ID         string             `json:"id"`
	InboundID  string             `json:"inboundId"`
	Enabled    bool               `json:"enabled"`
	Version    int                `json:"version"`
	Capability *BindingCapability `json:"capability,omitempty"`
	// Credential is metadata-only (configured/kind/version/rotatedAt); never
	// any encrypted or plaintext material.
	Credential *CredentialMeta `json:"credential,omitempty"`
}

// CredentialMeta describes the active credential for a binding without any
// secret material.
type CredentialMeta struct {
	Configured bool   `json:"configured"`
	Kind       string `json:"kind,omitempty"`
	Version    int    `json:"version,omitempty"`
	RotatedAt  *int64 `json:"rotatedAt,omitempty"`
}

// BindingCapability captures the protocol capabilities of a bound inbound.
type BindingCapability struct {
	Protocol             string   `json:"protocol"`
	Transports           []string `json:"transports"`
	PerClientCredentials bool     `json:"perClientCredentials"`
	RequiresCaddy        bool     `json:"requiresCaddy"`
}

// ErrValidation marks a 400-class client-side validation failure.
var ErrValidation = errors.New("client: validation error")

// MaxQuotaBytes caps quotaBytes at Number.MAX_SAFE_INTEGER (2^53-1). The API
// serializes quotas as JSON numbers and the SPA consumes them as JS numbers;
// larger int64 values would silently lose precision on the client. Values
// above the cap are rejected end-to-end (OpenAPI maximum, this validation,
// and the SPA's form schemas) instead of being represented imprecisely.
const MaxQuotaBytes int64 = 1<<53 - 1

func validate(c Client) error {
	if c.Name == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	if c.QuotaBytes != nil {
		if *c.QuotaBytes < 0 {
			return fmt.Errorf("%w: quotaBytes must be >= 0", ErrValidation)
		}
		if *c.QuotaBytes > MaxQuotaBytes {
			return fmt.Errorf("%w: quotaBytes must be <= %d (Number.MAX_SAFE_INTEGER)", ErrValidation, MaxQuotaBytes)
		}
	}
	switch c.QuotaResetPolicy {
	case "", ResetNever, ResetDaily, ResetWeekly, ResetMonthly:
	default:
		return fmt.Errorf("%w: invalid quotaResetPolicy %q", ErrValidation, c.QuotaResetPolicy)
	}
	return nil
}

// Create validates and creates a client. The caller (HTTP layer) is
// responsible for the post-commit apply orchestration.
func (s *Service) Create(c Client) (View, error) {
	if err := validate(c); err != nil {
		return View{}, err
	}
	created, err := s.repo.Create(c)
	if err != nil {
		return View{}, err
	}
	return s.toView(created)
}

// BindingInput pairs an inbound ID with an optional plaintext credential for
// transactional client creation.
type BindingInput struct {
	InboundID  string
	Credential string
}

// CreateWithBindings atomically creates a client plus its bindings and
// credentials in ONE SQL transaction. If any
// binding or credential fails the whole mutation rolls back — the client is
// never persisted half-configured, no apply runs for a partial state, and no
// compensating deletes are used. This is the required path for client create;
// the separate Create/AddBinding/SetCredential sequence is legacy.
func (s *Service) CreateWithBindings(c Client, bindings []BindingInput) (View, error) {
	view, _, err := s.CreateWithBindingsIssued(c, bindings)
	return view, err
}

// IssuedCredential is a credential generated server-side during client
// creation. The plaintext is returned exactly once (never persisted; only the
// encrypted form is stored) so the operator can deliver it to the end user.
type IssuedCredential struct {
	BindingID string `json:"bindingId"`
	InboundID string `json:"inboundId"`
	Kind      string `json:"kind"`
	Plaintext string `json:"plaintext"`
}

// CreateWithBindingsIssued is CreateWithBindings plus server-side credential
// generation: for every binding whose credential is empty a protocol-
// compatible high-entropy secret is generated, encrypted, and persisted inside
// the SAME SQL transaction, and its plaintext is returned exactly once in the
// IssuedCredential list. Errors never suppress a partial create — any failure
// rolls the whole transaction back.
func (s *Service) CreateWithBindingsIssued(c Client, bindings []BindingInput) (View, []IssuedCredential, error) {
	var createdID string
	var issued []IssuedCredential
	err := s.repo.WithTx(func(tx *Tx) error {
		id, iss, err := s.CreateWithBindingsIssuedTx(tx, c, bindings)
		createdID, issued = id, iss
		return err
	})
	if err != nil {
		return View{}, nil, err
	}
	// Build the view AFTER commit. Building it inside the transaction would
	// call the inbound-capability lookup, which acquires the management-state
	// mutex — a self-deadlock when the API layer wraps the whole mutation in
	// that same lock.
	view, err := s.Get(createdID)
	if err != nil {
		return View{}, nil, err
	}
	return view, issued, nil
}

// CreateWithBindingsIssuedTx is the transactional core of
// CreateWithBindingsIssued for the unified mutation orchestration: the API
// layer folds the create, the desired-revision bump, and the immutable
// snapshot into ONE SQLite transaction through this entry point. It returns
// the created client ID; the caller builds the response view after commit.
func (s *Service) CreateWithBindingsIssuedTx(tx *Tx, c Client, bindings []BindingInput) (string, []IssuedCredential, error) {
	if err := validate(c); err != nil {
		return "", nil, err
	}
	for _, b := range bindings {
		if b.InboundID == "" {
			return "", nil, fmt.Errorf("%w: inboundId is required", ErrValidation)
		}
	}
	created, err := tx.CreateClient(c)
	if err != nil {
		return "", nil, err
	}
	issued := []IssuedCredential{}
	for _, b := range bindings {
		bind, err := tx.CreateBinding(Binding{ClientID: created.ID, InboundID: b.InboundID, Enabled: true})
		if err != nil {
			return "", nil, err
		}
		plaintext := b.Credential
		generated := false
		if plaintext == "" {
			plaintext, err = generateCredentialPlaintext()
			if err != nil {
				return "", nil, err
			}
			generated = true
		}
		if _, err := tx.SetCredential(s.creds, bind.ID, "password", plaintext); err != nil {
			return "", nil, err
		}
		if generated {
			issued = append(issued, IssuedCredential{
				BindingID: bind.ID,
				InboundID: bind.InboundID,
				Kind:      "password",
				Plaintext: plaintext,
			})
		}
	}
	return created.ID, issued, nil
}

// UpdateTx is the transactional variant of Update for the unified mutation
// orchestration. It performs the optimistic-concurrency update only; callers
// build the response view AFTER commit (the view's inbound-capability lookup
// takes the management-state mutex, which the API layer already holds around
// the whole mutation).
func (s *Service) UpdateTx(tx *Tx, c Client, version int) error {
	if err := validate(c); err != nil {
		return err
	}
	_, err := tx.Update(c, version)
	return err
}

// DeleteTx is the transactional variant of Delete for the unified mutation
// orchestration.
func (s *Service) DeleteTx(tx *Tx, id string) error { return tx.Delete(id) }

// AddBindingTx is the transactional variant of AddBinding.
func (s *Service) AddBindingTx(tx *Tx, clientID, inboundID string) (Binding, error) {
	if inboundID == "" {
		return Binding{}, fmt.Errorf("%w: inboundId is required", ErrValidation)
	}
	return tx.CreateBinding(Binding{ClientID: clientID, InboundID: inboundID, Enabled: true})
}

// RemoveBindingTx is the transactional variant of RemoveBinding.
func (s *Service) RemoveBindingTx(tx *Tx, bindingID, clientID string) error {
	return tx.DeleteBinding(bindingID)
}

// SetCredentialTx is the transactional variant of SetCredential.
func (s *Service) SetCredentialTx(tx *Tx, bindingID, kind, plaintext string) (Credential, error) {
	if plaintext == "" {
		return Credential{}, fmt.Errorf("%w: credential value is required", ErrValidation)
	}
	return tx.SetCredential(s.creds, bindingID, kind, plaintext)
}

// RotateCredentialTx is the transactional variant of RotateCredential.
func (s *Service) RotateCredentialTx(tx *Tx, bindingID, kind, plaintext string) (Credential, error) {
	if plaintext == "" {
		return Credential{}, fmt.Errorf("%w: credential value is required", ErrValidation)
	}
	return tx.RotateCredential(s.creds, bindingID, kind, plaintext)
}

// RotateCredentialGeneratedTx is the transactional variant of
// RotateCredentialGenerated: the server generates the high-entropy plaintext
// and returns it exactly once.
func (s *Service) RotateCredentialGeneratedTx(tx *Tx, bindingID, kind string) (GeneratedCredential, error) {
	plaintext, err := generateCredentialPlaintext()
	if err != nil {
		return GeneratedCredential{}, err
	}
	c, err := s.RotateCredentialTx(tx, bindingID, kind, plaintext)
	if err != nil {
		return GeneratedCredential{}, err
	}
	return GeneratedCredential{Credential: c, Plaintext: plaintext}, nil
}

// SetBindingEnabledTx is the transactional variant of SetBindingEnabled.
func (s *Service) SetBindingEnabledTx(tx *Tx, bindingID string, enabled bool, version int) (Binding, error) {
	b, err := tx.GetBinding(bindingID)
	if err != nil {
		return Binding{}, err
	}
	b.Enabled = enabled
	updated, err := tx.UpdateBinding(b, version)
	if err != nil {
		return Binding{}, err
	}
	return updated, nil
}

// Get returns one client with computed status.
func (s *Service) Get(id string) (View, error) {
	c, err := s.repo.Get(id)
	if err != nil {
		return View{}, ErrNotFound
	}
	return s.toView(c)
}

// List returns a page of clients with computed status.
func (s *Service) List(f ListFilter) ([]View, int, error) {
	items, total, err := s.repo.List(f)
	if err != nil {
		return nil, 0, err
	}
	views := make([]View, 0, len(items))
	for _, c := range items {
		v, err := s.toView(c)
		if err != nil {
			return nil, 0, err
		}
		views = append(views, v)
	}
	return views, total, nil
}

// Update applies an optimistic-locking update.
func (s *Service) Update(c Client, version int) (View, error) {
	if err := validate(c); err != nil {
		return View{}, err
	}
	updated, err := s.repo.Update(c, version)
	if err != nil {
		return View{}, err
	}
	return s.toView(updated)
}

// Delete removes a client (cascading bindings + credentials).
func (s *Service) Delete(id string) error {
	return s.repo.Delete(id)
}

// AddBinding binds a client to an inbound.
func (s *Service) AddBinding(clientID, inboundID string) (Binding, error) {
	if inboundID == "" {
		return Binding{}, fmt.Errorf("%w: inboundId is required", ErrValidation)
	}
	return s.repo.CreateBinding(Binding{ClientID: clientID, InboundID: inboundID, Enabled: true})
}

// RemoveBinding deletes a binding. The client survives as an orphan.
func (s *Service) RemoveBinding(bindingID, clientID string) error {
	return s.repo.DeleteBinding(bindingID)
}

// SetCredential encrypts and stores a credential for a binding.
func (s *Service) SetCredential(bindingID, kind, plaintext string) (Credential, error) {
	if plaintext == "" {
		return Credential{}, fmt.Errorf("%w: credential value is required", ErrValidation)
	}
	return s.creds.Set(bindingID, kind, plaintext)
}

// RotateCredential rotates a binding's credential to a new version.
func (s *Service) RotateCredential(bindingID, kind, plaintext string) (Credential, error) {
	if plaintext == "" {
		return Credential{}, fmt.Errorf("%w: credential value is required", ErrValidation)
	}
	return s.creds.Rotate(bindingID, kind, plaintext)
}

// GeneratedCredential pairs the rotated credential metadata with the one-time
// plaintext the server generated. The plaintext is returned exactly once and
// never persisted (only the encrypted form is stored).
type GeneratedCredential struct {
	Credential Credential `json:"credential"`
	Plaintext  string     `json:"plaintext"`
}

// RotateCredentialGenerated rotates a credential with a server-generated
// high-entropy plaintext, returning the new plaintext once. This is the
// preferred rotate path (capability-driven): the caller never supplies the
// secret and it is shown to the operator a single time.
func (s *Service) RotateCredentialGenerated(bindingID, kind string) (GeneratedCredential, error) {
	plaintext, err := generateCredentialPlaintext()
	if err != nil {
		return GeneratedCredential{}, err
	}
	c, err := s.RotateCredential(bindingID, kind, plaintext)
	if err != nil {
		return GeneratedCredential{}, err
	}
	return GeneratedCredential{Credential: c, Plaintext: plaintext}, nil
}

// generateCredentialPlaintext returns a URL-safe 256-bit random secret.
func generateCredentialPlaintext() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("client: generate credential: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// SetBindingEnabled toggles a binding's enabled flag with optimistic locking.
func (s *Service) SetBindingEnabled(bindingID string, enabled bool, version int) (Binding, error) {
	b, err := s.repo.GetBinding(bindingID)
	if err != nil {
		return Binding{}, err
	}
	b.Enabled = enabled
	updated, err := s.repo.UpdateBinding(b, version)
	if err != nil {
		return Binding{}, err
	}
	return updated, nil
}

// BindingCredential pairs a normalized client's resolved credential with its
// identity, for render-time injection into an inbound's runtime access model.
type BindingCredential struct {
	Name     string
	Username string
	Password string
}

// CredentialsForInbound resolves the active credential plaintext for every
// enabled binding on the given inbound. It is the bridge from the normalized
// Client+Binding+Credential store to the runtime renderer: the returned
// credentials are merged into the inbound's access model so normalized clients
// reach the live config. Only enabled clients and enabled bindings contribute.
func (s *Service) CredentialsForInbound(inboundID string) ([]BindingCredential, error) {
	clients, _, err := s.repo.List(ListFilter{})
	if err != nil {
		return nil, err
	}
	out := []BindingCredential{}
	for _, c := range clients {
		if !c.Enabled {
			continue
		}
		bindings, err := s.repo.BindingsForClient(c.ID)
		if err != nil {
			return nil, err
		}
		for _, b := range bindings {
			if b.InboundID != inboundID || !b.Enabled {
				continue
			}
			creds, err := s.creds.ListForBinding(b.ID)
			if err != nil {
				return nil, err
			}
			for _, cr := range creds {
				if cr.RevokedAt != nil {
					continue
				}
				plaintext, rerr := s.creds.Reveal(cr.ID)
				if rerr != nil {
					return nil, rerr
				}
				out = append(out, BindingCredential{Name: c.Name, Username: c.Name, Password: plaintext})
			}
		}
	}
	return out, nil
}

func (s *Service) toView(c Client) (View, error) {
	return s.viewWith(c, s.repo.BindingsForClient, s.creds.ActiveForBinding, s.creds.ListForBinding)
}

func (s *Service) viewWith(c Client,
	bindingsFn func(clientID string) ([]Binding, error),
	activeFn func(bindingID, kind string) (Credential, error),
	listFn func(bindingID string) ([]Credential, error),
) (View, error) {
	bindings, err := bindingsFn(c.ID)
	if err != nil {
		return View{}, err
	}
	inbounds := make([]string, 0, len(bindings))
	bindingViews := make([]BindingView, 0, len(bindings))
	hasCreds := false
	for _, b := range bindings {
		inbounds = append(inbounds, b.InboundID)
		bv := BindingView{ID: b.ID, InboundID: b.InboundID, Enabled: b.Enabled, Version: b.Version}
		if s.inboundLookup != nil {
			bv.Capability = s.inboundLookup(b.InboundID)
		}
		if active, err := activeFn(b.ID, "password"); err == nil && active.ID != "" {
			bv.Credential = &CredentialMeta{
				Configured: true,
				Kind:       active.Kind,
				Version:    active.CredentialVersion,
				RotatedAt:  active.RotatedAt,
			}
		}
		bindingViews = append(bindingViews, bv)
		if !hasCreds {
			creds, err := listFn(b.ID)
			if err == nil && len(creds) > 0 {
				hasCreds = true
			}
		}
	}
	status := ComputeStatus(c, timeFromUnix(s.now()), false, false, len(bindings) == 0)
	return View{Client: c, Status: status, InboundIDs: inbounds, HasCreds: hasCreds, Bindings: bindingViews}, nil
}
