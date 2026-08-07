package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"time"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/livevalidation"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/testguard"
)

type ManagementStateLifecycle struct {
	state *managementState
}

func NewManagementStateLifecycle(state *managementState) ManagementStateLifecycle {
	return ManagementStateLifecycle{state: state}
}

func newManagementStateProduction(info ServerInfo) *managementState {
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	keyPath := info.KeyPath
	if keyPath == "" && info.StatePath != "" {
		keyPath = filepath.Join(filepath.Dir(info.StatePath), "state.key")
	}
	applyRoot := info.ApplyRoot
	if applyRoot == "" && info.StatePath != "" {
		// An explicit state file with no apply root is a dev/test-style isolated
		// instance. Keep generated/live files beside that state rather than
		// falling through to the production /etc/veil default. Production serve
		// resolves and passes ApplyRoot explicitly.
		applyRoot = filepath.Join(filepath.Dir(info.StatePath), "staging")
	}
	if keyPath == "" {
		// No StatePath and no KeyPath: this is an ephemeral dev/test state.
		// Production serve always passes explicit paths resolved from
		// flags/VEIL_* env in cliflow/serve, so reaching this branch means an
		// in-memory style state — give it a per-instance isolated key file
		// instead of the live-system default /etc/veil/state.key. Unit tests
		// constructing bare ServerInfo{} stay hermetic even under -shuffle and
		// can never clobber a production key when run as root.
		tmp, err := os.MkdirTemp("", "veil-dev-state-*")
		if err != nil {
			// Extremely unusual (TMPDIR broken); keep the historical default
			// but let the test guard catch it if armed.
			keyPath = "/etc/veil/state.key"
			if runtime.GOOS == "windows" {
				pd := os.Getenv("ProgramData")
				if pd == "" {
					pd = `C:\ProgramData`
				}
				keyPath = filepath.Join(pd, "Veil", "state.key")
			}
			testguard.CheckProductionPath(keyPath)
		} else {
			keyPath = filepath.Join(tmp, "state.key")
		}
	}
	if applyRoot == "" {
		// Bare ServerInfo{} instances receive an ephemeral key above. Keep their
		// generated and live trees under that same isolated root as well.
		applyRoot = filepath.Join(filepath.Dir(keyPath), "staging")
	}
	// NOTE: do not os.Setenv(VEIL_STATE_PATH/VEIL_KEY_PATH) here. Those
	// process-global side effects leak production default paths into unrelated
	// tests (and, when tests run as root, let them modify live state). All
	// in-process consumers must read paths from this state's own fields.
	model := managementstate.BuildDefaultState(managementstate.DefaultInput{
		PanelListen: info.PanelListen,
		PanelAccess: info.PanelAccess,
		WebBasePath: info.WebBasePath,
		Mode:        info.Mode,
		Domain:      info.Domain,
		Email:       info.Email,
	})
	configurationValidator := info.ConfigurationValidator
	enforceConfigurationValidation := configurationValidator != nil
	if configurationValidator == nil {
		configurationValidator = livevalidation.Validator{}
	}
	passwordHasher := info.PasswordHasher
	if passwordHasher == nil {
		passwordHasher = productionPasswordHasher()
	}
	state := &managementState{
		lifecycleCtx:                   lifecycleCtx,
		lifecycleCancel:                lifecycleCancel,
		statePath:                      info.StatePath,
		requireApplyTracking:           info.StatePath != "",
		requirePrivilegedHelper:        info.RequirePrivilegedHelper,
		applyRoot:                      defaultApplyRoot(applyRoot),
		liveRoot:                       info.LiveRoot,
		keyPath:                        keyPath,
		authToken:                      info.AuthToken,
		allowDevAnonymous:              !info.PublicListen,
		setupAllowed:                   info.SetupAllowed,
		settings:                       model.Settings,
		inbounds:                       model.Inbounds,
		rules:                          model.Rules,
		warp:                           model.Warp,
		version:                        info.Version,
		backupJobs:                     make(map[string]BackupRestoreJob),
		configurationValidator:         configurationValidator,
		passwordHasher:                 passwordHasher,
		databaseOpener:                 info.DatabaseOpener,
		enforceConfigurationValidation: enforceConfigurationValidation,
		privileged:                     info.Privileged,
		updateStager:                   info.UpdateStager,
	}
	if state.liveRoot == "" {
		state.liveRoot = filepath.Join(state.applyRoot, "live")
	}
	if state.updateStager == nil {
		updateRoot := filepath.Join(state.applyRoot, "updates")
		if info.StatePath != "" {
			updateRoot = filepath.Join(filepath.Dir(info.StatePath), "updates")
		}
		stager := newPanelUpdateStager(updateRoot)
		state.updateStager = stager.Stage
	}
	sessionPath := ""
	if info.StatePath != "" {
		sessionPath = filepath.Join(filepath.Dir(info.StatePath), "sessions.json")
	}
	sessionRegistry, err := NewSessionRegistry(sessionPath)
	if err != nil {
		log.Printf("error loading Panel sessions from %s: %v", sessionPath, err)
		sessionRegistry = newSessionRegistryWithoutLoad(sessionPath)
		if info, statErr := os.Stat(sessionPath); statErr == nil && info.Mode().IsRegular() {
			suffix, randomErr := generateRandomHex(8)
			if randomErr == nil {
				quarantinePath := sessionPath + ".corrupt-" + suffix
				if renameErr := os.Rename(sessionPath, quarantinePath); renameErr == nil {
					_ = syncSessionDirectory(sessionPath)
					sessionRegistry.mu.Lock()
					recoverErr := sessionRegistry.saveLocked()
					sessionRegistry.mu.Unlock()
					if recoverErr == nil {
						err = nil
						log.Printf("quarantined corrupt Panel session snapshot as %s", quarantinePath)
					} else {
						_ = os.Rename(quarantinePath, sessionPath)
					}
				}
			}
		}
	}
	state.sessions = sessionRegistry
	if info.StatePath != "" {
		state.backupDir = filepath.Join(filepath.Dir(info.StatePath), "backups")
		state.backupJobsPath = filepath.Join(filepath.Dir(info.StatePath), "backup-restore-jobs.json")
		if err := state.loadBackupRestoreJobs(); err != nil {
			log.Printf("error loading backup restore job history: %v", err)
		}
	}
	state.backupPassphrasePath = filepath.Join(filepath.Dir(keyPath), "backup.passphrase")
	auditPath := ""
	if info.StatePath != "" {
		auditPath = filepath.Join(filepath.Dir(info.StatePath), "audit", "panel.jsonl")
	} else if info.ApplyRoot != "" {
		auditPath = filepath.Join(defaultApplyRoot(info.ApplyRoot), "generated", "veil", "audit.log")
	}
	state.audit = audit.NewRecorder(auditPath, audit.RecorderOptions{})
	if state.privileged == nil && !info.RequirePrivilegedHelper {
		state.privileged = newLocalPrivilegedClient(state)
		state.privilegedLocal = true
	}
	lifecycle := NewManagementStateLifecycle(state)
	if err := lifecycle.loadCoherentStateLocked(); err != nil {
		log.Printf("error loading coherent management state for %s: %v", info.StatePath, err)
		if info.StatePath != "" {
			state.startupStateLoadFailed = true
			state.startupStateLoadErr = err
			var privilegedErr *privileged.Error
			state.startupPrivilegedFailure = errors.As(err, &privilegedErr)
			state.allowDevAnonymous = false
		}
	}
	// Now that the cipher is available, wire the client domain subsystem
	// (repository + encrypted credentials + service) onto the same SQLite store.
	initClientSubsystem(state)

	// Blocker A3: legacy-profile migration at NORMAL startup/upgrade, not only
	// on SIGHUP reload. Never mutate SQLite after state/revision recovery failed;
	// the mismatch must remain fail-closed and diagnosable.
	if !state.startupStateLoadFailed {
		if err := lifecycle.StartupMigrateLegacyLocked(); err != nil {
			log.Printf("startup legacy migration: %v", err)
		}
	}

	return state
}

