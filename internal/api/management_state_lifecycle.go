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
)

type ManagementStateLifecycle struct {
	state *managementState
}

func NewManagementStateLifecycle(state *managementState) ManagementStateLifecycle {
	return ManagementStateLifecycle{state: state}
}

func newManagementState(info ServerInfo) *managementState {
	keyPath := info.KeyPath
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
		keyPath:                        keyPath,
		setupAllowed:                   info.SetupAllowed,
		settings:                       model.Settings,
		inbounds:                       model.Inbounds,
		rules:                          model.Rules,
		warp:                           model.Warp,
		version:                        info.Version,
		backupJobs:                     make(map[string]BackupRestoreJob),
		configurationValidator:         configurationValidator,
		enforceConfigurationValidation: enforceConfigurationValidation,
	}
	sessionPath := ""
	if info.StatePath != "" {
		sessionPath = filepath.Join(filepath.Dir(info.StatePath), "sessions.json")
	}
	sessionRegistry, err := NewSessionRegistry(sessionPath)
	if err != nil {
		log.Printf("error loading Panel sessions from %s: %v", sessionPath, err)
		sessionRegistry = mustNewSessionRegistry("")
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
	lifecycle := NewManagementStateLifecycle(state)
	if err := lifecycle.loadOrCreateCipher(); err != nil {
		log.Printf("error loading encryption key from %s: %v", keyPath, err)
	}
	if err := lifecycle.Load(); err != nil {
		log.Printf("error loading management state from %s: %v", info.StatePath, err)
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
	return managementstate.BuildSnapshot(managementstate.SnapshotInput{
		Setup:         l.state.setup,
		Settings:      l.state.settings,
		Inbounds:      l.state.inbounds,
		Rules:         l.state.rules,
		RoutingPreset: l.state.routingPreset,
		RoutingSource: l.state.routingSource,
		Warp:          l.state.warp,
		Users:         l.state.users,
	})
}

func (l ManagementStateLifecycle) SaveLocked() error {
	return managementstate.NewStore(l.state.statePath, l.state.cipher).Save(l.SnapshotLocked())
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

func generateRandomHex(length int) (string, error) {
	b := make([]byte, length/2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
