package storage

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
	{
		version: 11,
		name:    "durable_quota_enforcement",
		sql: `
CREATE TABLE IF NOT EXISTS quota_enforcement (
  client_id TEXT PRIMARY KEY REFERENCES clients(id) ON DELETE CASCADE,
  state TEXT NOT NULL CHECK(state IN ('pending','failed','enforced')),
  desired_revision INTEGER NOT NULL DEFAULT 0,
  next_retry_at INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  attempts INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_quota_enforcement_retry
  ON quota_enforcement(state, next_retry_at, client_id);
`,
	},
	{
		version: 12,
		name:    "durable_idempotency",
		sql: `
CREATE TABLE IF NOT EXISTS idempotency_records (
  scope TEXT PRIMARY KEY,
  actor_id TEXT NOT NULL,
  endpoint TEXT NOT NULL,
  idempotency_key TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('reserved','completed')),
  owner_process TEXT NOT NULL,
  reserved_until INTEGER NOT NULL,
  response_status INTEGER,
  response_headers BLOB,
  response_body BLOB,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_idempotency_actor_endpoint_key
  ON idempotency_records(actor_id, endpoint, idempotency_key);
CREATE INDEX idx_idempotency_expiry ON idempotency_records(expires_at);
`,
	},
	{
		version: 13,
		name:    "credential_and_expiration_invariants",
		sql: `
-- credential_version is unique per binding/kind, so the highest active
-- version is the only provable winner. Revoke every older active duplicate
-- before installing the fail-closed partial uniqueness constraint.
UPDATE client_credentials AS old
SET revoked_at=COALESCE(old.rotated_at, old.created_at)
WHERE old.revoked_at IS NULL
  AND EXISTS (
    SELECT 1 FROM client_credentials AS newer
    WHERE newer.binding_id=old.binding_id
      AND newer.kind=old.kind
      AND newer.revoked_at IS NULL
      AND newer.credential_version>old.credential_version
  );
CREATE UNIQUE INDEX idx_client_credentials_one_active_kind
  ON client_credentials(binding_id, kind)
  WHERE revoked_at IS NULL;
CREATE TRIGGER validate_credential_kind_insert BEFORE INSERT ON client_credentials
WHEN NEW.kind <> 'password'
BEGIN SELECT RAISE(ABORT, 'unsupported credential kind'); END;
CREATE TRIGGER validate_credential_kind_update BEFORE UPDATE OF kind ON client_credentials
WHEN NEW.kind <> 'password'
BEGIN SELECT RAISE(ABORT, 'unsupported credential kind'); END;

CREATE TABLE expiration_enforcement (
  client_id TEXT PRIMARY KEY REFERENCES clients(id) ON DELETE CASCADE,
  expires_at INTEGER NOT NULL,
  state TEXT NOT NULL CHECK(state IN ('pending','applying','enforced','failed')),
  desired_revision INTEGER NOT NULL DEFAULT 0,
  applied_revision INTEGER NOT NULL DEFAULT 0,
  effective_at INTEGER NOT NULL,
  next_retry_at INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  attempts INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);
CREATE INDEX idx_expiration_enforcement_retry
  ON expiration_enforcement(state, next_retry_at, client_id);
CREATE INDEX idx_clients_expiration_keyset
  ON clients(expires_at, created_at, id)
  WHERE enabled=1 AND expires_at IS NOT NULL;
`,
	},
	{
		version: 14,
		name:    "apply_publication_intent",
		sql: `
ALTER TABLE runtime_publications ADD COLUMN owner_process TEXT NOT NULL DEFAULT '';
ALTER TABLE runtime_publications ADD COLUMN operation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE runtime_publications ADD COLUMN lease_expires_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runtime_publications ADD COLUMN phase TEXT NOT NULL DEFAULT 'published';
ALTER TABLE runtime_publications ADD COLUMN expected_live_manifest_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE runtime_publications ADD COLUMN previous_live_manifest_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE runtime_publications ADD COLUMN artifacts_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE runtime_publications ADD COLUMN service_phase TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE runtime_publications ADD COLUMN firewall_phase TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE runtime_publications ADD COLUMN updated_at INTEGER NOT NULL DEFAULT 0;
CREATE INDEX idx_runtime_publications_phase ON runtime_publications(phase, revision, job_id);
`,
	},
	{
		version: 15,
		name:    "apply_publication_live_root",
		sql: `
ALTER TABLE runtime_publications ADD COLUMN live_root TEXT NOT NULL DEFAULT '';
`,
	},
	{
		version: 16,
		name:    "enforcement_finalization",
		sql: `
ALTER TABLE runtime_publications ADD COLUMN confirmations_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE quota_enforcement RENAME TO quota_enforcement_old;
CREATE TABLE quota_enforcement (
  client_id TEXT PRIMARY KEY REFERENCES clients(id) ON DELETE CASCADE,
  state TEXT NOT NULL CHECK(state IN ('pending','applying','failed','enforced')),
  desired_revision INTEGER NOT NULL DEFAULT 0,
  applied_revision INTEGER NOT NULL DEFAULT 0,
  next_retry_at INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  attempts INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL
);
INSERT INTO quota_enforcement(client_id,state,desired_revision,next_retry_at,last_error,attempts,updated_at)
SELECT client_id,state,desired_revision,next_retry_at,last_error,attempts,updated_at FROM quota_enforcement_old;
DROP TABLE quota_enforcement_old;
CREATE INDEX idx_quota_enforcement_retry ON quota_enforcement(state,next_retry_at,client_id);
`,
	},
	{
		version: 17,
		name:    "idempotency_takeover_results",
		sql: `
ALTER TABLE idempotency_records ADD COLUMN operation_generation INTEGER NOT NULL DEFAULT 1;
ALTER TABLE idempotency_records ADD COLUMN auth_generation TEXT NOT NULL DEFAULT '';
ALTER TABLE idempotency_records ADD COLUMN result_record_id TEXT NOT NULL DEFAULT '';
ALTER TABLE idempotency_records ADD COLUMN response_encrypted INTEGER NOT NULL DEFAULT 0;
ALTER TABLE idempotency_records ADD COLUMN replay_expires_at INTEGER NOT NULL DEFAULT 0;
DROP INDEX idx_idempotency_actor_endpoint_key;
CREATE UNIQUE INDEX idx_idempotency_actor_auth_endpoint_key
  ON idempotency_records(actor_id,auth_generation,endpoint,idempotency_key);
CREATE TABLE idempotency_results (
  id TEXT PRIMARY KEY,
  scope TEXT NOT NULL,
  operation_generation INTEGER NOT NULL,
  response_status INTEGER NOT NULL,
  response_headers BLOB NOT NULL,
  response_body BLOB,
  encrypted INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);
CREATE INDEX idx_idempotency_results_expiry ON idempotency_results(expires_at);
`,
	},
	{
		version: 18,
		name:    "panel_update_jobs",
		sql: `
CREATE TABLE panel_update_jobs (
  id TEXT PRIMARY KEY,
  target_version TEXT NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('staging','restart_pending','restarting','succeeded','failed')),
  stage_apply_job_id TEXT NOT NULL DEFAULT '',
  restart_apply_job_id TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX idx_panel_update_jobs_status ON panel_update_jobs(status,updated_at,id);
`,
	},
	{
		version: 19,
		name:    "traffic_cleanup",
		sql: `
CREATE TRIGGER traffic_binding_cleanup AFTER DELETE ON client_bindings BEGIN
  DELETE FROM traffic_counters WHERE binding_id=OLD.id;
  DELETE FROM traffic_samples WHERE binding_id=OLD.id;
  DELETE FROM traffic_runtime_state WHERE provider_key LIKE '%:' || OLD.id;
END;
CREATE TRIGGER traffic_client_cleanup AFTER DELETE ON clients BEGIN
  DELETE FROM traffic_counters WHERE client_id=OLD.id;
  DELETE FROM traffic_samples WHERE client_id=OLD.id;
END;
`,
	},
	{
		version: 20,
		name:    "subscription_token_label",
		sql: `
ALTER TABLE subscription_tokens ADD COLUMN label TEXT NOT NULL DEFAULT '';
UPDATE subscription_tokens SET label=created_by,created_by='';
`,
	},
	{
		version: 21,
		name:    "staged_apply_jobs",
		sql: `
DROP TRIGGER validate_apply_job_insert;
DROP TRIGGER validate_apply_job_update;
CREATE TRIGGER validate_apply_job_insert BEFORE INSERT ON apply_jobs
WHEN NEW.status NOT IN ('pending','planning','validating','applying','health_check','staged','recovery_pending','succeeded','failed','rolling_back','rolled_back','rollback_failed')
  OR NEW.desired_revision < 0 OR NEW.base_revision < 0
BEGIN SELECT RAISE(ABORT, 'invalid apply job domain values'); END;
CREATE TRIGGER validate_apply_job_update BEFORE UPDATE ON apply_jobs
WHEN NEW.status NOT IN ('pending','planning','validating','applying','health_check','staged','recovery_pending','succeeded','failed','rolling_back','rolled_back','rollback_failed')
  OR NEW.desired_revision < 0 OR NEW.base_revision < 0
BEGIN SELECT RAISE(ABORT, 'invalid apply job domain values'); END;
`,
	},
	{
		version: 22,
		name:    "runtime_publication_state_machine",
		sql: `
ALTER TABLE runtime_publications ADD COLUMN base_revision INTEGER NOT NULL DEFAULT 0;
ALTER TABLE runtime_publications ADD COLUMN service_plan_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE runtime_publications ADD COLUMN previous_service_states_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE runtime_publications ADD COLUMN expected_service_generation TEXT NOT NULL DEFAULT '';
ALTER TABLE runtime_publications ADD COLUMN expected_config_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE runtime_publications ADD COLUMN firewall_transaction_id TEXT NOT NULL DEFAULT '';
ALTER TABLE runtime_publications ADD COLUMN previous_firewall_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE runtime_publications ADD COLUMN intended_firewall_digest TEXT NOT NULL DEFAULT '';
ALTER TABLE runtime_publications ADD COLUMN health_evidence_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE runtime_publications ADD COLUMN disposition TEXT NOT NULL DEFAULT '';
UPDATE runtime_publications
SET base_revision=COALESCE((SELECT base_revision FROM apply_jobs WHERE apply_jobs.id=runtime_publications.job_id),0);
CREATE TABLE runtime_publication_phases (
  job_id TEXT NOT NULL,
  phase TEXT NOT NULL,
  generation INTEGER NOT NULL CHECK(generation > 0),
  evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json)),
  committed_at INTEGER NOT NULL,
  PRIMARY KEY(job_id,phase),
  FOREIGN KEY(job_id) REFERENCES runtime_publications(job_id) ON DELETE CASCADE
);
INSERT INTO runtime_publication_phases(job_id,phase,generation,evidence_json,committed_at)
SELECT job_id,phase,generation,'{}',updated_at FROM runtime_publications;
CREATE INDEX idx_runtime_publication_phase_active
ON runtime_publications(phase,revision,updated_at,job_id);
CREATE TABLE runtime_publication_history (
  job_id TEXT PRIMARY KEY,
  revision INTEGER NOT NULL,
  base_revision INTEGER NOT NULL,
  final_phase TEXT NOT NULL,
  receipt_json TEXT NOT NULL CHECK(json_valid(receipt_json)),
  phases_json TEXT NOT NULL CHECK(json_valid(phases_json)),
  finalized_at INTEGER NOT NULL,
  FOREIGN KEY(job_id) REFERENCES apply_jobs(id) ON DELETE CASCADE
);
`,
	},
	{
		version: 23,
		name:    "runtime_verification_state",
		sql: `
CREATE TABLE runtime_verification (
  id INTEGER PRIMARY KEY CHECK (id=1),
  historical_applied_revision INTEGER NOT NULL DEFAULT 0,
  verified_revision INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL CHECK (status IN ('verified','unknown','recovering')),
  updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
);
INSERT INTO runtime_verification(id,historical_applied_revision,verified_revision,status)
SELECT 1,COALESCE(applied_revision,0),COALESCE(applied_revision,0),'verified' FROM revisions WHERE id=1;
INSERT OR IGNORE INTO runtime_verification(id,historical_applied_revision,verified_revision,status)
VALUES(1,0,0,'verified');
`,
	},
	{
		version: 24,
		name:    "idempotency_lease_and_domain_operations",
		sql: `
ALTER TABLE idempotency_records ADD COLUMN heartbeat_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE idempotency_records ADD COLUMN domain_operation_id TEXT NOT NULL DEFAULT '';
ALTER TABLE idempotency_records ADD COLUMN outcome_class TEXT NOT NULL DEFAULT '';
ALTER TABLE idempotency_records ADD COLUMN sensitivity TEXT NOT NULL DEFAULT 'public';
ALTER TABLE idempotency_records ADD COLUMN resource_id TEXT NOT NULL DEFAULT '';
ALTER TABLE idempotency_records ADD COLUMN secret_generation INTEGER NOT NULL DEFAULT 0;
CREATE TABLE domain_operations (
  id TEXT PRIMARY KEY,
  scope TEXT NOT NULL,
  operation_generation INTEGER NOT NULL CHECK(operation_generation>0),
  state TEXT NOT NULL CHECK(state IN ('reserved','mutation_committed','committed','abandoned')),
  result_record_id TEXT,
  domain_result_json TEXT NOT NULL DEFAULT '{}',
  mutation_bound_at INTEGER,
  created_at INTEGER NOT NULL,
  committed_at INTEGER,
  UNIQUE(scope,operation_generation),
  FOREIGN KEY(result_record_id) REFERENCES idempotency_results(id) ON DELETE RESTRICT
);
CREATE INDEX idx_domain_operations_scope_state ON domain_operations(scope,state,operation_generation DESC);
CREATE TRIGGER validate_idempotency_result_reference_insert BEFORE INSERT ON idempotency_records
WHEN NEW.result_record_id IS NOT NULL AND NEW.result_record_id<>'' AND NOT EXISTS (SELECT 1 FROM idempotency_results WHERE id=NEW.result_record_id)
BEGIN SELECT RAISE(ABORT,'invalid idempotency result reference'); END;
CREATE TRIGGER validate_idempotency_result_reference_update BEFORE UPDATE OF result_record_id ON idempotency_records
WHEN NEW.result_record_id IS NOT NULL AND NEW.result_record_id<>'' AND NOT EXISTS (SELECT 1 FROM idempotency_results WHERE id=NEW.result_record_id)
BEGIN SELECT RAISE(ABORT,'invalid idempotency result reference'); END;
`,
	},
	{
		version: 25,
		name:    "enforcement_target_generations",
		sql: `
DROP INDEX IF EXISTS idx_quota_enforcement_retry;
ALTER TABLE quota_enforcement RENAME TO quota_enforcement_legacy;
CREATE TABLE quota_enforcement (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  client_id TEXT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  target_generation INTEGER NOT NULL CHECK(target_generation>0),
  target_payload_hash TEXT NOT NULL CHECK(length(target_payload_hash)=64),
  target_depleted INTEGER NOT NULL CHECK(target_depleted IN (0,1)),
  target_period_epoch INTEGER NOT NULL DEFAULT 0,
  target_expires_at INTEGER NOT NULL DEFAULT 0,
  desired_revision INTEGER NOT NULL DEFAULT 0,
  applied_revision INTEGER NOT NULL DEFAULT 0,
  superseded_revision INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL CHECK(state IN ('pending','applying','failed','enforced','superseded')),
  next_retry_at INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  attempts INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL,
  UNIQUE(client_id,target_generation)
);
INSERT INTO quota_enforcement(client_id,target_generation,target_payload_hash,target_depleted,desired_revision,applied_revision,state,next_retry_at,last_error,attempts,updated_at)
SELECT q.client_id,1,printf('%064x',q.rowid),COALESCE(c.depleted,0),q.desired_revision,q.applied_revision,q.state,q.next_retry_at,q.last_error,q.attempts,q.updated_at
FROM quota_enforcement_legacy q JOIN clients c ON c.id=q.client_id;
DROP TABLE quota_enforcement_legacy;
CREATE UNIQUE INDEX idx_quota_enforcement_active_target ON quota_enforcement(client_id) WHERE state<>'superseded';
CREATE INDEX idx_quota_enforcement_retry ON quota_enforcement(state,next_retry_at,client_id,target_generation);
DROP INDEX IF EXISTS idx_expiration_enforcement_retry;
ALTER TABLE expiration_enforcement RENAME TO expiration_enforcement_legacy;
CREATE TABLE expiration_enforcement (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  client_id TEXT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
  target_generation INTEGER NOT NULL CHECK(target_generation>0),
  target_payload_hash TEXT NOT NULL CHECK(length(target_payload_hash)=64),
  target_depleted INTEGER NOT NULL DEFAULT 1 CHECK(target_depleted IN (0,1)),
  target_period_epoch INTEGER NOT NULL DEFAULT 0,
  target_expires_at INTEGER NOT NULL,
  desired_revision INTEGER NOT NULL DEFAULT 0,
  applied_revision INTEGER NOT NULL DEFAULT 0,
  superseded_revision INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL CHECK(state IN ('pending','applying','enforced','failed','superseded')),
  effective_at INTEGER NOT NULL,
  next_retry_at INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  attempts INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL,
  UNIQUE(client_id,target_generation)
);
INSERT INTO expiration_enforcement(client_id,target_generation,target_payload_hash,target_expires_at,desired_revision,applied_revision,state,effective_at,next_retry_at,last_error,attempts,updated_at)
SELECT client_id,1,printf('%064x',rowid),expires_at,desired_revision,applied_revision,state,effective_at,next_retry_at,last_error,attempts,updated_at FROM expiration_enforcement_legacy;
DROP TABLE expiration_enforcement_legacy;
CREATE UNIQUE INDEX idx_expiration_enforcement_active_target ON expiration_enforcement(client_id) WHERE state<>'superseded';
CREATE INDEX idx_expiration_enforcement_retry ON expiration_enforcement(state,next_retry_at,target_expires_at,client_id,target_generation);
CREATE INDEX idx_clients_next_expiry ON clients(enabled,expires_at,created_at,id);
`,
	},
	{
		version: 26,
		name:    "runtime_and_enforcement_query_guards",
		sql: `
CREATE INDEX idx_apply_jobs_status_created ON apply_jobs(status,created_at DESC,id);
CREATE INDEX idx_clients_next_expiry_active ON clients(enabled,expires_at,created_at,id) WHERE expires_at IS NOT NULL;
CREATE TRIGGER runtime_publications_validate_insert
BEFORE INSERT ON runtime_publications
WHEN NEW.phase NOT IN ('intent','publishing','artifacts_prepared','artifacts_committed','services_planned','services_converged','health_verified','firewall_committed','side_effect_planned','side_effect_committed','side_effect_verified','published','finalization_pending','rolled_back','recovery_transferred')
 OR json_valid(NEW.artifacts_json)=0 OR json_valid(NEW.operations_json)=0 OR json_valid(NEW.confirmations_json)=0
BEGIN SELECT RAISE(ABORT,'invalid runtime publication'); END;
CREATE TRIGGER runtime_publications_validate_update
BEFORE UPDATE ON runtime_publications
WHEN NEW.phase NOT IN ('intent','publishing','artifacts_prepared','artifacts_committed','services_planned','services_converged','health_verified','firewall_committed','side_effect_planned','side_effect_committed','side_effect_verified','published','finalization_pending','rolled_back','recovery_transferred')
 OR json_valid(NEW.artifacts_json)=0 OR json_valid(NEW.operations_json)=0 OR json_valid(NEW.confirmations_json)=0
BEGIN SELECT RAISE(ABORT,'invalid runtime publication'); END;
CREATE TRIGGER quota_enforcement_target_insert
BEFORE INSERT ON quota_enforcement
WHEN NEW.target_generation<=0 OR length(NEW.target_payload_hash)<>64 OR NEW.state NOT IN ('pending','applying','enforced','failed','superseded')
BEGIN SELECT RAISE(ABORT,'invalid quota enforcement target'); END;
CREATE TRIGGER quota_enforcement_target_update
BEFORE UPDATE ON quota_enforcement
WHEN NEW.target_generation<=0 OR length(NEW.target_payload_hash)<>64 OR NEW.state NOT IN ('pending','applying','enforced','failed','superseded')
BEGIN SELECT RAISE(ABORT,'invalid quota enforcement target'); END;
CREATE TRIGGER expiration_enforcement_target_insert
BEFORE INSERT ON expiration_enforcement
WHEN NEW.target_generation<=0 OR length(NEW.target_payload_hash)<>64 OR NEW.state NOT IN ('pending','applying','enforced','failed','superseded')
BEGIN SELECT RAISE(ABORT,'invalid expiration enforcement target'); END;
CREATE TRIGGER expiration_enforcement_target_update
BEFORE UPDATE ON expiration_enforcement
WHEN NEW.target_generation<=0 OR length(NEW.target_payload_hash)<>64 OR NEW.state NOT IN ('pending','applying','enforced','failed','superseded')
BEGIN SELECT RAISE(ABORT,'invalid expiration enforcement target'); END;
`,
	},
	{
		version: 27,
		name:    "subscription_token_ciphertext",
		sql: `
ALTER TABLE subscription_tokens ADD COLUMN token_ciphertext TEXT NOT NULL DEFAULT '';
`,
	},
}

