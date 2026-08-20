package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/statecommit"
	"github.com/spf13/cobra"
)

func newAdminCommand(hasher PasswordHasher) *cobra.Command {
	var statePath string
	var keyPath string

	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Manage Veil admin accounts",
	}

	resetCmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset administrator credentials with new randomly generated ones",
		RunE: func(cmd *cobra.Command, args []string) error {
			env := serveflow.NewEnvironment()
			resolvedState, _ := env.StatePath(statePath)
			resolvedKey, _ := env.KeyPath(keyPath)

			suffix, err := generateRandomHex(4)
			if err != nil {
				return err
			}
			pass, err := generateRandomHex(16)
			if err != nil {
				return err
			}
			username := "admin_" + suffix

			hashed, err := hasher.Hash([]byte(pass))
			if err != nil {
				return fmt.Errorf("hash password: %w", err)
			}

			if _, err := statecommit.Update(statecommit.UpdateOptions{
				StatePath: resolvedState, KeyPath: resolvedKey, AllowCreate: true,
			}, func(snapshot *model.ManagementSnapshot) error {
				if snapshot.Settings.Mode == "" {
					snapshot.Settings = managementstate.BuildDefaultState(managementstate.DefaultInput{}).Settings
				}
				snapshot.Users = []model.User{{
					Username: username, PasswordHash: string(hashed), Role: "admin",
				}}
				managementstate.CompleteSetupForAdmins(snapshot, time.Now())
				return nil
			}); err != nil {
				return fmt.Errorf("save state: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Admin credentials successfully reset.\n")
			fmt.Fprintf(cmd.OutOrStdout(), "Username: %s\n", username)
			fmt.Fprintf(cmd.OutOrStdout(), "Password: %s\n", pass)
			return nil
		},
	}

	var customUsername string
	var customPassword string
	var customRole string

	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Set or update custom administrative credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if customPassword == "" {
				return fmt.Errorf("--password is required")
			}

			env := serveflow.NewEnvironment()
			resolvedState, _ := env.StatePath(statePath)
			resolvedKey, _ := env.KeyPath(keyPath)

			hashed, err := hasher.Hash([]byte(customPassword))
			if err != nil {
				return fmt.Errorf("hash password: %w", err)
			}

			targetRole := customRole
			if targetRole == "" {
				targetRole = "admin"
			}
			username := customUsername
			if _, err := statecommit.Update(statecommit.UpdateOptions{
				StatePath: resolvedState, KeyPath: resolvedKey, AllowCreate: true,
			}, func(snapshot *model.ManagementSnapshot) error {
				if snapshot.Settings.Mode == "" {
					snapshot.Settings = managementstate.BuildDefaultState(managementstate.DefaultInput{}).Settings
				}
				if username == "" {
					for _, user := range snapshot.Users {
						if user.Role == "admin" {
							username = user.Username
							break
						}
					}
					if username == "" {
						username = "admin"
					}
				}
				updatedUser := model.User{
					Username: username, PasswordHash: string(hashed), Role: targetRole,
				}
				for index := range snapshot.Users {
					if snapshot.Users[index].Username == username {
						snapshot.Users[index] = updatedUser
						managementstate.CompleteSetupForAdmins(snapshot, time.Now())
						return nil
					}
				}
				snapshot.Users = append(snapshot.Users, updatedUser)
				managementstate.CompleteSetupForAdmins(snapshot, time.Now())
				return nil
			}); err != nil {
				return fmt.Errorf("save state: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "User credentials successfully set.\n")
			fmt.Fprintf(cmd.OutOrStdout(), "Username: %s\n", username)
			fmt.Fprintf(cmd.OutOrStdout(), "Role: %s\n", targetRole)
			return nil
		},
	}

	setCmd.Flags().StringVar(&customUsername, "username", "", "username to set or update; defaults to the first administrator in state")
	setCmd.Flags().StringVar(&customPassword, "password", "", "custom password to set (required)")
	setCmd.Flags().StringVar(&customRole, "role", "admin", "role of the user (admin or viewer)")

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show all registered users and roles",
		RunE: func(cmd *cobra.Command, args []string) error {
			env := serveflow.NewEnvironment()
			resolvedState, _ := env.StatePath(statePath)
			resolvedKey, _ := env.KeyPath(keyPath)

			key, err := secrets.LoadOrCreateKey(resolvedKey)
			if err != nil {
				return fmt.Errorf("load key: %w", err)
			}
			cipher, err := secrets.NewCipher(*key)
			if err != nil {
				return fmt.Errorf("new cipher: %w", err)
			}

			store := managementstate.NewStore(resolvedState, cipher)
			snapshot, ok, err := store.Load()
			if err != nil {
				return fmt.Errorf("load state: %w", err)
			}
			if !ok || len(snapshot.Users) == 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "No users registered in state.\n")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Registered users:\n")
			for _, u := range snapshot.Users {
				fmt.Fprintf(cmd.OutOrStdout(), "- %s (Role: %s)\n", u.Username, u.Role)
			}
			return nil
		},
	}

	var newKeyPath string
	rotateKeyCmd := &cobra.Command{
		Use:   "rotate-key",
		Short: "Rotate the AES encryption key and re-encrypt the state file",
		RunE: func(cmd *cobra.Command, args []string) error {
			env := serveflow.NewEnvironment()
			resolvedState, _ := env.StatePath(statePath)
			resolvedKey, _ := env.KeyPath(keyPath)

			targetKeyPath := newKeyPath
			if targetKeyPath == "" {
				targetKeyPath = resolvedKey
			}

			if _, err := statecommit.RotateKey(statecommit.RotateKeyOptions{
				StatePath:     resolvedState,
				KeyPath:       resolvedKey,
				TargetKeyPath: targetKeyPath,
			}); err != nil {
				return fmt.Errorf("rotate state key: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Key successfully rotated.\n")
			fmt.Fprintf(cmd.OutOrStdout(), "New key written to: %s\n", targetKeyPath)
			return nil
		},
	}

	rotateKeyCmd.Flags().StringVar(&newKeyPath, "new-key-path", "", "destination path for the new key (defaults to overwriting the current key)")

	for _, subCmd := range []*cobra.Command{resetCmd, setCmd, showCmd, rotateKeyCmd} {
		subCmd.Flags().StringVar(&statePath, "state", "", "management state JSON path; defaults to VEIL_STATE_PATH or /var/lib/veil/state.json")
		subCmd.Flags().StringVar(&keyPath, "key-path", "", "encryption key file path; defaults to VEIL_KEY_PATH or /etc/veil/state.key")
		cmd.AddCommand(subCmd)
	}

	return cmd
}

func generateRandomHex(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
