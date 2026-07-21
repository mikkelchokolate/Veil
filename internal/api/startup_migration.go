package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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
//   - BACKUP: before the first migration run, a consistent copy of the state
//     file and the SQLite store is written under
//     backupDir/migrations/legacy-profiles-<ts>/ (the DB copy uses
//     VACUUM INTO, which is consistent even on a live database).
//   - MARKER: a migration_markers row (key "legacy_profiles", version 1)
//     records completion; later boots with the marker present take the fast
//     path and skip both backup and migration.
//   - VERIFICATION: inside the same transaction, every migratable legacy
//     profile is verified to have a normalized client + binding + active
//     credential; any mismatch rolls the migration back instead of marking
//     success over a broken state.
//   - IDEMPOTENT: stable derived client IDs make re-runs no-ops even when the
//     marker table is unavailable (e.g. tests without a StatePath).

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
	markerAvailable := s.clientRepo != nil && s.db != nil
	var marker *client.MigrationMarker
	if markerAvailable {
		m, err := s.clientRepo.GetMigrationMarker(legacyProfilesMarkerKey)
		if err != nil {
			return fmt.Errorf("read migration marker: %w", err)
		}
		marker = m
	}
	if len(pending) == 0 {
		// Nothing to migrate. Deliberately no marker write here: the marker is
		// a record of a completed migration, and must never suppress a future
		// migration if legacy profiles appear later (e.g. state file restored
		// from an older backup after this boot).
		return nil
	}
	if marker != nil && marker.Version >= legacyProfilesMarkerVersion {
		// Already migrated by a previous boot. Legacy profiles intentionally
		// remain in the state file (they render from normalized clients), so
		// there is nothing further to do.
		return nil
	}

	// Backup BEFORE mutating anything (best-effort when no backupDir, e.g.
	// in-memory tests).
	backupPath, err := l.backupForLegacyMigration()
	if err != nil {
		return fmt.Errorf("pre-migration backup: %w", err)
	}

	// Migrate + verify + marker in ONE transaction: either every migratable
	// profile is normalized and verified, or nothing is committed.
	created := 0
	if s.clientRepo == nil {
		return fmt.Errorf("client store unavailable for legacy migration")
	}
	err = s.clientRepo.WithTx(func(tx *client.Tx) error {
		for _, in := range pending {
			res, err := s.clientMigrator.MigrateInboundProfilesTx(tx, in.Name, in.Protocol, in.Profiles)
			if err != nil {
				return fmt.Errorf("migrate inbound %s: %w", in.Name, err)
			}
			created += res.ClientsCreated
		}
		for _, in := range pending {
			if err := s.clientMigrator.VerifyInboundProfiles(tx, in.Name, in.Profiles); err != nil {
				return err
			}
		}
		details, _ := json.Marshal(map[string]any{
			"clientsCreated": created,
			"backup":         backupPath,
		})
		return tx.PutMigrationMarker(client.MigrationMarker{
			Key:       legacyProfilesMarkerKey,
			Version:   legacyProfilesMarkerVersion,
			AppliedAt: time.Now().Unix(),
			Details:   string(details),
		})
	})
	if err != nil {
		return err
	}
	if created > 0 {
		log.Printf("startup: migrated %d legacy profiles to normalized clients (backup: %s)", created, backupPath)
	}
	return nil
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
	dir := filepath.Join(s.backupDir, "migrations", fmt.Sprintf("legacy-profiles-%d", time.Now().Unix()))
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
