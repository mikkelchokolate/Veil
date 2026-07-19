package client

import (
	"errors"
	"fmt"
)

// Service orchestrates client use-cases: CRUD, bindings, credentials, bulk
// operations, and the computed effective status. It owns transactions and
// coordinates with an apply notifier so mutations trigger config apply.
type Service struct {
	repo     *Repository
	creds    *CredentialStore
	notifier ApplyNotifier
	now      func() int64
}

// ApplyNotifier is invoked after a mutation that changes desired config so a
// durable apply can be triggered. It is optional (nil disables).
type ApplyNotifier interface {
	NotifyMutation(kind, clientID string)
}

// NewService builds the client service.
func NewService(repo *Repository, creds *CredentialStore, notifier ApplyNotifier) *Service {
	return &Service{repo: repo, creds: creds, notifier: notifier, now: nowUnix}
}

// View is the API-facing representation of a client with its computed status.
type View struct {
	Client
	Status     EffectiveStatus `json:"status"`
	InboundIDs []string        `json:"inboundIds,omitempty"`
	HasCreds   bool            `json:"hasCredentials"`
}

// ErrValidation marks a 400-class client-side validation failure.
var ErrValidation = errors.New("client: validation error")

func validate(c Client) error {
	if c.Name == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	if c.QuotaBytes != nil && *c.QuotaBytes < 0 {
		return fmt.Errorf("%w: quotaBytes must be >= 0", ErrValidation)
	}
	switch c.QuotaResetPolicy {
	case "", ResetNever, ResetDaily, ResetWeekly, ResetMonthly, ResetFixedInterval:
	default:
		return fmt.Errorf("%w: invalid quotaResetPolicy %q", ErrValidation, c.QuotaResetPolicy)
	}
	return nil
}

// Create validates and creates a client, then triggers an apply.
func (s *Service) Create(c Client) (View, error) {
	if err := validate(c); err != nil {
		return View{}, err
	}
	created, err := s.repo.Create(c)
	if err != nil {
		return View{}, err
	}
	s.notify("create", created.ID)
	return s.toView(created)
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
	s.notify("update", updated.ID)
	return s.toView(updated)
}

// Delete removes a client (cascading bindings + credentials) and applies.
func (s *Service) Delete(id string) error {
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	s.notify("delete", id)
	return nil
}

// AddBinding binds a client to an inbound.
func (s *Service) AddBinding(clientID, inboundID string) (Binding, error) {
	if inboundID == "" {
		return Binding{}, fmt.Errorf("%w: inboundId is required", ErrValidation)
	}
	b, err := s.repo.CreateBinding(Binding{ClientID: clientID, InboundID: inboundID, Enabled: true})
	if err != nil {
		return Binding{}, err
	}
	s.notify("bind", clientID)
	return b, nil
}

// RemoveBinding deletes a binding. The client survives as an orphan.
func (s *Service) RemoveBinding(bindingID, clientID string) error {
	if err := s.repo.DeleteBinding(bindingID); err != nil {
		return err
	}
	s.notify("unbind", clientID)
	return nil
}

// SetCredential encrypts and stores a credential for a binding.
func (s *Service) SetCredential(bindingID, kind, plaintext string) (Credential, error) {
	if plaintext == "" {
		return Credential{}, fmt.Errorf("%w: credential value is required", ErrValidation)
	}
	c, err := s.creds.Set(bindingID, kind, plaintext)
	if err != nil {
		return Credential{}, err
	}
	s.notify("credential", bindingID)
	return c, nil
}

// RotateCredential rotates a binding's credential to a new version.
func (s *Service) RotateCredential(bindingID, kind, plaintext string) (Credential, error) {
	if plaintext == "" {
		return Credential{}, fmt.Errorf("%w: credential value is required", ErrValidation)
	}
	c, err := s.creds.Rotate(bindingID, kind, plaintext)
	if err != nil {
		return Credential{}, err
	}
	s.notify("credential-rotate", bindingID)
	return c, nil
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
	bindings, err := s.repo.BindingsForClient(c.ID)
	if err != nil {
		return View{}, err
	}
	inbounds := make([]string, 0, len(bindings))
	hasCreds := false
	for _, b := range bindings {
		inbounds = append(inbounds, b.InboundID)
		if !hasCreds {
			creds, err := s.creds.ListForBinding(b.ID)
			if err == nil && len(creds) > 0 {
				hasCreds = true
			}
		}
	}
	status := ComputeStatus(c, timeFromUnix(s.now()), false, false, len(bindings) == 0)
	return View{Client: c, Status: status, InboundIDs: inbounds, HasCreds: hasCreds}, nil
}

func (s *Service) notify(kind, id string) {
	if s.notifier != nil {
		s.notifier.NotifyMutation(kind, id)
	}
}
