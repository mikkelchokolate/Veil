package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"syscall"

	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/bcrypt"
)

func newAdminCommand() *cobra.Command {
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
			if !ok {
				snapshot = model.ManagementSnapshot{
					Settings: managementstate.BuildDefaultState(managementstate.DefaultInput{}).Settings,
				}
			}

			suffix, err := generateRandomHex(4)
			if err != nil {
				return err
			}
			pass, err := generateRandomHex(16)
			if err != nil {
				return err
			}
			username := "admin_" + suffix

			hashed, err := bcrypt.GenerateFromPassword([]byte(pass), 10)
			if err != nil {
				return fmt.Errorf("hash password: %w", err)
			}

			snapshot.Users = []model.User{
				{
					Username:     username,
					PasswordHash: string(hashed),
					Role:         "admin",
				},
			}

			if err := store.Save(snapshot); err != nil {
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
			if !ok {
				snapshot = model.ManagementSnapshot{
					Settings: managementstate.BuildDefaultState(managementstate.DefaultInput{}).Settings,
				}
			}

			hashed, err := bcrypt.GenerateFromPassword([]byte(customPassword), 10)
			if err != nil {
				return fmt.Errorf("hash password: %w", err)
			}

			// If username is not explicitly provided, update the first admin
			username := customUsername
			if username == "" {
				for _, u := range snapshot.Users {
					if u.Role == "admin" {
						username = u.Username
						break
					}
				}
				if username == "" {
					username = "admin"
				}
			}

			foundIndex := -1
			for i, u := range snapshot.Users {
				if u.Username == username {
					foundIndex = i
					break
				}
			}

			targetRole := customRole
			if targetRole == "" {
				targetRole = "admin"
			}

			updatedUser := model.User{
				Username:     username,
				PasswordHash: string(hashed),
				Role:         targetRole,
			}

			if foundIndex >= 0 {
				snapshot.Users[foundIndex] = updatedUser
			} else {
				snapshot.Users = append(snapshot.Users, updatedUser)
			}

			if err := store.Save(snapshot); err != nil {
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

			// Read old key bytes
			oldKeyBytes, err := os.ReadFile(resolvedKey)
			if err != nil {
				return fmt.Errorf("read old key file: %w", err)
			}
			if len(oldKeyBytes) != secrets.KeySize {
				return fmt.Errorf("old key file has wrong length: %d bytes (expected %d)", len(oldKeyBytes), secrets.KeySize)
			}
			var oldKey [secrets.KeySize]byte
			copy(oldKey[:], oldKeyBytes)

			// Load snapshot with old key
			oldCipher, err := secrets.NewCipher(oldKey)
			if err != nil {
				return fmt.Errorf("init cipher with old key: %w", err)
			}
			store := managementstate.NewStore(resolvedState, oldCipher)
			snapshot, ok, err := store.Load()
			if err != nil {
				return fmt.Errorf("load state snapshot: %w", err)
			}
			if !ok {
				return fmt.Errorf("no state found at %s to rotate", resolvedState)
			}

			// Generate new key bytes
			var newKey [secrets.KeySize]byte
			if _, err := rand.Read(newKey[:]); err != nil {
				return fmt.Errorf("generate new key: %w", err)
			}

			// Prepare new cipher and marshal the snapshot using the new key
			newCipher, err := secrets.NewCipher(newKey)
			if err != nil {
				return fmt.Errorf("init cipher with new key: %w", err)
			}
			newStore := managementstate.NewStore(resolvedState, newCipher)
			encryptedBytes, err := newStore.Marshal(snapshot)
			if err != nil {
				return fmt.Errorf("encrypt state snapshot: %w", err)
			}

			// Capture ownership/permissions of existing files so the rotated files
			// keep the same access rights (e.g. root:veil 640).
			statePerm := captureFilePermissions(resolvedState)
			keyPerm := captureFilePermissions(targetKeyPath)

			// Write new state to temporary file
			tempStatePath := resolvedState + ".tmp"
			if err := os.WriteFile(tempStatePath, encryptedBytes, 0o600); err != nil {
				return fmt.Errorf("write temporary state file: %w", err)
			}

			// Write new key to temporary file
			tempKeyPath := targetKeyPath + ".tmp"
			if err := os.WriteFile(tempKeyPath, newKey[:], 0o600); err != nil {
				os.Remove(tempStatePath)
				return fmt.Errorf("write temporary key file: %w", err)
			}

			// Rename the old key to a backup name (e.g., key.bak) if targetKeyPath exists
			backupKeyPath := targetKeyPath + ".bak"
			hasBackup := false
			if _, statErr := os.Stat(targetKeyPath); statErr == nil {
				if err := os.Rename(targetKeyPath, backupKeyPath); err != nil {
					os.Remove(tempStatePath)
					os.Remove(tempKeyPath)
					return fmt.Errorf("backup old key file: %w", err)
				}
				hasBackup = true
			}

			// Rename the new key to the target name
			if err := os.Rename(tempKeyPath, targetKeyPath); err != nil {
				if hasBackup {
					_ = os.Rename(backupKeyPath, targetKeyPath)
				}
				os.Remove(tempStatePath)
				os.Remove(tempKeyPath)
				return fmt.Errorf("rename key file: %w", err)
			}

			// Rename the state file
			if err := os.Rename(tempStatePath, resolvedState); err != nil {
				if hasBackup {
					if rbErr := os.Rename(backupKeyPath, targetKeyPath); rbErr != nil {
						return fmt.Errorf("critical failure: state rename failed (%v) and key rollback failed: %w", err, rbErr)
					}
				} else {
					os.Remove(targetKeyPath)
				}
				os.Remove(tempStatePath)
				return fmt.Errorf("rename state file (rolled back key): %w", err)
			}

			// Delete backup files on success
			if hasBackup {
				_ = os.Remove(backupKeyPath)
			}

			// Restore ownership/permissions on the rotated files.
			statePerm.apply(resolvedState)
			keyPerm.apply(targetKeyPath)

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

// filePermissions captures ownership and mode of an existing file so that a
// replacement file can inherit them. Missing files are represented by a zero
// value that does nothing when applied.
type filePermissions struct {
	uid  int
	gid  int
	mode os.FileMode
	ok   bool
}

func captureFilePermissions(path string) filePermissions {
	fi, err := os.Stat(path)
	if err != nil {
		return filePermissions{}
	}
	p := filePermissions{mode: fi.Mode().Perm(), ok: true}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		p.uid = int(st.Uid)
		p.gid = int(st.Gid)
	}
	return p
}

func (p filePermissions) apply(path string) {
	if !p.ok {
		return
	}
	_ = os.Chmod(path, p.mode)
	if p.uid >= 0 && p.gid >= 0 {
		_ = os.Chown(path, p.uid, p.gid)
	}
}
