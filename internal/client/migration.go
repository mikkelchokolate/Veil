package client

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// LegacyProfile is the inbound-embedded client profile shape being migrated
// away from. It matches model.ClientProfile without pulling a dependency.
type LegacyProfile struct {
	Name     string
	Username string
	Password string
	Enabled  bool
}

// MigrationResult summarizes a legacy-profile migration run.
type MigrationResult struct {
	ClientsCreated     int      `json:"clientsCreated"`
	BindingsCreated    int      `json:"bindingsCreated"`
	CredentialsCreated int      `json:"credentialsCreated"`
	Skipped            int      `json:"skipped"`
	ClientIDs          []string `json:"clientIds"`
}

// Migrator converts legacy inbound-embedded client profiles into the
// normalized Client + Binding + Credential model. It is idempotent: the client
// ID is derived deterministically from (inboundID, username), so re-running a
// migration never duplicates clients.
type Migrator struct {
	repo  *Repository
	creds *CredentialStore
	opts  migratorOptions
}

type migratorOptions struct{ includeDisabled bool }

// MigratorOption customizes migration behavior.
type MigratorOption func(*migratorOptions)

// WithIncludeDisabled migrates disabled legacy profiles as disabled clients
// (visible but inactive) instead of skipping them.
func WithIncludeDisabled() MigratorOption {
	return func(o *migratorOptions) { o.includeDisabled = true }
}

func NewMigrator(repo *Repository, creds *CredentialStore, opts ...MigratorOption) *Migrator {
	m := &Migrator{repo: repo, creds: creds}
	for _, opt := range opts {
		opt(&m.opts)
	}
	return m
}

// MigrateInboundProfiles converts one inbound's legacy profiles. Clients that
// already exist (by stable derived ID) are skipped, making the operation safe
// to re-run during rolling migration. The whole inbound migrates in ONE
// transaction: a failure rolls back every client/binding/credential created
// for it instead of leaving a half-migrated inbound.
func (m *Migrator) MigrateInboundProfiles(inboundID, protocol string, profiles []LegacyProfile) (MigrationResult, error) {
	var res MigrationResult
	err := m.repo.WithTx(func(tx *Tx) error {
		r, err := m.MigrateInboundProfilesTx(tx, inboundID, protocol, profiles)
		res = r
		return err
	})
	return res, err
}

// MigrateInboundProfilesTx is the transactional core of MigrateInboundProfiles
// for callers (startup migration, API endpoint) that must fold the migration
// into a larger atomic commit (e.g. with the desired-revision bump and the
// immutable snapshot).
func (m *Migrator) MigrateInboundProfilesTx(tx *Tx, inboundID, protocol string, profiles []LegacyProfile) (MigrationResult, error) {
	var res MigrationResult
	for _, p := range profiles {
		if !p.Enabled && !m.opts.includeDisabled {
			res.Skipped++
			continue
		}
		if p.Password == "" || p.Username == "" {
			res.Skipped++
			continue
		}
		clientID := stableClientID(inboundID, p.Username)
		if _, err := tx.Get(clientID); err == nil {
			// Repair an interrupted migration: the client row may have committed
			// before its binding/credential and provenance marker. A completed
			// migration is filtered by the startup provenance gate before here.
			bindings, err := tx.BindingsForClient(clientID)
			if err != nil {
				return res, err
			}
			var binding *Binding
			for i := range bindings {
				if bindings[i].InboundID == inboundID {
					binding = &bindings[i]
					break
				}
			}
			if binding == nil {
				created, err := tx.CreateBinding(Binding{ClientID: clientID, InboundID: inboundID, Enabled: p.Enabled})
				if err != nil {
					return res, fmt.Errorf("client: repair binding %q: %w", p.Username, err)
				}
				binding = &created
				res.BindingsCreated++
			}
			active, activeErr := activeCredentialQ(tx.q, binding.ID, "password")
			if activeErr != nil || active.ID == "" {
				if _, err := tx.SetCredential(m.creds, binding.ID, "password", p.Password); err != nil {
					return res, fmt.Errorf("client: repair credential %q: %w", p.Username, err)
				}
				res.CredentialsCreated++
			}
			res.Skipped++
			continue
		}
		name := p.Name
		if name == "" {
			name = p.Username
		}
		cl, err := tx.CreateClient(Client{
			ID:               clientID,
			Name:             name,
			Enabled:          p.Enabled,
			QuotaResetPolicy: ResetNever,
		})
		if err != nil {
			return res, fmt.Errorf("client: migrate create %q: %w", p.Username, err)
		}
		b, err := tx.CreateBinding(Binding{ClientID: cl.ID, InboundID: inboundID, Enabled: p.Enabled})
		if err != nil {
			return res, fmt.Errorf("client: migrate binding %q: %w", p.Username, err)
		}
		if _, err := tx.SetCredential(m.creds, b.ID, "password", p.Password); err != nil {
			return res, fmt.Errorf("client: migrate credential %q: %w", p.Username, err)
		}
		res.ClientsCreated++
		res.BindingsCreated++
		res.CredentialsCreated++
		res.ClientIDs = append(res.ClientIDs, cl.ID)
	}
	return res, nil
}