func (l ManagementStateLifecycle) loadOrCreateCipher() error {
	key, err := secrets.LoadOrCreateKey(l.state.keyPath)
	if err != nil {
		return err
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		return err
	}
	l.state.cipher = cipher
	return nil
}

// RecoverPendingKeyRotation runs all root-owned management-state recovery
// journals through the privileged helper before Panel opens state.key or
// veil.db. The operation name remains backward compatible with older helpers.
func (l ManagementStateLifecycle) RecoverPendingKeyRotation() error {
	return l.RecoverPendingKeyRotationContext(context.Background())
}

func (l ManagementStateLifecycle) RecoverPendingKeyRotationContext(ctx context.Context) error {
	if l.state.statePath == "" {
		return nil
	}
	if l.state.privileged == nil {
		return errors.New("recover interrupted key rotation: privileged helper is unavailable")
	}
	if err := l.state.privileged.RecoverKeyRotation(ctx, privileged.RecoverKeyRotationRequest{}); err != nil {
		return fmt.Errorf("recover interrupted key rotation through privileged helper: %w", err)
	}
	return nil
}

func (l ManagementStateLifecycle) loadCoherentStateLocked() error {
	if err := l.RecoverPendingKeyRotation(); err != nil {
		return err
	}
	return managementstate.WithSnapshotBarrier(l.state.statePath, func() error {
		if l.state.keyPath != "" {
			if err := l.loadOrCreateCipher(); err != nil {
				return fmt.Errorf("load encryption key: %w", err)
			}
		} else if l.state.statePath != "" && l.state.cipher == nil {
			return errors.New("load encryption key: key path is unavailable for persistent state")
		}
		// Backup restore closes the SQLite domain before replacing veil.db.
		if l.state.statePath != "" && l.state.db == nil {
			initApplySubsystem(l.state)
			if l.state.db == nil {
				return errors.New("reload database: open restored veil.db failed")
			}
		}
		if l.state.statePath == "" {
			return nil
		}
		if err := l.recoverPendingCommitLocked(); err != nil {
			return fmt.Errorf("recover interrupted state mutation: %w", err)
		}
		if err := l.verifyCurrentStateRevisionLocked(); err != nil {
			return fmt.Errorf("verify state revision: %w", err)
		}
		if err := l.Load(); err != nil {
			return fmt.Errorf("load state: %w", err)
		}
		return nil
	})
}