func migrationChecksum(m migration) string {
	digest := sha256.Sum256([]byte(m.name + "\x00" + m.sql))
	return hex.EncodeToString(digest[:])
}

// Migrate applies all pending migrations in order. Each migration runs in its
// own transaction so a failure cannot leave a half-applied schema. Safe to run
// repeatedly; already-applied versions are skipped.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
	  version INTEGER PRIMARY KEY,
	  name TEXT NOT NULL,
	  checksum TEXT NOT NULL DEFAULT '',
	  applied_at INTEGER NOT NULL DEFAULT (strftime('%s','now'))
	)`); err != nil {
		return fmt.Errorf("storage: create schema_migrations: %w", err)
	}
	var hasChecksum int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('schema_migrations') WHERE name='checksum'`).Scan(&hasChecksum); err != nil {
		return fmt.Errorf("storage: inspect schema_migrations: %w", err)
	}
	if hasChecksum == 0 {
		if _, err := db.Exec(`ALTER TABLE schema_migrations ADD COLUMN checksum TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("storage: add migration checksum: %w", err)
		}
	}

	rows, err := db.Query(`SELECT version, name, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return fmt.Errorf("storage: read migration history: %w", err)
	}
	current := 0
	missingChecksums := make([]struct {
		version  int
		checksum string
	}, 0)
	for rows.Next() {
		var version int
		var name, checksum string
		if err := rows.Scan(&version, &name, &checksum); err != nil {
			rows.Close()
			return err
		}
		if version != current+1 || version > len(migrations) || migrations[version-1].name != name {
			rows.Close()
			return fmt.Errorf("storage: invalid ordered migration history at version %d", version)
		}
		expectedChecksum := migrationChecksum(migrations[version-1])
		if checksum == "" {
			missingChecksums = append(missingChecksums, struct {
				version  int
				checksum string
			}{version: version, checksum: expectedChecksum})
		} else if checksum != expectedChecksum {
			rows.Close()
			return fmt.Errorf("storage: historical migration checksum mismatch at version %d", version)
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
	for _, missing := range missingChecksums {
		if _, err := db.Exec(`UPDATE schema_migrations SET checksum=? WHERE version=? AND checksum=''`, missing.checksum, missing.version); err != nil {
			return fmt.Errorf("storage: backfill migration checksum %d: %w", missing.version, err)
		}
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
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, name, checksum) VALUES(?, ?, ?)`, m.version, m.name, migrationChecksum(m)); err != nil {
			tx.Rollback()
			return fmt.Errorf("storage: record migration %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("storage: commit migration %d: %w", m.version, err)
		}
	}
	quickRows, err := db.Query(`PRAGMA quick_check`)
	if err != nil {
		return fmt.Errorf("storage: quick_check: %w", err)
	}
	quickOK := false
	for quickRows.Next() {
		var result string
		if err := quickRows.Scan(&result); err != nil {
			quickRows.Close()
			return fmt.Errorf("storage: quick_check result: %w", err)
		}
		if result != "ok" {
			quickRows.Close()
			return fmt.Errorf("storage: quick_check failed")
		}
		quickOK = true
	}
	if err := quickRows.Close(); err != nil {
		return fmt.Errorf("storage: close quick_check: %w", err)
	}
	if !quickOK {
		return fmt.Errorf("storage: quick_check returned no result")
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
		{"apply jobs", `SELECT EXISTS(SELECT 1 FROM apply_jobs WHERE status NOT IN ('pending','planning','validating','applying','health_check','staged','recovery_pending','succeeded','failed','rolling_back','rolled_back','rollback_failed') OR desired_revision < 0 OR base_revision < 0)`},
		{"revision ordering", `SELECT EXISTS(SELECT 1 FROM revisions WHERE desired_revision < applied_revision OR desired_revision < 0 OR applied_revision < 0)`},
		{"applied revision evidence", `SELECT EXISTS(SELECT 1 FROM revisions r WHERE r.applied_revision > 0 AND (NOT EXISTS (SELECT 1 FROM revision_snapshots s WHERE s.revision=r.applied_revision) OR NOT EXISTS (SELECT 1 FROM runtime_verification v WHERE v.id=1 AND v.status='verified' AND v.verified_revision=r.applied_revision)))`},
		{"runtime verification", `SELECT EXISTS(SELECT 1 FROM runtime_verification WHERE id<>1 OR historical_applied_revision<0 OR verified_revision<0 OR status NOT IN ('verified','unknown','recovering') OR (status='unknown' AND verified_revision<>0))`},
		{"snapshot JSON", `SELECT EXISTS(SELECT 1 FROM revision_snapshots WHERE revision > 0 AND (json_valid(payload)=0 OR trim(payload)=''))`},
		{"publication receipts", `SELECT EXISTS(SELECT 1 FROM runtime_publications WHERE phase NOT IN ('intent','artifacts_prepared','artifacts_committed','services_planned','services_converged','health_verified','firewall_committed','side_effect_planned','side_effect_committed','side_effect_verified','published','finalization_pending','rolled_back','recovery_transferred') OR json_valid(artifacts_json)=0 OR json_valid(operations_json)=0 OR json_valid(confirmations_json)=0 OR json_valid(service_plan_json)=0 OR json_valid(previous_service_states_json)=0 OR json_valid(health_evidence_json)=0 OR (revision > 0 AND length(snapshot_sha256)<>64))`},
		{"apply lease", `SELECT EXISTS(SELECT 1 FROM apply_lease WHERE id<>1 OR generation<0 OR lease_expires_at<0 OR ((owner_process='' OR current_operation='') AND NOT (owner_process='' AND current_operation='')))`},
		{"clients", `SELECT EXISTS(SELECT 1 FROM clients WHERE enabled NOT IN (0,1) OR depleted NOT IN (0,1) OR quota_bytes < 0 OR device_limit < 0 OR quota_reset_policy NOT IN ('never','daily','weekly','monthly') OR version < 1)`},
		{"credentials", `SELECT EXISTS(SELECT 1 FROM client_credentials WHERE length(encrypted_value)<=12 OR key_version<1 OR credential_version<1) OR EXISTS(SELECT 1 FROM client_credentials WHERE revoked_at IS NULL GROUP BY binding_id,kind HAVING COUNT(*)>1)`},
		{"bindings", `SELECT EXISTS(SELECT 1 FROM client_bindings WHERE enabled NOT IN (0,1) OR version < 1)`},
		{"tokens", `SELECT EXISTS(SELECT 1 FROM subscription_tokens WHERE enabled NOT IN (0,1) OR length(token_hash)<>32)`},
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
