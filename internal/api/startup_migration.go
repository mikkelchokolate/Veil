package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/client"
)

// startup_migration.go — blocker A3: legacy-profile migration at NORMAL
// startup/upgrade. Previously the migrator ran only from ReloadLocked(),
// which the server lifecycle invokes only on SIGHUP — so upgrades never
// migrated until an operator reloaded by hand. The startup path here is
// idempotent and self-protecting:
//
//   - FINGERPRINT EVERY BOOT: the CURRENT legacy profile set is checked for
//     representation (stable derived client ID + binding) on every startup.
//     A restored older state file may carry profiles that were not
//     represented when the marker was written, so the marker is an audit
//     record, never a skip gate.
//   - BACKUP: before migrating, a consistent copy of the state file and the
//     SQLite store is written under backupDir/migrations/legacy-profiles-<ts>/
//     (the DB copy uses VACUUM INTO, consistent even on a live database).
//   - ORCHESTRATION: the migration runs through commitClientMutationLocked —
//     normalized clients, the desired-revision bump, and the immutable
//     snapshot commit in ONE transaction, and one apply job runs for the new
//     revision. The system can never report synced while migrated state
//     differs from the runtime.
//   - VERIFICATION: inside the same transaction, every newly migrated profile
//     is verified to have a normalized client + binding + active credential;
//     any mismatch rolls the migration (and the revision) back.
//   - IDEMPOTENT: stable derived client IDs make re-runs no-ops.

const (
	legacyProfilesMarkerKey     = "legacy_profiles"
	legacyProfilesMarkerVersion = 1
)

