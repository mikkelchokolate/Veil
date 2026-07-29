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
	{
		version: 3,
		name:    "migration_markers",
		sql: `
CREATE TABLE migration_markers (
  key TEXT PRIMARY KEY,
  version INTEGER NOT NULL,
  applied_at INTEGER NOT NULL,
  details TEXT NOT NULL DEFAULT '{}'
);
`,
	},
	{
		version: 4,
		name:    "revision_snapshot_state_digest",
		sql: `
ALTER TABLE revision_snapshots
  ADD COLUMN state_sha256 TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 5,
		name:    "immutable_apply_rollbacks",
		sql: `
CREATE TABLE apply_rollbacks (
  id TEXT PRIMARY KEY,
  selected_revision INTEGER NOT NULL,
  new_revision INTEGER NOT NULL UNIQUE,
  actor_id TEXT NOT NULL,
  selected_snapshot_sha256 TEXT NOT NULL,
  created_at INTEGER NOT NULL
);
`,
	},
	{
		version: 6,
		name:    "durable_apply_lease",
		sql: `
CREATE TABLE apply_lease (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  owner_process TEXT NOT NULL DEFAULT '',
  lease_expires_at INTEGER NOT NULL DEFAULT 0,
  heartbeat_at INTEGER NOT NULL DEFAULT 0,
  current_operation TEXT NOT NULL DEFAULT ''
);
INSERT INTO apply_lease(id) VALUES(1);
`,
	},
	{
		version: 7,
		name:    "binding_runtime_identity",
		sql: `
ALTER TABLE client_bindings ADD COLUMN runtime_identity TEXT NOT NULL DEFAULT '';
UPDATE client_bindings
SET runtime_identity = 'v_' || lower(replace(id, '-', ''))
WHERE runtime_identity = '';
CREATE UNIQUE INDEX idx_client_bindings_inbound_runtime_identity
ON client_bindings(inbound_id, runtime_identity);
`,
	},
	{
		version: 8,
		name:    "domain_integrity_guards",
		sql: `
CREATE TRIGGER validate_clients_insert BEFORE INSERT ON clients
WHEN NEW.enabled NOT IN (0,1) OR NEW.depleted NOT IN (0,1) OR NEW.quota_bytes < 0 OR NEW.device_limit < 0
BEGIN SELECT RAISE(ABORT, 'invalid client domain values'); END;
CREATE TRIGGER validate_clients_update BEFORE UPDATE ON clients
WHEN NEW.enabled NOT IN (0,1) OR NEW.depleted NOT IN (0,1) OR NEW.quota_bytes < 0 OR NEW.device_limit < 0
BEGIN SELECT RAISE(ABORT, 'invalid client domain values'); END;
CREATE TRIGGER validate_traffic_counter BEFORE INSERT ON traffic_counters
WHEN NEW.upload_bytes < 0 OR NEW.download_bytes < 0
BEGIN SELECT RAISE(ABORT, 'invalid traffic counter'); END;
CREATE TRIGGER validate_traffic_counter_update BEFORE UPDATE ON traffic_counters
WHEN NEW.upload_bytes < 0 OR NEW.download_bytes < 0
BEGIN SELECT RAISE(ABORT, 'invalid traffic counter'); END;
CREATE TRIGGER validate_revision_insert BEFORE INSERT ON revisions
WHEN NEW.desired_revision < NEW.applied_revision
BEGIN SELECT RAISE(ABORT, 'desired revision below applied revision'); END;
CREATE TRIGGER validate_revision_update BEFORE UPDATE ON revisions
WHEN NEW.desired_revision < NEW.applied_revision
BEGIN SELECT RAISE(ABORT, 'desired revision below applied revision'); END;
CREATE TRIGGER validate_credential_version BEFORE INSERT ON client_credentials
WHEN NEW.key_version < 1 OR NEW.credential_version < 1
BEGIN SELECT RAISE(ABORT, 'invalid credential version'); END;
`,
	},
	{
		version: 9,
		name:    "domain_integrity_completion",
		sql: `
CREATE TRIGGER validate_apply_job_insert BEFORE INSERT ON apply_jobs
WHEN NEW.status NOT IN ('pending','planning','validating','applying','health_check','succeeded','failed','rolling_back','rolled_back','rollback_failed')
  OR NEW.desired_revision < 0 OR NEW.base_revision < 0
BEGIN SELECT RAISE(ABORT, 'invalid apply job domain values'); END;
CREATE TRIGGER validate_apply_job_update BEFORE UPDATE ON apply_jobs
WHEN NEW.status NOT IN ('pending','planning','validating','applying','health_check','succeeded','failed','rolling_back','rolled_back','rollback_failed')
  OR NEW.desired_revision < 0 OR NEW.base_revision < 0
BEGIN SELECT RAISE(ABORT, 'invalid apply job domain values'); END;
CREATE TRIGGER validate_client_domain_insert_v2 BEFORE INSERT ON clients
WHEN NEW.quota_reset_policy NOT IN ('never','daily','weekly','monthly') OR NEW.version < 1
BEGIN SELECT RAISE(ABORT, 'invalid client reset policy or version'); END;
CREATE TRIGGER validate_client_domain_update_v2 BEFORE UPDATE ON clients
WHEN NEW.quota_reset_policy NOT IN ('never','daily','weekly','monthly') OR NEW.version < 1
BEGIN SELECT RAISE(ABORT, 'invalid client reset policy or version'); END;
CREATE TRIGGER validate_binding_insert BEFORE INSERT ON client_bindings
WHEN NEW.enabled NOT IN (0,1) OR NEW.version < 1
BEGIN SELECT RAISE(ABORT, 'invalid binding domain values'); END;
CREATE TRIGGER validate_binding_update BEFORE UPDATE ON client_bindings
WHEN NEW.enabled NOT IN (0,1) OR NEW.version < 1
BEGIN SELECT RAISE(ABORT, 'invalid binding domain values'); END;
CREATE TRIGGER validate_token_insert BEFORE INSERT ON subscription_tokens
WHEN NEW.enabled NOT IN (0,1)
BEGIN SELECT RAISE(ABORT, 'invalid token domain values'); END;
CREATE TRIGGER validate_token_update BEFORE UPDATE ON subscription_tokens
WHEN NEW.enabled NOT IN (0,1)
BEGIN SELECT RAISE(ABORT, 'invalid token domain values'); END;
CREATE TRIGGER validate_traffic_sample_insert BEFORE INSERT ON traffic_samples
WHEN NEW.bucket_start < 0 OR NEW.upload_delta < 0 OR NEW.download_delta < 0
  OR NOT EXISTS (SELECT 1 FROM clients WHERE id=NEW.client_id)
  OR (NEW.binding_id <> '' AND NOT EXISTS (SELECT 1 FROM client_bindings WHERE id=NEW.binding_id AND client_id=NEW.client_id))
BEGIN SELECT RAISE(ABORT, 'invalid traffic sample ownership or counters'); END;
CREATE TRIGGER validate_traffic_sample_update BEFORE UPDATE ON traffic_samples
WHEN NEW.bucket_start < 0 OR NEW.upload_delta < 0 OR NEW.download_delta < 0
  OR NOT EXISTS (SELECT 1 FROM clients WHERE id=NEW.client_id)
  OR (NEW.binding_id <> '' AND NOT EXISTS (SELECT 1 FROM client_bindings WHERE id=NEW.binding_id AND client_id=NEW.client_id))
BEGIN SELECT RAISE(ABORT, 'invalid traffic sample ownership or counters'); END;
CREATE TRIGGER validate_traffic_counter_ownership_insert BEFORE INSERT ON traffic_counters
WHEN NOT EXISTS (SELECT 1 FROM clients WHERE id=NEW.client_id)
  OR (NEW.binding_id <> '' AND NOT EXISTS (SELECT 1 FROM client_bindings WHERE id=NEW.binding_id AND client_id=NEW.client_id))
BEGIN SELECT RAISE(ABORT, 'invalid traffic counter ownership'); END;
CREATE TRIGGER validate_traffic_counter_ownership_update BEFORE UPDATE ON traffic_counters
WHEN NOT EXISTS (SELECT 1 FROM clients WHERE id=NEW.client_id)
  OR (NEW.binding_id <> '' AND NOT EXISTS (SELECT 1 FROM client_bindings WHERE id=NEW.binding_id AND client_id=NEW.client_id))
BEGIN SELECT RAISE(ABORT, 'invalid traffic counter ownership'); END;
`,
	},
	{
		version: 10,
		name:    "apply_fencing_and_publication_receipts",
		sql: `
ALTER TABLE apply_lease ADD COLUMN generation INTEGER NOT NULL DEFAULT 0;
ALTER TABLE apply_jobs ADD COLUMN lease_generation INTEGER NOT NULL DEFAULT 0;
ALTER TABLE apply_jobs ADD COLUMN owner_process TEXT NOT NULL DEFAULT '';
CREATE TABLE runtime_publications (
  job_id TEXT PRIMARY KEY REFERENCES apply_jobs(id) ON DELETE CASCADE,
  revision INTEGER NOT NULL,
  generation INTEGER NOT NULL CHECK (generation > 0),
  snapshot_sha256 TEXT NOT NULL,
  operations_json TEXT NOT NULL DEFAULT '[]',
  published_at INTEGER NOT NULL
);
CREATE INDEX idx_runtime_publications_revision ON runtime_publications(revision);
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

	rows, err := db.Query(`SELECT version, name FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("storage: read migration history: %w", err)
	}
	current := 0
	for rows.Next() {
		var version int
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			rows.Close()
			return err
		}
		if version != current+1 || version > len(migrations) || migrations[version-1].name != name {
			rows.Close()
			return fmt.Errorf("storage: invalid ordered migration history at version %d", version)
		}
		current = version
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("storage: iterate migration history: %w", err)
	}
	if err := rows.Close(); err != nil {
		return err
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
	var fkTable string
	if err := db.QueryRow(`SELECT "table" FROM pragma_foreign_key_check LIMIT 1`).Scan(&fkTable); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("storage: foreign_key_check: %w", err)
	} else if err == nil {
		return fmt.Errorf("storage: foreign_key_check failed for %s", fkTable)
	}
	if err := validateDomainIntegrity(db); err != nil {
		return err
	}
	return nil
}

func validateDomainIntegrity(db *sql.DB) error {
	checks := []struct {
		name  string
		query string
	}{
		{"apply jobs", `SELECT EXISTS(SELECT 1 FROM apply_jobs WHERE status NOT IN ('pending','planning','validating','applying','health_check','succeeded','failed','rolling_back','rolled_back','rollback_failed') OR desired_revision < 0 OR base_revision < 0)`},
		{"clients", `SELECT EXISTS(SELECT 1 FROM clients WHERE enabled NOT IN (0,1) OR depleted NOT IN (0,1) OR quota_bytes < 0 OR device_limit < 0 OR quota_reset_policy NOT IN ('never','daily','weekly','monthly') OR version < 1)`},
		{"bindings", `SELECT EXISTS(SELECT 1 FROM client_bindings WHERE enabled NOT IN (0,1) OR version < 1)`},
		{"tokens", `SELECT EXISTS(SELECT 1 FROM subscription_tokens WHERE enabled NOT IN (0,1))`},
		{"traffic counters", `SELECT EXISTS(SELECT 1 FROM traffic_counters t WHERE upload_bytes < 0 OR download_bytes < 0 OR NOT EXISTS (SELECT 1 FROM clients c WHERE c.id=t.client_id) OR (t.binding_id <> '' AND NOT EXISTS (SELECT 1 FROM client_bindings b WHERE b.id=t.binding_id AND b.client_id=t.client_id)))`},
		{"traffic samples", `SELECT EXISTS(SELECT 1 FROM traffic_samples t WHERE bucket_start < 0 OR upload_delta < 0 OR download_delta < 0 OR NOT EXISTS (SELECT 1 FROM clients c WHERE c.id=t.client_id) OR (t.binding_id <> '' AND NOT EXISTS (SELECT 1 FROM client_bindings b WHERE b.id=t.binding_id AND b.client_id=t.client_id)))`},
	}
	for _, check := range checks {
		var invalid int
		if err := db.QueryRow(check.query).Scan(&invalid); err != nil {
			return fmt.Errorf("storage: validate %s: %w", check.name, err)
		}
		if invalid != 0 {
			return fmt.Errorf("storage: invalid %s domain rows", check.name)
		}
	}
	return nil
}
