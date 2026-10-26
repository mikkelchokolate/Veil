package cli

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/api"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/secrets"
	"github.com/mikkelchokolate/Veil/internal/statecommit"
	"github.com/mikkelchokolate/Veil/internal/syspath"
	"github.com/mikkelchokolate/Veil/internal/veilenv"
	"github.com/spf13/cobra"
)

var adminNotifyPanel = notifyRunningPanel

var adminSystemctlRun = func(args ...string) error {
	return syspath.Command("systemctl", args...).Run()
}

// errPanelNotActive reports that veil.service is not active, so no running
// Panel was signalled. The on-disk revocation still takes effect when a Panel
// next starts; a Panel running outside systemd must be signalled by hand.
var errPanelNotActive = errors.New("veil.service is not active")

func notifyRunningPanel() error {
	if err := adminSystemctlRun("is-active", "--quiet", "veil.service"); err != nil {
		return errPanelNotActive
	}
	if err := adminSystemctlRun("kill", "-s", "HUP", "veil.service"); err != nil {
		return fmt.Errorf("veil.service is running but could not be reloaded; stop the unit first: %w", err)
	}
	return nil
}

var adminRestartPanel = restartRunningPanel

// restartRunningPanel restarts veil.service instead of signalling it. The
// Panel resolves VEIL_KEY_PATH from its process environment at startup and
// only SIGHUP-reloads the cipher for the path it already holds, so a key
// rotation that repoints the environment must restart the unit for systemd to
// re-read EnvironmentFile= (#1453 review).
func restartRunningPanel() error {
	if err := adminSystemctlRun("is-active", "--quiet", "veil.service"); err != nil {
		return errPanelNotActive
	}
	if err := adminSystemctlRun("restart", "veil.service"); err != nil {
		return fmt.Errorf("veil.service is running but could not be restarted to load the new VEIL_KEY_PATH: %w", err)
	}
	return nil
}

// rotatePreflightSystemdDir is where rotate-key looks for stale pre-#1472
// drop-ins. Tests replace it.
var rotatePreflightSystemdDir = veilenv.DefaultSystemdDir

var adminRestartHelper = restartHelperUnit

// restartHelperUnit makes veil-helper.service re-read veil.env. The helper
// resolves VEIL_KEY_PATH from its process environment and uses it for
// panel-invoked backups, restores and helper-side key rotation, so a rotation
// that repoints the key must restart it too or those paths keep the old key
// (#1471). The unit is socket-activated: when it is not running there is
// nothing to restart, because the next request starts it with the new
// environment.
func restartHelperUnit() error {
	if err := adminSystemctlRun("is-active", "--quiet", "veil-helper.service"); err != nil {
		return nil
	}
	if err := adminSystemctlRun("restart", "veil-helper.service"); err != nil {
		return fmt.Errorf("veil-helper.service could not be restarted to load the new VEIL_KEY_PATH; restart it by hand: %w", err)
	}
	return nil
}

func revokePersistedUserSessions(statePath, username string) error {
	return api.RevokeUsernamePersistedSessions(statePath, username)
}

// afterAdminCredentialWrite revokes the affected Panel sessions on disk and
// signals the running Panel, whose SIGHUP reload re-reads the session store
// (#1265). The revocation is journaled as a positional intent, so it holds
// even if the Panel writes a session record before it handles the signal.
// prepareRotatedKeyEnv stages the environment rewrite that keeps the installed
// Panel on the rotated key. It returns nil when the rotation is in place: the
// environment file already names the live key path, so there is nothing to
// repoint.
func prepareRotatedKeyEnv(liveKeyPath, targetKeyPath string) (*veilenv.PreparedUpdate, error) {
	if sameKeyPath(liveKeyPath, targetKeyPath) {
		return nil, nil
	}
	envPath := veilenv.Resolve(veilenv.DefaultSystemdDir)
	update, err := veilenv.PrepareKeyPath(envPath, targetKeyPath)
	if err != nil {
		return nil, fmt.Errorf("refusing to rotate the state key: the Panel would keep reading %s; rotate in place (omit --new-key-path) or point VEIL_KEY_PATH at the new key first: %w", liveKeyPath, err)
	}
	return update, nil
}

func sameKeyPath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)
	if errA == nil && errB == nil && absA == absB {
		return true
	}
	// The live key may be reached through a symlink (a user-supplied
	// --key-path is not canonicalized); a rotation to the target it points at
	// is still an in-place rotation.
	realA, errA := filepath.EvalSymlinks(a)
	realB, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && realA == realB
}

