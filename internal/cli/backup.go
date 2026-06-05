package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/backup"
	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newBackupCommand(version string) *cobra.Command {
	var statePath string
	var keyPath string
	var outputPath string
	var passphrase string
	var passphraseFile string
	var yes bool
	var checkOnly bool

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Manage encrypted and unencrypted state backups",
	}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a backup of the Veil panel state and encryption key",
		RunE: func(cmd *cobra.Command, args []string) error {
			env := serveflow.NewEnvironment()
			resolvedState, _ := env.StatePath(statePath)
			resolvedKey, _ := env.KeyPath(keyPath)

			// Resolve passphrase
			resolvedPass, err := resolvePassphrase(passphrase, passphraseFile)
			if err != nil {
				return err
			}

			if resolvedPass == "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "WARNING: Creating an unencrypted backup. This contains your sensitive state data and encryption keys in plaintext. Use -p or --passphrase to encrypt the backup.")
			}

			backupData, err := backup.CreateBackupWithOptions(resolvedState, resolvedKey, resolvedPass, backup.ArchiveOptions{
				VeilVersion: version,
			})
			if err != nil {
				return fmt.Errorf("create backup failed: %w", err)
			}

			targetOutput := outputPath
			if targetOutput == "" {
				ts := time.Now().Format("20060102_150405")
				if resolvedPass != "" {
					targetOutput = fmt.Sprintf("veil_backup_%s.tar.gz.enc", ts)
				} else {
					targetOutput = fmt.Sprintf("veil_backup_%s.tar.gz", ts)
				}
			}

			if err := os.WriteFile(targetOutput, backupData, 0o600); err != nil {
				return fmt.Errorf("write backup file: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Backup successfully created: %s\n", targetOutput)
			return nil
		},
	}

	restoreCmd := &cobra.Command{
		Use:   "restore <backup-file>",
		Short: "Restore Veil panel state and encryption key from a backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			backupFile := args[0]

			env := serveflow.NewEnvironment()
			resolvedState, _ := env.StatePath(statePath)
			resolvedKey, _ := env.KeyPath(keyPath)

			// Resolve passphrase
			resolvedPass, err := resolvePassphrase(passphrase, passphraseFile)
			if err != nil {
				return err
			}

			// Read backup data
			backupData, err := os.ReadFile(backupFile)
			if err != nil {
				return fmt.Errorf("read backup file %s: %w", backupFile, err)
			}

			if !checkOnly && !yes {
				stat, err := os.Stdin.Stat()
				if err != nil || (stat.Mode()&os.ModeCharDevice) == 0 {
					return errors.New("stdin is not a terminal; use --yes to bypass confirmation")
				}
				fmt.Fprintf(cmd.OutOrStdout(), "WARNING: Restoring a backup will overwrite the current management state and encryption key.\n")
				fmt.Fprintf(cmd.OutOrStdout(), "Are you sure you want to continue? [y/N]: ")
				reader := bufio.NewReader(os.Stdin)
				text, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("failed to read confirmation: %w", err)
				}
				text = strings.TrimSpace(strings.ToLower(text))
				if text != "y" && text != "yes" {
					fmt.Fprintln(cmd.OutOrStdout(), "Restore operation cancelled.")
					return nil
				}
			}

			result, err := backup.RestoreBackupWithOptions(
				backupData,
				resolvedState,
				resolvedKey,
				resolvedPass,
				backup.RestoreOptions{CheckOnly: checkOnly},
			)
			if err != nil {
				return fmt.Errorf("restore backup failed: %w", err)
			}

			if checkOnly {
				fmt.Fprintln(cmd.OutOrStdout(), "Restore check passed; no files were changed.")
				printBackupVerification(cmd, result.Verification)
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Backup successfully restored.")
			if result.SafetyStatePath != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Previous state preserved at: %s\n", result.SafetyStatePath)
			}
			if result.SafetyKeyPath != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Previous key preserved at: %s\n", result.SafetyKeyPath)
			}
			return nil
		},
	}

	verifyCmd := &cobra.Command{
		Use:   "verify <backup-file>",
		Short: "Decrypt and verify a backup without writing state",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedPass, err := resolvePassphrase(passphrase, passphraseFile)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("read backup file %s: %w", args[0], err)
			}
			report, err := backup.VerifyBackup(data, resolvedPass)
			if err != nil {
				return fmt.Errorf("verify backup failed: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Backup verified.")
			printBackupVerification(cmd, report)
			return nil
		},
	}

	for _, subCmd := range []*cobra.Command{createCmd, restoreCmd, verifyCmd} {
		subCmd.Flags().StringVar(&statePath, "state", "", "management state JSON path; defaults to VEIL_STATE_PATH or /var/lib/veil/state.json")
		subCmd.Flags().StringVar(&keyPath, "key-path", "", "encryption key file path; defaults to VEIL_KEY_PATH or /etc/veil/state.key")
		subCmd.Flags().StringVarP(&passphrase, "passphrase", "p", "", "encryption/decryption passphrase")
		subCmd.Flags().StringVar(&passphraseFile, "passphrase-file", "", "file containing the encryption/decryption passphrase")
		cmd.AddCommand(subCmd)
	}

	createCmd.Flags().StringVarP(&outputPath, "output", "o", "", "output backup file path (defaults to veil_backup_YYYYMMDD_HHMMSS.tar.gz[.enc])")
	restoreCmd.Flags().BoolVarP(&yes, "yes", "y", false, "confirm restore operation without prompting")
	restoreCmd.Flags().BoolVar(&checkOnly, "check-only", false, "verify compatibility without writing state or key files")

	return cmd
}

func printBackupVerification(cmd *cobra.Command, report backup.VerificationReport) {
	format := fmt.Sprintf("%d", report.FormatVersion)
	if report.Legacy {
		format = "legacy"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Archive format: %s\n", format)
	fmt.Fprintf(cmd.OutOrStdout(), "Encrypted: %t\n", report.Encrypted)
	if report.VeilVersion != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Veil version: %s\n", report.VeilVersion)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "State schema: %d\n", report.StateSchemaVersion)
	for _, file := range report.Files {
		fmt.Fprintf(cmd.OutOrStdout(), "- %s: %d bytes sha256:%s\n", file.Name, file.Size, file.SHA256)
	}
}

func resolvePassphrase(pass, file string) (string, error) {
	if pass != "" && file != "" {
		return "", errors.New("--passphrase and --passphrase-file are mutually exclusive")
	}
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read passphrase file: %w", err)
		}
		// Trim standard newlines and whitespace
		return strings.TrimRight(string(data), "\r\n"), nil
	}
	if pass == "" && term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Enter passphrase: ")
		pwd, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return "", fmt.Errorf("read passphrase interactively: %w", err)
		}
		fmt.Fprintln(os.Stderr)
		return string(pwd), nil
	}
	return pass, nil
}
