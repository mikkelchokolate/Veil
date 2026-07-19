package storage

import (
	"database/sql"
	"fmt"
)

// migration is a single versioned schema change applied inside a transaction.
type migration struct {
	version int
	name    string
	sql     string
}

// migrations is the ordered list of schema changes. Each runs exactly once, in
// order, and is recorded in schema_migrations. Never edit an applied
// migration; append a new one with the next version number.
var migrations = []migration{
	{
		version: 1,
		name:    "core_domain",
		sql: `
CREATE TABLE revisions (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  desired_revision INTEGER NOT NULL DEFAULT 0,
  applied_revision INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE apply_jobs (
  id TEXT PRIMARY KEY,
  desired_revision INTEGER NOT NULL,
  base_revision INTEGER NOT NULL,
  status TEXT NOT NULL,
  trigger TEXT NOT NULL,
  actor_id TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  started_at INTEGER,
  finished_at INTEGER,
  error_code TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  operations TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX idx_apply_jobs_status ON apply_jobs(status);
CREATE INDEX idx_apply_jobs_created ON apply_jobs(created_at);

CREATE TABLE clients (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  email TEXT,
  enabled INTEGER NOT NULL DEFAULT 1,
  group_id TEXT,
  quota_bytes INTEGER,
  quota_reset_policy TEXT NOT NULL DEFAULT 'never',
  quota_reset_at INTEGER,
  expires_at INTEGER,
  device_limit INTEGER,
  notes TEXT NOT NULL DEFAULT '',
  depleted INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  version INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX idx_clients_enabled ON clients(enabled);
CREATE INDEX idx_clients_expires ON clients(expires_at);

CREATE TABLE client_bindings (
  id TEXT PRIMARY KEY,
  client_id TEXT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  inbound_id TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  protocol_settings TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  version INTEGER NOT NULL DEFAULT 1,
  UNIQUE(client_id, inbound_id)
);
CREATE INDEX idx_bindings_client ON client_bindings(client_id);
CREATE INDEX idx_bindings_inbound ON client_bindings(inbound_id);

CREATE TABLE client_credentials (
  id TEXT PRIMARY KEY,
  binding_id TEXT NOT NULL REFERENCES client_bindings(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  encrypted_value BLOB NOT NULL,
  key_version INTEGER NOT NULL DEFAULT 1,
  credential_version INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL,
  rotated_at INTEGER,
  revoked_at INTEGER,
  UNIQUE(binding_id, kind, credential_version)
);
CREATE INDEX idx_credentials_binding ON client_credentials(binding_id);

CREATE TABLE subscription_tokens (
  id TEXT PRIMARY KEY,
  client_id TEXT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  token_hash BLOB NOT NULL UNIQUE,
  token_prefix TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  expires_at INTEGER,
  created_at INTEGER NOT NULL,
  last_used_at INTEGER,
  revoked_at INTEGER,
  created_by TEXT
);
CREATE INDEX idx_tokens_client ON subscription_tokens(client_id);

CREATE TABLE traffic_counters (
  client_id TEXT NOT NULL,
  binding_id TEXT NOT NULL DEFAULT '',
  upload_bytes INTEGER NOT NULL DEFAULT 0,
  download_bytes INTEGER NOT NULL DEFAULT 0,
  last_online_at INTEGER,
  last_observed_at INTEGER,
  telemetry_state TEXT NOT NULL DEFAULT 'unknown',
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (client_id, binding_id)
);
CREATE INDEX idx_counters_binding ON traffic_counters(binding_id);

CREATE TABLE traffic_runtime_state (
  provider_key TEXT PRIMARY KEY,
  runtime_instance TEXT NOT NULL DEFAULT '',
  last_upload_raw INTEGER NOT NULL DEFAULT 0,
  last_download_raw INTEGER NOT NULL DEFAULT 0,
  last_observed_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE traffic_samples (
  bucket_start INTEGER NOT NULL,
  client_id TEXT NOT NULL,
  binding_id TEXT NOT NULL DEFAULT '',
  upload_delta INTEGER NOT NULL DEFAULT 0,
  download_delta INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (bucket_start, client_id, binding_id)
);
CREATE INDEX idx_samples_client ON traffic_samples(client_id, bucket_start);
`,
	},
	{
		version: 2,
		name:    "revision_snapshots",
		sql: `
CREATE TABLE revision_snapshots (
  revision INTEGER PRIMARY KEY,
  payload TEXT NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
`,
	},
}

// Migrate applies all pending migrations in order. Each migration runs in its
// own transaction so a failure cannot leave a half-applied schema. Safe to run
// repeatedly; already-applied versions are skipped.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
	  version INTEGER PRIMARY KEY,
	  name TEXT NOT NULL,
	  applied_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
	)`); err != nil {
		return fmt.Errorf("storage: create schema_migrations: %w", err)
	}

	var current int
	if err := db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&current); err != nil {
		return fmt.Errorf("storage: read schema version: %w", err)
	}

	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("storage: begin migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("storage: migration %d (%s): %w", m.version, m.name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, name) VALUES(?, ?)`, m.version, m.name); err != nil {
			tx.Rollback()
			return fmt.Errorf("storage: record migration %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("storage: commit migration %d: %w", m.version, err)
		}
	}
	return nil
}