func afterAdminCredentialWrite(statePath string, usernames []string, revokeAll bool, restartPanel bool, warn io.Writer) error {
	if revokeAll {
		if err := api.RevokeAllPersistedSessions(statePath); err != nil {
			return fmt.Errorf("revoke Panel sessions: %w", err)
		}
	} else {
		for _, username := range usernames {
			if username == "" {
				continue
			}
			if err := revokePersistedUserSessions(statePath, username); err != nil {
				return err
			}
		}
	}
	notify := adminNotifyPanel
	if restartPanel {
		notify = adminRestartPanel
	}
	err := notify()
	if errors.Is(err, errPanelNotActive) {
		if warn != nil {
			fmt.Fprintln(warn, "warning: veil.service is not active, so no running Panel was reloaded. "+
				"The change is recorded on disk and applies when a Panel starts or receives SIGHUP. "+
				"A Panel running outside systemd keeps serving the old state until then: "+
				"send it SIGHUP or restart it now.")
		}
		return nil
	}
	return err
}

func administratorCount(users []model.User) int {
	count := 0
	for _, user := range users {
		if user.Role == "admin" {
			count++
		}
	}
	return count
}

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
			resolvedState, resolvedKey, _, _ := resolveInstalledPaths(statePath, keyPath)

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

			if err := afterAdminCredentialWrite(resolvedState, nil, true, false, cmd.ErrOrStderr()); err != nil {
				return fmt.Errorf("credentials were reset but the running Panel was not updated: %w", err)
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
	var customPasswordStdin bool
	var customPasswordFile string
	var clearSecondFactors bool

	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Set or update custom administrative credentials",
		Long: "Set or update a Panel user. The password is read from --password-stdin,\n" +
			"--password-file, or an interactive no-echo prompt when standard input is a\n" +
			"terminal. --password still works but exposes the password in the process list.",
		RunE: func(cmd *cobra.Command, args []string) error {
			password, err := resolveAdminPassword(adminPasswordSource{
				argv:     customPassword,
				argvSet:  cmd.Flags().Changed("password"),
				useStdin: customPasswordStdin,
				file:     customPasswordFile,
			}, cmd.InOrStdin(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			if err := api.ValidatePanelPassword(password); err != nil {
				return err
			}
			roleFlagSet := cmd.Flags().Changed("role")
			targetRole := customRole
			if targetRole == "" {
				targetRole = "admin"
			}
			if targetRole != "admin" && targetRole != "viewer" {
				return fmt.Errorf("role must be admin or viewer")
			}

			resolvedState, resolvedKey, _, _ := resolveInstalledPaths(statePath, keyPath)

			hashed, err := hasher.Hash([]byte(password))
			if err != nil {
				return fmt.Errorf("hash password: %w", err)
			}

			username := customUsername
			secondFactorReport := ""
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
				existingIndex := -1
				for index := range snapshot.Users {
					if snapshot.Users[index].Username == username {
						existingIndex = index
						break
					}
				}
				// A NEW account must satisfy the same username policy as
				// first-run setup and POST /api/users (#1266); otherwise the
				// CLI could mint names like "me" (shadowed by the
				// /api/v1/users/me/* self-service routes) or "a/b" that no API
				// route can address. Updating an EXISTING account is left
				// unrestricted so a legacy name stays recoverable here.
				if existingIndex < 0 {
					if clearSecondFactors {
						// --clear-2fa is lost-authenticator recovery for an
						// account that exists. A mistyped --username must
						// not mint a fresh account and look like success
						// for the intended user (#1447).
						return fmt.Errorf("%w: user %q does not exist", errClear2FANewUser, username)
					}
					if err := api.ValidatePanelUsername(username); err != nil {
						return fmt.Errorf("refusing to create user %q: %w", username, err)
					}
				}
				role := targetRole
				locale := ""
				if existingIndex >= 0 {
					existing := snapshot.Users[existingIndex]
					if !roleFlagSet {
						role = existing.Role
					}
					locale = existing.Locale
				}
				if role != "admin" && existingIndex >= 0 && snapshot.Users[existingIndex].Role == "admin" && administratorCount(snapshot.Users) <= 1 {
					return fmt.Errorf("%w; create another administrator first or use `veil admin reset`", managementstate.ErrLastAdministrator)
				}
				updatedUser := model.User{
					Username:     username,
					PasswordHash: string(hashed),
					Role:         role,
					Locale:       locale,
				}
				if existingIndex >= 0 {
					// Updating an existing account changes only the password
					// (and the role when --role is passed). Every
					// second-factor field and the TOTP replay watermark are
					// carried over through the same helpers UpdateUser uses,
					// so a password rotation never silently downgrades the
					// account to password-only login (#1447).
					existing := snapshot.Users[existingIndex]
					updatedUser.PreserveTOTP(existing)
					updatedUser.PreservePasskeys(existing)
					factors := describeSecondFactors(existing)
					if clearSecondFactors {
						// Explicit lost-authenticator recovery: the same
						// teardown as the admin 2FA reset API. ClearTOTP
						// keeps TOTPLastStep, so replay protection is never
						// rewound (#1220).
						updatedUser.ClearTOTP()
						updatedUser.ClearPasskeys()
						if factors == "" {
							secondFactorReport = "none to clear"
						} else {
							secondFactorReport = "cleared (" + factors + "); the user signs in with the password alone until they enroll again"
						}
					} else if factors != "" {
						secondFactorReport = "kept (" + factors + "); pass --clear-2fa to remove them"
					}
					snapshot.Users[existingIndex] = updatedUser
				} else {
					snapshot.Users = append(snapshot.Users, updatedUser)
				}
				targetRole = role
				managementstate.CompleteSetupForAdmins(snapshot, time.Now())
				return nil
			}); err != nil {
				if errors.Is(err, api.ErrPanelUsernameInvalid) || errors.Is(err, errClear2FANewUser) {
					// Nothing was saved: report the policy refusal as-is
					// rather than as a "save state" failure.
					return err
				}
				return fmt.Errorf("save state: %w", err)
			}

			if err := afterAdminCredentialWrite(resolvedState, []string{username}, false, false, cmd.ErrOrStderr()); err != nil {
				return fmt.Errorf("credentials were saved but the running Panel was not updated: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "User credentials successfully set.\n")
			fmt.Fprintf(cmd.OutOrStdout(), "Username: %s\n", username)
			fmt.Fprintf(cmd.OutOrStdout(), "Role: %s\n", targetRole)
			if secondFactorReport != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Second factors: %s\n", secondFactorReport)
			}
			return nil
		},
	}

	setCmd.Flags().StringVar(&customUsername, "username", "", "username to set or update; defaults to the first administrator in state")
	setCmd.Flags().StringVar(&customPassword, "password", "", "password to set; deprecated: visible to every local user via the process list, prefer --password-stdin, --password-file or the interactive prompt")
	setCmd.Flags().BoolVar(&customPasswordStdin, "password-stdin", false, "read the password from standard input (one line)")
	setCmd.Flags().StringVar(&customPasswordFile, "password-file", "", "read the password from a regular file (one line; symlinks are refused)")
	setCmd.Flags().StringVar(&customRole, "role", "admin", "role of the user (admin or viewer)")
	setCmd.Flags().BoolVar(&clearSecondFactors, "clear-2fa", false, "also remove the existing user's TOTP factor, recovery codes and passkeys (lost-authenticator recovery); without it they are kept")

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show all registered users and roles",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedState, resolvedKey, _, _ := resolveInstalledPaths(statePath, keyPath)

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
			// Same installed-layout resolution as the rest of the root CLI:
			// after a path-moving rotation the state is encrypted with the key
			// veil.env names, so the next --new-key-path call (and an in-place
			// rotation) has to read from there, not from the packaged default
			// (#1477).
			resolvedState, resolvedKey, liveKeyPath, envPath := resolveInstalledPaths(statePath, keyPath)

			targetKeyPath := newKeyPath
			if targetKeyPath == "" {
				targetKeyPath = resolvedKey
			}
			if abs, err := filepath.Abs(targetKeyPath); err == nil {
				targetKeyPath = abs
			}

			// Pre-#1472 drop-ins pin the key path in the unit itself and would
			// ignore an environment rewrite, leaving the helper and the backup
			// timer on the pre-rotation key (#1473). A pin on exactly the file
			// this rotation rewrites cannot do that: the unit reads that path,
			// so an in-place rewrite replaces the key underneath it and the
			// next operation loads the new one. Refusing those blocked the
			// emergency re-key on the very hosts that need it (#1483).
			if pins := staleKeyPins(rotatePreflightSystemdDir); len(pins) > 0 {
				var blocking []string
				for _, pin := range pins {
					// An unparsable pin stays conservative: without the value
					// there is no way to prove the rotation picks it up.
					if pin.Pinned != "" && sameKeyPath(pin.Pinned, targetKeyPath) {
						continue
					}
					blocking = append(blocking, pin.DropIn)
				}
				if len(blocking) > 0 {
					return fmt.Errorf("refusing to rotate the state key: %s still pin the key path instead of reading veil.env, so the helper and the backup timer would keep the old key; run `veil repair` (with --etc-dir/--var-dir on a custom install) to re-render them, then retry", strings.Join(blocking, ", "))
				}
			}

			// The Panel, the privileged helper and the backup unit load their
			// key from VEIL_KEY_PATH in the installed environment file, so two
			// invariants have to hold: the managed state must not be left
			// encrypted under a key the units do not read, and the live key
			// must not be overwritten for the sake of a foreign state tree.
			//
			// The decision follows the layout, not the requested key: in the
			// managed layout the environment follows the rotation whenever the
			// target is not the live key path (a --new-key-path move, or a
			// --key-path that names a copy of the live key), and the rewrite is
			// staged before anything is rotated so a failure leaves state and
			// Panel in agreement (#1453, #1477). A rotation outside the managed
			// layout that would overwrite the live key is refused instead.
			var pending *veilenv.PreparedUpdate
			var err error
			_, envFileErr := os.Stat(envPath)
			managedState := envOwnsRestoreLayout(envPath, resolvedState)
			switch {
			case managedState:
				if !sameKeyPath(targetKeyPath, liveKeyPath) {
					// prepareRotatedKeyEnv compares the live path with the
					// target, so it must get the path the units read, not the
					// source of this rotation.
					pending, err = prepareRotatedKeyEnv(liveKeyPath, targetKeyPath)
				}
			case envNamesKey(envPath, resolvedKey):
				// The key is the one the units read, but the state is not the
				// managed one: rotating here would overwrite the live key for a
				// foreign tree and break the Panel.
				return fmt.Errorf("refusing to rotate: --state %s is outside the installed layout, but the rotation would overwrite the live key %s the Panel reads; rerun with the installed --state, or pass both --state and --key-path for the tree you mean", resolvedState, liveKeyPath)
			case envFileErr != nil && !sameKeyPath(targetKeyPath, resolvedKey):
				// No installed environment to repoint: moving the key would
				// leave whoever starts with the packaged default unable to
				// decrypt the state (#1453).
				return fmt.Errorf("refusing to rotate the state key: %s does not exist, so the key path cannot be repointed; rotate in place (omit --new-key-path) or create the environment file first", envPath)
			}
			if err != nil {
				return err
			}
			if pending != nil {
				defer pending.Abort()
			}

			if _, err := statecommit.RotateKey(statecommit.RotateKeyOptions{
				StatePath:     resolvedState,
				KeyPath:       resolvedKey,
				TargetKeyPath: targetKeyPath,
			}); err != nil {
				return fmt.Errorf("rotate state key: %w", err)
			}
			if pending != nil {
				if err := pending.Commit(); err != nil {
					// The environment still points at the old key while state
					// holds the new one: say exactly what to do, and do not
					// reload the Panel into a broken key.
					return fmt.Errorf("state was re-encrypted with %s but the Panel environment could not be repointed: %w; set VEIL_KEY_PATH=%s in %s and restart the Panel",
						targetKeyPath, err, targetKeyPath, pending.Path())
				}
			}
			// The Panel goes first: it is the process that writes state, so it
			// must never keep the pre-rotation cipher after the environment
			// moved. The helper is second because it only reads state. Both are
			// attempted even when the first one fails — a failed helper restart
			// must not skip the Panel restart, and a failed Panel restart must
			// not leave the helper on the old key — and the errors are reported
			// together so the operator knows exactly what still needs a manual
			// restart (#1471 review).
			panelErr := afterAdminCredentialWrite(resolvedState, nil, true, pending != nil, cmd.ErrOrStderr())
			var helperErr error
			if pending != nil {
				helperErr = adminRestartHelper()
			}
			if panelErr != nil || helperErr != nil {
				return fmt.Errorf("state key was rotated but not every process was updated: %w", errors.Join(panelErr, helperErr))
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Key successfully rotated.\n")
			fmt.Fprintf(cmd.OutOrStdout(), "New key written to: %s\n", targetKeyPath)
			if pending != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "VEIL_KEY_PATH in %s now points at the new key.\n", pending.Path())
			}
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

// errClear2FANewUser refuses `admin set --clear-2fa` for a username that is
// not in state; nothing is saved.
var errClear2FANewUser = errors.New("--clear-2fa only applies to an existing user")

// describeSecondFactors names the second factors armed on user for the
// `admin set` report, or "" when there are none (#1447).
func describeSecondFactors(user model.User) string {
	var parts []string
	if user.TOTPEnabled {
		parts = append(parts, "TOTP")
	}
	if user.TOTPPendingSecret != "" {
		parts = append(parts, "pending TOTP enrollment")
	}
	if n := len(user.TOTPRecoveryHashes); n > 0 {
		parts = append(parts, fmt.Sprintf("%d recovery code(s)", n))
	}
	if n := len(user.Passkeys); n > 0 {
		parts = append(parts, fmt.Sprintf("%d passkey(s)", n))
	}
	return strings.Join(parts, ", ")
}
