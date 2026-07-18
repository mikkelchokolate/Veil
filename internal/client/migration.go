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
// to re-run during rolling migration.
func (m *Migrator) MigrateInboundProfiles(inboundID, protocol string, profiles []LegacyProfile) (MigrationResult, error) {
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
		if _, err := m.repo.Get(clientID); err == nil {
			// Already migrated.
			res.Skipped++
			continue
		}
		name := p.Name
		if name == "" {
			name = p.Username
		}
		cl, err := m.repo.Create(Client{
			ID:               clientID,
			Name:             name,
			Enabled:          p.Enabled,
			QuotaResetPolicy: ResetNever,
		})
		if err != nil {
			return res, fmt.Errorf("client: migrate create %q: %w", p.Username, err)
		}
		b, err := m.repo.CreateBinding(Binding{ClientID: cl.ID, InboundID: inboundID, Enabled: p.Enabled})
		if err != nil {
			return res, fmt.Errorf("client: migrate binding %q: %w", p.Username, err)
		}
		if _, err := m.creds.Set(b.ID, "password", p.Password); err != nil {
			return res, fmt.Errorf("client: migrate credential %q: %w", p.Username, err)
		}
		res.ClientsCreated++
		res.BindingsCreated++
		res.CredentialsCreated++
		res.ClientIDs = append(res.ClientIDs, cl.ID)
	}
	return res, nil
}

// stableClientID derives a deterministic client ID from (inbound, username)
// so migration is idempotent. It is a UUID-shaped hex string to stay
// compatible with the clients.id TEXT column.
func stableClientID(inboundID, username string) string {
	sum := sha256.Sum256([]byte(inboundID + "|" + username))
	h := hex.EncodeToString(sum[:16])
	return fmt.Sprintf("%s-%s-%s-%s-%s", h[0:8], h[8:12], h[12:16], h[16:20], h[20:32])
}