func (l ManagementStateLifecycle) SnapshotLocked() (managementSnapshot, error) {
	input := managementstate.SnapshotInput{
		EffectiveAt:   time.Now().UTC().Unix(),
		Setup:         l.state.setup,
		Settings:      l.state.settings,
		Inbounds:      l.state.inbounds,
		Rules:         l.state.rules,
		RoutingPreset: l.state.routingPreset,
		RoutingSource: l.state.routingSource,
		Warp:          l.state.warp,
		Users:         l.state.users,
	}
	// A3: freeze normalized client state so an apply job for revision N renders
	// exactly the configuration committed as revision N, never newer mutable
	// state. Load all clients, bindings, and active credentials from the repo.
	if l.state.clientRepo != nil {
		clients, err := l.state.clientRepo.AllClients()
		if err != nil {
			return managementSnapshot{}, fmt.Errorf("snapshot: read clients: %w", err)
		}
		bindings, err := l.state.clientRepo.AllBindings()
		if err != nil {
			return managementSnapshot{}, fmt.Errorf("snapshot: read bindings: %w", err)
		}
		creds, err := l.state.clientRepo.AllActiveCredentials()
		if err != nil {
			return managementSnapshot{}, fmt.Errorf("snapshot: read credentials: %w", err)
		}
		input.Clients, input.Bindings, input.Credentials = clientSnapshotRows(clients, bindings, creds)
	}
	return managementstate.BuildSnapshot(input), nil
}

func (l ManagementStateLifecycle) SaveLocked() error {
	return managementstate.WithSnapshotBarrier(l.state.statePath, func() error {
		store := managementstate.NewStore(l.state.statePath, l.state.cipher)
		snapshot, err := l.SnapshotLocked()
		if err != nil {
			return err
		}
		if l.state.statePath == "" {
			if err := store.Save(snapshot); err != nil {
				return err
			}
			_, err := l.state.bumpDesiredRevisionLocked()
			return err
		}
		if !l.state.applyTrackingEnabled() {
			if l.state.requireApplyTracking {
				return errors.New("apply subsystem unavailable for persistent management-state mutation")
			}
			return store.Save(snapshot)
		}
		revisions, err := l.state.applyRevisions.Get()
		if err != nil {
			return fmt.Errorf("read desired revision before state mutation: %w", err)
		}
		encoded, err := store.Marshal(snapshot)
		if err != nil {
			return err
		}
		commit, err := store.PrepareStateCommit(encoded, revisions.Desired, revisions.Desired+1)
		if err != nil {
			return err
		}
		if _, err := l.state.bumpDesiredRevisionLocked(commit.Journal().IntendedStateSHA256); err != nil {
			if rollbackErr := commit.Rollback(); rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("restore previous management state: %w", rollbackErr))
			}
			return err
		}
		// SQLite is committed. Marker cleanup is recoverable at startup, so do not
		// report a failed API mutation after both durable stores have committed.
		if err := commit.Finalize(); err != nil {
			log.Printf("management state: committed mutation left recovery marker: %v", err)
		}
		return nil
	})
}

