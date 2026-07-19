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
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/livevalidation"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
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
	if keyPath == "" {
		if runtime.GOOS == "windows" {
			pd := os.Getenv("ProgramData")
			if pd == "" {
				pd = `C:\ProgramData`
			}
			keyPath = filepath.Join(pd, "Veil", "state.key")
		} else {
			keyPath = "/etc/veil/state.key"
		}
	}
	if info.StatePath != "" {
		os.Setenv("VEIL_STATE_PATH", info.StatePath)
	}
	if keyPath != "" {
		os.Setenv("VEIL_KEY_PATH", keyPath)
	}
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
		applyRoot:                      defaultApplyRoot(info.ApplyRoot),
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
		if clients, err := l.state.clientRepo.AllClients(); err == nil {
			input.Clients = make([]model.ClientSnapshot, 0, len(clients))
			for _, c := range clients {
				input.Clients = append(input.Clients, model.ClientSnapshot{
					ID: c.ID, Name: c.Name, Email: c.Email, Enabled: c.Enabled,
					GroupID: c.GroupID, QuotaBytes: c.QuotaBytes, QuotaResetPolicy: c.QuotaResetPolicy,
					QuotaResetAt: c.QuotaResetAt, ExpiresAt: c.ExpiresAt, DeviceLimit: c.DeviceLimit,
					Depleted: c.Depleted, Version: c.Version,
				})
			}
		}
		if bindings, err := l.state.clientRepo.AllBindings(); err == nil {
			input.Bindings = make([]model.BindingSnapshot, 0, len(bindings))
			for _, b := range bindings {
				input.Bindings = append(input.Bindings, model.BindingSnapshot{
					ID: b.ID, ClientID: b.ClientID, InboundID: b.InboundID, Enabled: b.Enabled,
					ProtocolSettings: b.ProtocolSettings, Version: b.Version,
				})
			}
		}
		if creds, err := l.state.clientRepo.AllActiveCredentials(); err == nil {
			input.Credentials = make([]model.CredentialSnapshot, 0, len(creds))
			for _, c := range creds {
				input.Credentials = append(input.Credentials, model.CredentialSnapshot{
					ID: c.ID, BindingID: c.BindingID, Kind: c.Kind, EncryptedValue: c.EncryptedValue,
					KeyVersion: c.KeyVersion, CredentialVersion: c.CredentialVersion,
				})
			}
		}
	}
	return managementstate.BuildSnapshot(input)
}

func (l ManagementStateLifecycle) SaveLocked() error {
	if err := managementstate.NewStore(l.state.statePath, l.state.cipher).Save(l.SnapshotLocked()); err != nil {
		return err
	}
	// The configuration mutation is committed; record a new desired revision so
	// the system reports desired != applied until an apply job succeeds.
	l.state.bumpDesiredRevisionLocked()
	return nil
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
// the normalized model. Idempotent: clients already migrated (by stable ID)
// are skipped. Caller must hold l.state.mu.
func (l ManagementStateLifecycle) AutoMigrateLegacyLocked() error {
	if l.state.clientMigrator == nil {
		return nil
	}
	migrated := 0
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
		res, err := l.state.clientMigrator.MigrateInboundProfiles(in.Name, in.Protocol, profiles)
		if err != nil {
			return fmt.Errorf("migrate inbound %s: %w", in.Name, err)
		}
		migrated += res.ClientsCreated
	}
	if migrated > 0 {
		log.Printf("auto-migrated %d legacy profiles to normalized clients", migrated)
	}
	return nil
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
