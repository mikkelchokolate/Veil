package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/livevalidation"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/testguard"
)

type ManagementStateLifecycle struct {
	state *managementState
}

func NewManagementStateLifecycle(state *managementState) ManagementStateLifecycle {
	return ManagementStateLifecycle{state: state}
}

func newManagementState(info ServerInfo) *managementState {
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
	state := &managementState{
		statePath:                      info.StatePath,
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
	}
	state.sessions = sessionRegistry
	if info.StatePath != "" {
		state.backupDir = filepath.Join(filepath.Dir(info.StatePath), "backups")
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
	initApplySubsystem(state)
	lifecycle := NewManagementStateLifecycle(state)
	if err := lifecycle.loadOrCreateCipher(); err != nil {
		log.Printf("error loading encryption key from %s: %v", keyPath, err)
		if info.StatePath != "" {
			state.startupStateLoadFailed = true
			state.allowDevAnonymous = false
		}
	} else if err := lifecycle.Load(); err != nil {
		log.Printf("error loading management state from %s: %v", info.StatePath, err)
		state.startupStateLoadFailed = true
		state.allowDevAnonymous = false
	}
	// Now that the cipher is available, wire the client domain subsystem
	// (repository + encrypted credentials + service) onto the same SQLite store.
	initClientSubsystem(state)

	// Blocker A3: legacy-profile migration at NORMAL startup/upgrade, not only
	// on SIGHUP reload. Pre-flight backup + migration marker + verification,
	// idempotent. Non-fatal by design: log loudly and keep booting — a broken
	// migration must not take the panel down, and the manual migration API
	// remains available.
	if err := lifecycle.StartupMigrateLegacyLocked(); err != nil {
		log.Printf("startup legacy migration: %v", err)
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

func (l ManagementStateLifecycle) SnapshotLocked() managementSnapshot {
	input := managementstate.SnapshotInput{
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
			log.Printf("snapshot: read clients: %v", err)
		}
		bindings, err := l.state.clientRepo.AllBindings()
		if err != nil {
			log.Printf("snapshot: read bindings: %v", err)
		}
		creds, err := l.state.clientRepo.AllActiveCredentials()
		if err != nil {
			log.Printf("snapshot: read credentials: %v", err)
		}
		input.Clients, input.Bindings, input.Credentials = clientSnapshotRows(clients, bindings, creds)
	}
	return managementstate.BuildSnapshot(input)
}

func (l ManagementStateLifecycle) SaveLocked() error {
	if err := managementstate.NewStore(l.state.statePath, l.state.cipher).Save(l.SnapshotLocked()); err != nil {
		return err
	}
	// The configuration mutation is committed; record a new desired revision so
	// the system reports desired != applied until an apply job succeeds. A
	// revision/snapshot failure fails the mutation honestly (blocker A1): it
	// is returned, not just logged.
	_, err := l.state.bumpDesiredRevisionLocked()
	return err
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
	if l.state.keyPath != "" {
		key, err := secrets.LoadOrCreateKey(l.state.keyPath)
		if err != nil {
			return fmt.Errorf("reload key: %w", err)
		}
		cipher, err := secrets.NewCipher(*key)
		if err != nil {
			return fmt.Errorf("reload cipher: %w", err)
		}
		l.state.cipher = cipher
	}
	if l.state.statePath != "" {
		if err := l.Load(); err != nil {
			return fmt.Errorf("reload state: %w", err)
		}
	}
	// A backup restore closes the SQLite-backed domain before the privileged
	// helper atomically replaces veil.db. Reopen it only after state+key loaded,
	// so every repository observes the restored database and restored cipher.
	if l.state.statePath != "" && l.state.db == nil {
		initApplySubsystem(l.state)
		if l.state.db == nil {
			return fmt.Errorf("reload database: open restored veil.db failed")
		}
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