// RecoverPendingCommit deterministically reconciles an interrupted
// state-file/SQLite commit before state.json is loaded into memory.
func (l ManagementStateLifecycle) RecoverPendingCommit() error {
	return managementstate.WithSnapshotBarrier(l.state.statePath, l.recoverPendingCommitLocked)
}

func (l ManagementStateLifecycle) recoverPendingCommitLocked() error {
	store := managementstate.NewStore(l.state.statePath, l.state.cipher)
	commit, ok, err := store.LoadPendingStateCommit()
	if err != nil || !ok {
		return err
	}
	if !l.state.applyTrackingEnabled() {
		return errors.New("pending management-state mutation exists but apply subsystem is unavailable")
	}
	journal := commit.Journal()
	revisions, err := l.state.applyRevisions.Get()
	if err != nil {
		return fmt.Errorf("read desired revision for pending mutation: %w", err)
	}
	currentDigest, currentExists, err := commit.CurrentStateDigest()
	if err != nil {
		return fmt.Errorf("read state for pending mutation: %w", err)
	}
	matchesPrevious := currentExists == journal.PreviousStateExists && currentDigest == journal.PreviousStateSHA256
	matchesIntended := currentExists && currentDigest == journal.IntendedStateSHA256
	switch revisions.Desired {
	case journal.PreviousRevision:
		if l.state.applySnapshots.Has(journal.IntendedRevision) {
			return fmt.Errorf("pending mutation revision %d has a snapshot but is not desired", journal.IntendedRevision)
		}
		if !matchesPrevious && !matchesIntended {
			return errors.New("state file matches neither side of the pending mutation")
		}
		return commit.Rollback()
	case journal.IntendedRevision:
		if !l.state.applySnapshots.Has(journal.IntendedRevision) {
			return fmt.Errorf("pending committed revision %d has no immutable snapshot", journal.IntendedRevision)
		}
		snapshotDigest, err := l.state.applySnapshots.StateDigest(journal.IntendedRevision)
		if err != nil {
			return err
		}
		if snapshotDigest != journal.IntendedStateSHA256 {
			return fmt.Errorf("pending committed revision %d snapshot state digest mismatch", journal.IntendedRevision)
		}
		if !matchesIntended {
			return fmt.Errorf("state file does not match committed pending revision %d", journal.IntendedRevision)
		}
		return commit.Finalize()
	default:
		return fmt.Errorf("pending mutation expects desired revision %d or %d, database has %d", journal.PreviousRevision, journal.IntendedRevision, revisions.Desired)
	}
}

// VerifyCurrentStateRevision prevents startup from loading a state file that is
// not cryptographically associated with the recorded desired revision.
func (l ManagementStateLifecycle) VerifyCurrentStateRevision() error {
	return managementstate.WithSnapshotBarrier(l.state.statePath, l.verifyCurrentStateRevisionLocked)
}