// MissingInboundProfiles returns the subset of migratable legacy profiles
// whose normalized representation (a client with the stable derived ID AND a
// binding to this inbound) does not exist yet. Startup uses it to fingerprint
// the CURRENT legacy profile set on every boot: a restored older state file
// may carry profiles that were not represented when the migration marker was
// written, and the marker alone must never suppress their migration.
//
// Credential state is deliberately NOT part of "represented": operators
// rotate/revoke credentials of migrated clients during normal operation, and
// treating that as a migration gap would re-migrate forever.
func (m *Migrator) MissingInboundProfiles(tx *Tx, inboundID string, profiles []LegacyProfile) ([]LegacyProfile, error) {
	var missing []LegacyProfile
	for _, p := range profiles {
		if !p.Enabled && !m.opts.includeDisabled {
			continue
		}
		if p.Password == "" || p.Username == "" {
			continue
		}
		clientID := stableClientID(inboundID, p.Username)
		if _, err := tx.Get(clientID); err != nil {
			missing = append(missing, p)
			continue
		}
		bindings, err := tx.BindingsForClient(clientID)
		if err != nil {
			return nil, fmt.Errorf("read bindings for profile %q: %w", p.Username, err)
		}
		represented := false
		for _, b := range bindings {
			if b.InboundID == inboundID {
				represented = true
				break
			}
		}
		if !represented {
			missing = append(missing, p)
		}
	}
	return missing, nil
}

// VerifyInboundProfiles checks that every migratable legacy profile of the
// inbound has a corresponding normalized client (stable derived ID) with a
// binding to this inbound and an active credential. It returns an error
// describing the first gap found. Used after startup migration to prove the
// conversion actually persisted before the marker is recorded.
func (m *Migrator) VerifyInboundProfiles(tx *Tx, inboundID string, profiles []LegacyProfile) error {
	for _, p := range profiles {
		if !p.Enabled && !m.opts.includeDisabled {
			continue
		}
		if p.Password == "" || p.Username == "" {
			continue
		}
		clientID := stableClientID(inboundID, p.Username)
		if _, err := tx.Get(clientID); err != nil {
			return fmt.Errorf("client %q (profile %q) missing after migration", clientID, p.Username)
		}
		bindings, err := tx.BindingsForClient(clientID)
		if err != nil {
			return fmt.Errorf("read bindings for migrated profile %q: %w", p.Username, err)
		}
		found := false
		for _, b := range bindings {
			if b.InboundID != inboundID {
				continue
			}
			active, aerr := activeCredentialQ(tx.q, b.ID, "password")
			if aerr == nil && active.ID != "" {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("migrated profile %q has no binding with active credential on inbound %q", p.Username, inboundID)
		}
	}
	return nil
}

// stableClientID derives a deterministic client ID from (inbound, username)
// so migration is idempotent. It is a UUID-shaped hex string to stay
// compatible with the clients.id TEXT column.
func stableClientID(inboundID, username string) string {
	sum := sha256.Sum256([]byte(inboundID + "|" + username))
	h := hex.EncodeToString(sum[:16])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}

// StableClientID exposes the deterministic migration client ID so callers
// (e.g. startup-migration fingerprinting) can reason about which legacy
// profiles are already represented without duplicating the derivation.
func StableClientID(inboundID, username string) string {
	return stableClientID(inboundID, username)
}