// StartupMigrateLegacyLocked migrates legacy inbound-embedded profiles to
// normalized Clients/Bindings/Credentials at startup (and reload). Caller
// must hold s.mu (or run during single-threaded init). Errors are returned so
// the caller can surface them loudly; startup treats them as non-fatal
// (logged) so a migration hiccup never prevents the panel from booting.
func (l ManagementStateLifecycle) StartupMigrateLegacyLocked() error {
	s := l.state
	if s.clientMigrator == nil {
		return nil
	}
	pending := l.legacyProfileInbounds()
	if len(pending) == 0 {
		// Nothing to migrate. Deliberately no marker write here: the marker is
		// a record of a completed migration, and must never suppress a future
		// migration if legacy profiles appear later (e.g. state file restored
		// from an older backup after this boot).
		return nil
	}
	if s.clientRepo == nil {
		return fmt.Errorf("client store unavailable for legacy migration")
	}

	// Fingerprint the CURRENT legacy set: find profiles with no normalized
	// representation yet. Read-only pass; runs on every startup.
	type inboundMissing struct {
		Name     string
		Protocol string
		Profiles []client.LegacyProfile
	}
	var missing []inboundMissing
	missingCount := 0
	if err := s.clientRepo.WithTx(func(tx *client.Tx) error {
		for _, in := range pending {
			m, err := s.clientMigrator.MissingInboundProfiles(tx, in.Name, in.Profiles)
			if err != nil {
				return err
			}
			if len(m) > 0 {
				missing = append(missing, inboundMissing{Name: in.Name, Protocol: in.Protocol, Profiles: m})
				missingCount += len(m)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("verify legacy profile representation: %w", err)
	}
	if missingCount == 0 {
		// Every current legacy profile is already represented (regardless of
		// what the marker says) — nothing to do.
		return nil
	}

	// Backup BEFORE mutating anything (best-effort when no backupDir, e.g.
	// in-memory tests).
	backupPath, err := l.backupForLegacyMigration()
	if err != nil {
		return fmt.Errorf("pre-migration backup: %w", err)
	}

	// Migrate + verify + marker + desired-revision bump + immutable snapshot
	// in ONE transaction through the unified mutation orchestration.
	created := 0
	if _, err := s.commitClientMutationLocked(func(tx *client.Tx) error {
		for _, in := range missing {
			res, err := s.clientMigrator.MigrateInboundProfilesTx(tx, in.Name, in.Protocol, in.Profiles)
			if err != nil {
				return fmt.Errorf("migrate inbound %s: %w", in.Name, err)
			}
			created += res.ClientsCreated
		}
		for _, in := range missing {
			if err := s.clientMigrator.VerifyInboundProfiles(tx, in.Name, in.Profiles); err != nil {
				return err
			}
		}
		details, _ := json.Marshal(map[string]any{
			"clientsCreated": created,
			"backup":         backupPath,
			"fingerprint":    l.legacyProfileFingerprint(pending),
		})
		return tx.PutMigrationMarker(client.MigrationMarker{
			Key:       legacyProfilesMarkerKey,
			Version:   legacyProfilesMarkerVersion,
			AppliedAt: time.Now().Unix(),
			Details:   string(details),
		})
	}); err != nil {
		return err
	}

	// One apply job for the new revision: the runtime must converge to the
	// migrated state before anything can report synced.
	s.autoApplyResultLocked(nil, "system")

	if created > 0 {
		log.Printf("startup: migrated %d legacy profiles to normalized clients (backup: %s)", created, backupPath)
	}
	return nil
}

// legacyProfileFingerprint hashes the sorted stable client IDs of the current
// legacy profile set. Recorded in the migration marker details so operators
// can tell WHICH set a migration covered; never used as a skip gate.
func (l ManagementStateLifecycle) legacyProfileFingerprint(pending []struct {
	Name     string
	Protocol string
	Profiles []client.LegacyProfile
}) string {
	var ids []string
	for _, in := range pending {
		for _, p := range in.Profiles {
			if p.Username == "" || p.Password == "" {
				continue
			}
			ids = append(ids, client.StableClientID(in.Name, p.Username))
		}
	}
	sort.Strings(ids)
	sum := sha256.Sum256([]byte(strings.Join(ids, "\n")))
	return hex.EncodeToString(sum[:16])
}

// legacyProfileInbounds collects inbounds that still carry legacy embedded
// profiles, converted to the migrator's profile shape. Caller must hold s.mu
// (or run during single-threaded init).
func (l ManagementStateLifecycle) legacyProfileInbounds() []struct {
	Name     string
	Protocol string
	Profiles []client.LegacyProfile
} {
	var out []struct {
		Name     string
		Protocol string
		Profiles []client.LegacyProfile
	}
	for _, in := range l.state.inbounds {
		if len(in.Profiles) == 0 {
			continue
		}
		profiles := make([]client.LegacyProfile, 0, len(in.Profiles))
		for _, p := range in.Profiles {
			profiles = append(profiles, client.LegacyProfile{
				Name: p.Name, Username: p.Username, Password: p.Password, Enabled: p.Enabled,
			})
		}
		out = append(out, struct {
			Name     string
			Protocol string
			Profiles []client.LegacyProfile
		}{Name: in.Name, Protocol: in.Protocol, Profiles: profiles})
	}
	return out
}

// backupForLegacyMigration writes a pre-migration backup directory containing
// a copy of the state file and a consistent SQLite snapshot. Returns the
// backup directory path (empty when no backupDir is configured, e.g. tests).
func (l ManagementStateLifecycle) backupForLegacyMigration() (string, error) {
	s := l.state
	if s.backupDir == "" {
		return "", nil
	}
	// Second-granularity timestamps collide when two migrations run within
	// the same second (e.g. boot 2 right after a state restore): VACUUM INTO
	// refuses to overwrite the existing file and the migration would abort
	// before touching any data. Nanoseconds + a short random suffix keep the
	// name unique while staying sortable.
	suffix, err := generateRandomHex(4)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(s.backupDir, "migrations", fmt.Sprintf("legacy-profiles-%d-%s", time.Now().UnixNano(), suffix))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if s.statePath != "" {
		dst := filepath.Join(dir, "state.json.bak")
		if err := copyFileForBackup(s.statePath, dst); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("backup state file: %w", err)
		}
	}
	if s.db != nil {
		// VACUUM INTO produces a consistent backup of a live SQLite database.
		// The path is constructed by us; quote defensively (no parameters
		// supported in VACUUM INTO).
		dst := filepath.Join(dir, "veil.db.bak")
		quoted := "'" + strings.ReplaceAll(dst, "'", "''") + "'"
		if _, err := s.db.Exec(`VACUUM INTO ` + quoted); err != nil {
			return "", fmt.Errorf("backup database: %w", err)
		}
	}
	return dir, nil
}

func copyFileForBackup(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
