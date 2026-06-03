package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"
	"os"

	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"golang.org/x/crypto/bcrypt"
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
		keyPath = "/etc/veil/state.key"
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
	state := &managementState{
		statePath: info.StatePath,
		applyRoot: defaultApplyRoot(info.ApplyRoot),
		keyPath:   keyPath,
		settings:  model.Settings,
		inbounds:  model.Inbounds,
		rules:     model.Rules,
		warp:      model.Warp,
	}
	lifecycle := NewManagementStateLifecycle(state)
	if err := lifecycle.loadOrCreateCipher(); err != nil {
		log.Printf("error loading encryption key from %s: %v", keyPath, err)
	}
	if err := lifecycle.Load(); err != nil {
		log.Printf("error loading management state from %s: %v", info.StatePath, err)
	}

	effectiveMode := state.settings.Mode
	if effectiveMode == "" {
		effectiveMode = info.Mode
	}
	if len(state.users) == 0 && info.AuthToken == "" && info.StatePath != "" && info.KeyPath != "" && effectiveMode != "dev" {
		username, password, err := generateRandomAdminAuth()
		if err == nil {
			hashed, bcryptErr := bcrypt.GenerateFromPassword([]byte(password), 10)
			if bcryptErr == nil {
				state.users = []User{
					{
						Username:     username,
						PasswordHash: string(hashed),
						Role:         "admin",
					},
				}
				if state.settings.WebBasePath == "" {
					state.settings.WebBasePath = generateRandomWebBasePath()
				}
				if saveErr := lifecycle.SaveLocked(); saveErr == nil {
					fmt.Printf("\n========================================================================\n")
					fmt.Printf("VEIL INITIAL ADMIN CREDENTIALS GENERATED\n")
					fmt.Printf("Username: %s\n", username)
					fmt.Printf("Password: %s\n", password)
					fmt.Printf("WebBasePath: %s\n", state.settings.WebBasePath)
					fmt.Printf("Please record these credentials! You can change them later in the Web UI.\n")
					fmt.Printf("========================================================================\n\n")
				} else {
					log.Printf("error saving generated admin credentials: %v", saveErr)
				}
			}
		} else {
			log.Printf("error generating random admin credentials: %v", err)
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

func (l ManagementStateLifecycle) SnapshotLocked() managementSnapshot {
	return managementstate.BuildSnapshot(managementstate.SnapshotInput{
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
	return "/etc/veil"
}

func generateRandomHex(length int) (string, error) {
	b := make([]byte, length/2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generateRandomAdminAuth() (string, string, error) {
	suffix, err := generateRandomHex(4)
	if err != nil {
		return "", "", err
	}
	pass, err := generateRandomHex(16)
	if err != nil {
		return "", "", err
	}
	return "admin_" + suffix, pass, nil
}

func generateRandomWebBasePath() string {
	b := make([]byte, 9)
	if _, err := rand.Read(b); err != nil {
		return "/veil-panel/"
	}
	return "/" + base64.RawURLEncoding.EncodeToString(b) + "/"
}
