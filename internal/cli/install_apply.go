package cli

import (
	"fmt"
	"os"
	"path/filepath"

	installflow "github.com/mikkelchokolate/Veil/internal/cliflow/install"
	"github.com/mikkelchokolate/Veil/internal/hostaccess"
	"github.com/mikkelchokolate/Veil/internal/installer"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/service"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
)

var installSystemdRunFunc = func(actions []service.SystemdAction) error {
	return service.RunSystemdActions(service.ExecRunner{}, actions)
}

var installExecutableFunc = os.Executable
var installPrepareHostFunc = hostaccess.Prepare

func applyRURecommendedInstall(cmd *cobra.Command, profile installer.RURecommendedProfile, opts ruRecommendedInstallOptions) error {
	actualBackupDir := opts.BackupDir
	if !opts.BackupDirSet {
		actualBackupDir = filepath.Join(opts.VarDir, "backups")
	}
	systemdDir := opts.SystemdDir
	if systemdDir == "" {
		systemdDir = defaultSystemdDir
	}
	veilBinary, err := installExecutableFunc()
	if err != nil {
		veilBinary = ""
	}

	// 1. Ensure configuration and state directories exist before running install and writing files
	if err := os.MkdirAll(opts.EtcDir, 0755); err != nil {
		return fmt.Errorf("create etc directory: %w", err)
	}
	if err := os.MkdirAll(opts.VarDir, 0755); err != nil {
		return fmt.Errorf("create var directory: %w", err)
	}

	// 2. Initialize state.key and encrypted state.json with generated credentials
	resolvedKeyPath := filepath.Join(opts.EtcDir, "state.key")
	resolvedStatePath := filepath.Join(opts.VarDir, "state.json")

	key, err := secrets.LoadOrCreateKey(resolvedKeyPath)
	if err != nil {
		return fmt.Errorf("initialize encryption key: %w", err)
	}
	cipher, err := secrets.NewCipher(*key)
	if err != nil {
		return fmt.Errorf("create cipher: %w", err)
	}

	stateExists := false
	if _, err := os.Stat(resolvedStatePath); err == nil {
		stateExists = true
	}

	reusedExistingState := false
	if stateExists {
		// Reuse the existing state.json so an in-place reinstall keeps the admin
		// login and the secret web base path.
		store := managementstate.NewStore(resolvedStatePath, cipher)
		snapshot, ok, err := store.Load()
		if err != nil || !ok {
			// State is present but unreadable (corrupted, or the key no longer
			// matches). Stop instead of silently leaving a panel nobody can log
			// into or overwriting potentially recoverable data.
			return fmt.Errorf(
				"found existing Veil state at %s but could not read it with the encryption key at %s (corrupted state or a mismatched key).\n"+
					"To start fresh: run `sudo veil uninstall` to remove the old state, then reinstall.\n"+
					"To keep the existing data: restore the matching %s before reinstalling",
				resolvedStatePath, resolvedKeyPath, filepath.Base(resolvedKeyPath))
		}
		if snapshot.Settings.WebBasePath != "" {
			profile.WebBasePath = snapshot.Settings.WebBasePath
		}
		// Use the first admin user's username
		for _, u := range snapshot.Users {
			if u.Role == "admin" {
				profile.Username = u.Username
				break
			}
		}
		profile.Password = "" // clear it, as we didn't generate a new one
		reusedExistingState = true
	} else {
		hashed, err := bcrypt.GenerateFromPassword([]byte(profile.Password), 10)
		if err != nil {
			return fmt.Errorf("hash admin password: %w", err)
		}

		defaultState := managementstate.BuildDefaultState(managementstate.DefaultInput{
			PanelListen: profile.PanelListen,
			PanelAccess: profile.PanelAccess,
			WebBasePath: profile.WebBasePath,
			Domain:      profile.Domain,
			Email:       profile.Email,
		})

		initialSnapshot := model.ManagementSnapshot{
			Settings: defaultState.Settings,
			Users: []model.User{
				{
					Username:     profile.Username,
					PasswordHash: string(hashed),
					Role:         "admin",
				},
			},
		}

		store := managementstate.NewStore(resolvedStatePath, cipher)
		if err := store.Save(initialSnapshot); err != nil {
			return fmt.Errorf("write initial state.json: %w", err)
		}
	}

	// 3. Apply profile configurations (veil.env, caddyfile, systemd unit files, etc.)
	result, err := installApplyFunc(profile, installer.ApplyPaths{
		EtcDir:      opts.EtcDir,
		VarDir:      opts.VarDir,
		SystemdDir:  systemdDir,
		BackupDir:   actualBackupDir,
		VeilBinary:  veilBinary,
		CaddyBinary: opts.CaddyBinary,
	})
	if err != nil {
		_ = writeAuditInstall(opts.AuditLog, result.BackupID, false, err.Error(), nil)
		return err
	}
	if shouldPrepareInstallHost(systemdDir) {
		if err := installPrepareHostFunc(hostaccess.Paths{EtcDir: opts.EtcDir, VarDir: opts.VarDir}); err != nil {
			_ = writeAuditInstall(opts.AuditLog, result.BackupID, false, err.Error(), result.WrittenFiles)
			return fmt.Errorf("prepare panel service account and permissions: %w", err)
		}
		if err := installSystemdRunFunc(service.SystemdApplyPlan(installer.PanelSystemdUnits(profile))); err != nil {
			_ = writeAuditInstall(opts.AuditLog, result.BackupID, false, err.Error(), result.WrittenFiles)
			return err
		}
	}
	fmt.Fprintln(cmd.OutOrStdout(), "Written files:")
	for _, path := range result.WrittenFiles {
		fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", path)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", resolvedStatePath)
	fmt.Fprintln(cmd.OutOrStdout())
	fmt.Fprint(cmd.OutOrStdout(), installflow.CredentialSummary(profile))
	if reusedExistingState {
		fmt.Fprintf(cmd.OutOrStdout(), "Reused the existing admin login and panel path from %s; your previous password still applies.\n", resolvedStatePath)
		fmt.Fprintf(cmd.OutOrStdout(), "To set a new password: sudo veil admin set --username %s --password 'NEW' --role admin\n", profile.Username)
		fmt.Fprintln(cmd.OutOrStdout(), "To wipe and start fresh instead: run sudo veil uninstall, then reinstall.")
	}
	if err := writeAuditInstall(opts.AuditLog, result.BackupID, true, "", result.WrittenFiles); err != nil {
		return fmt.Errorf("audit log write failed after successful install: %w", err)
	}
	return nil
}

func shouldPrepareInstallHost(systemdDir string) bool {
	return filepath.Clean(systemdDir) == filepath.Clean(defaultSystemdDir)
}