func (l ManagementStateLifecycle) verifyCurrentStateRevisionLocked() error {
	if l.state.statePath == "" {
		return nil
	}
	if !l.state.applyTrackingEnabled() {
		return errors.New("apply subsystem unavailable for management-state revision verification")
	}
	revisions, err := l.state.applyRevisions.Get()
	if err != nil {
		return fmt.Errorf("read desired revision: %w", err)
	}
	if revisions.Desired == 0 {
		return nil
	}
	currentDigest, err := stateFileDigest(l.state.statePath)
	if err != nil {
		return fmt.Errorf("digest management state: %w", err)
	}
	storedDigest, err := l.state.applySnapshots.StateDigest(revisions.Desired)
	if err != nil {
		return err
	}
	if storedDigest != "" {
		if storedDigest != currentDigest {
			return fmt.Errorf("management state digest does not match desired revision %d", revisions.Desired)
		}
		return nil
	}

	// Migration v4 adds the digest column to legacy snapshot rows. Bind it
	// only after proving the decrypted state-file projection is identical to
	// the desired immutable snapshot; otherwise fail closed.
	store := managementstate.NewStore(l.state.statePath, l.state.cipher)
	fileSnapshot, ok, err := store.Load()
	if err != nil {
		return fmt.Errorf("load management state for legacy digest binding: %w", err)
	}
	if !ok {
		return errors.New("management state is missing for nonzero desired revision")
	}
	payload, err := l.state.applySnapshots.Load(revisions.Desired)
	if err != nil {
		return err
	}
	var revisionSnapshot managementSnapshot
	if err := json.Unmarshal(payload, &revisionSnapshot); err != nil {
		return fmt.Errorf("decode desired revision snapshot %d: %w", revisions.Desired, err)
	}
	if err := l.state.decryptSnapshot(&revisionSnapshot); err != nil {
		return fmt.Errorf("decrypt desired revision snapshot %d: %w", revisions.Desired, err)
	}
	if !reflect.DeepEqual(stateFileProjection(fileSnapshot), stateFileProjection(revisionSnapshot)) {
		return fmt.Errorf("legacy management state does not match desired revision %d", revisions.Desired)
	}
	return l.state.applySnapshots.BindStateDigest(revisions.Desired, currentDigest)
}

func stateFileProjection(snapshot managementSnapshot) managementSnapshot {
	return managementstate.BuildSnapshot(managementstate.SnapshotInput{
		Setup:         snapshot.Setup,
		Settings:      snapshot.Settings,
		Inbounds:      snapshot.Inbounds,
		Rules:         snapshot.Rules,
		RoutingPreset: snapshot.RoutingPreset,
		RoutingSource: snapshot.RoutingSource,
		Warp:          snapshot.Warp,
		Users:         snapshot.Users,
	})
}

func (l ManagementStateLifecycle) Load() error {
	snapshot, ok, err := managementstate.NewStore(l.state.statePath, l.state.cipher).Load()
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	ApplyManagementSnapshot(l.state, snapshot)
	return nil
}

func (l ManagementStateLifecycle) ReloadLocked() error {
	if err := l.loadCoherentStateLocked(); err != nil {
		return err
	}
	if l.state.statePath != "" {
		// Reload may have replaced the master cipher. Rebuild the normalized
		// client subsystem even when it was already initialized so no service
		// retains the pre-rotation credential cipher.
		initClientSubsystem(l.state)
	}
	// A6: auto-migrate legacy inbound-embedded profiles to normalized
	// Client+Binding+Credential on startup/upgrade. Idempotent (stable derived
	// client IDs) so safe to run every boot. Runs AFTER state load so legacy
	// profiles are visible.
	if err := l.AutoMigrateLegacyLocked(); err != nil {
		// Non-fatal: log and continue. Manual migration via API still available.
		log.Printf("auto-migrate legacy profiles: %v", err)
	}
	return nil
}

// AutoMigrateLegacyLocked converts any legacy inbound-embedded profiles into
// the normalized model. It now shares the blocker-A3 startup path (pre-flight
// backup, migration marker/version, in-transaction verification, idempotent
// fast path) defined in startup_migration.go. Caller must hold l.state.mu.
func (l ManagementStateLifecycle) AutoMigrateLegacyLocked() error {
	return l.StartupMigrateLegacyLocked()
}

func ApplyManagementSnapshot(state *managementState, snapshot managementSnapshot) {
	if state == nil {
		return
	}
	managementstate.ApplySnapshot(managementstate.SnapshotTarget{
		Setup:         &state.setup,
		Settings:      &state.settings,
		Inbounds:      &state.inbounds,
		Rules:         &state.rules,
		RoutingPreset: &state.routingPreset,
		RoutingSource: &state.routingSource,
		Warp:          &state.warp,
		Users:         &state.users,
	}, snapshot)
}

func defaultApplyRoot(root string) string {
	if root != "" {
		return root
	}
	if runtime.GOOS == "windows" {
		pd := os.Getenv("ProgramData")
		if pd == "" {
			pd = `C:\ProgramData`
		}
		return filepath.Join(pd, "Veil")
	}
	return "/etc/veil"
}

// randomReader is swapped in tests to exercise generateRandomHex error paths.
var randomReader = rand.Read

func generateRandomHex(length int) (string, error) {
	b := make([]byte, length/2)
	if _, err := randomReader(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
