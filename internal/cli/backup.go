package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/backup"
	serveflow "github.com/mikkelchokolate/Veil/internal/cliflow/serve"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var backupSystemctlRun = func(args ...string) error {
	return exec.Command("systemctl", args...).Run()
}

func newBackupCommand(version string) *cobra.Command {
	var statePath string
	var keyPath string
	var outputPath string
	var outputDir string
	var backupDir string
	var passphrase string
	var passphraseFile string
	var yes bool
	var checkOnly bool
	var allowUnencrypted bool
	var pruneAfterCreate bool
	var dryRun bool
	var schedulePassphrasePath string
	var removeSchedulePassphrase bool
	defaultRetention := backup.DefaultRetentionPolicy()
	var daily = defaultRetention.Daily
	var weekly = defaultRetention.Weekly
	var monthly = defaultRetention.Monthly

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

			if resolvedPass == "" && !allowUnencrypted {
				return errors.New("backup encryption is required; provide --passphrase/--passphrase-file or explicitly use --allow-unencrypted")
			}

			targetOutput := outputPath
			if targetOutput != "" && outputDir != "" {
				return errors.New("--output and --output-dir are mutually exclusive")
			}
			if targetOutput == "" {
				ts := time.Now().UTC().Format("20060102_150405")
				filename := ""
				if resolvedPass != "" {
					filename = fmt.Sprintf("veil_backup_%s.tar.gz.enc", ts)
				} else {
					filename = fmt.Sprintf("veil_backup_%s.tar.gz", ts)
				}
				if outputDir != "" {
					targetOutput = filepath.Join(outputDir, filename)
				} else {
					targetOutput = filename
				}
			}

			maxBytes, err := backup.ConfiguredMaxBackupBytes()
			if err != nil {
				return err
			}
			if err := backup.CreateBackupFileWithOptions(targetOutput, resolvedState, resolvedKey, resolvedPass, backup.ArchiveOptions{
				VeilVersion: version,
				MaxBytes:    maxBytes,
			}); err != nil {
				return fmt.Errorf("create backup failed: %w", err)
			}
			if _, err := backup.VerifyBackupFile(targetOutput, resolvedPass, maxBytes); err != nil {
				_ = os.Remove(targetOutput)
				return fmt.Errorf("verify generated backup failed: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Backup successfully created: %s\n", targetOutput)
			if pruneAfterCreate {
				dir := outputDir
				if dir == "" {
					dir = filepath.Dir(targetOutput)
				}
				result, err := backup.PruneArchives(dir, backup.RetentionPolicy{
					Daily: daily, Weekly: weekly, Monthly: monthly,
				}, false)
				if err != nil {
					return fmt.Errorf("prune backups after create: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Retention: kept %d, deleted %d\n", len(result.Kept), len(result.Deleted))
			}
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

			maxBytes, err := backup.ConfiguredMaxBackupBytes()
			if err != nil {
				return err
			}
			result, err := backup.RestoreBackupFileWithOptions(
				backupFile,
				resolvedState,
				resolvedKey,
				resolvedPass,
				backup.RestoreOptions{CheckOnly: checkOnly, MaxBytes: maxBytes},
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
			maxBytes, err := backup.ConfiguredMaxBackupBytes()
			if err != nil {
				return err
			}
			report, err := backup.VerifyBackupFile(args[0], resolvedPass, maxBytes)
			if err != nil {
				return fmt.Errorf("verify backup failed: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Backup verified.")
			printBackupVerification(cmd, report)
			return nil
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List managed backup archives",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := backup.ListArchives(backupDir)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No backup archives found.")
				return nil
			}
			for _, entry := range entries {
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"%s\t%s\t%d bytes\tencrypted=%t\n",
					entry.CreatedAt.Format(time.RFC3339),
					entry.Name,
					entry.Size,
					entry.Encrypted,
				)
			}
			return nil
		},
	}

	pruneCmd := &cobra.Command{
		Use:   "prune",
		Short: "Apply daily, weekly, and monthly backup retention",
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := backup.PruneArchives(backupDir, backup.RetentionPolicy{
				Daily: daily, Weekly: weekly, Monthly: monthly,
			}, dryRun)
			if err != nil {
				return err
			}
			for _, name := range result.Deleted {
				action := "Deleted"
				if dryRun {
					action = "Would delete"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", action, name)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Retention: kept %d, deleted %d, dry-run=%t\n", len(result.Kept), len(result.Deleted), dryRun)
			return nil
		},
	}

	scheduleCmd := &cobra.Command{
		Use:   "schedule",
		Short: "Manage the encrypted systemd backup timer",
	}
	scheduleEnableCmd := &cobra.Command{
		Use:   "enable",
		Short: "Store the backup passphrase and enable the daily timer",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedPass, err := resolvePassphrase(passphrase, passphraseFile)
			if err != nil {
				return err
			}
			if len(resolvedPass) < 16 {
				return errors.New("scheduled backup passphrase must be at least 16 characters")
			}
			if err := writeBackupArchive(schedulePassphrasePath, []byte(resolvedPass+"\n")); err != nil {
				return fmt.Errorf("write scheduled backup passphrase: %w", err)
			}
			if err := backupSystemctlRun("daemon-reload"); err != nil {
				return fmt.Errorf("systemctl daemon-reload: %w", err)
			}
			if err := backupSystemctlRun("enable", "--now", "veil-backup.timer"); err != nil {
				return fmt.Errorf("enable veil-backup.timer: %w", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Encrypted backup schedule enabled; passphrase stored at %s\n", schedulePassphrasePath)
			return nil
		},
	}
	scheduleDisableCmd := &cobra.Command{
		Use:   "disable",
		Short: "Disable the daily backup timer",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := backupSystemctlRun("disable", "--now", "veil-backup.timer"); err != nil {
				return fmt.Errorf("disable veil-backup.timer: %w", err)
			}
			if removeSchedulePassphrase {
				if err := os.Remove(schedulePassphrasePath); err != nil && !errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("remove scheduled backup passphrase: %w", err)
				}
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Encrypted backup schedule disabled.")
			return nil
		},
	}
	scheduleEnableCmd.Flags().StringVarP(&passphrase, "passphrase", "p", "", "backup encryption passphrase")
	scheduleEnableCmd.Flags().StringVar(&passphraseFile, "passphrase-file", "", "file containing the backup encryption passphrase")
	scheduleEnableCmd.Flags().StringVar(&schedulePassphrasePath, "passphrase-path", "/etc/veil/backup.passphrase", "root-owned passphrase destination used by the systemd service")
	scheduleDisableCmd.Flags().StringVar(&schedulePassphrasePath, "passphrase-path", "/etc/veil/backup.passphrase", "scheduled backup passphrase path")
	scheduleDisableCmd.Flags().BoolVar(&removeSchedulePassphrase, "remove-passphrase", false, "remove the stored passphrase after disabling the timer")
	scheduleCmd.AddCommand(scheduleEnableCmd, scheduleDisableCmd)
	cmd.AddCommand(scheduleCmd)

	for _, subCmd := range []*cobra.Command{createCmd, restoreCmd, verifyCmd} {
		subCmd.Flags().StringVar(&statePath, "state", "", "management state JSON path; defaults to VEIL_STATE_PATH or /var/lib/veil/state.json")
		subCmd.Flags().StringVar(&keyPath, "key-path", "", "encryption key file path; defaults to VEIL_KEY_PATH or /etc/veil/state.key")
		subCmd.Flags().StringVarP(&passphrase, "passphrase", "p", "", "encryption/decryption passphrase")
		subCmd.Flags().StringVar(&passphraseFile, "passphrase-file", "", "file containing the encryption/decryption passphrase")
		cmd.AddCommand(subCmd)
	}

	createCmd.Flags().StringVarP(&outputPath, "output", "o", "", "output backup file path (defaults to veil_backup_YYYYMMDD_HHMMSS.tar.gz[.enc])")
	createCmd.Flags().StringVar(&outputDir, "output-dir", "", "directory for timestamped backup archives")
	createCmd.Flags().BoolVar(&allowUnencrypted, "allow-unencrypted", false, "explicitly allow a plaintext archive containing state and key material")
	createCmd.Flags().BoolVar(&pruneAfterCreate, "prune", false, "apply retention after a successful verified backup")
	addRetentionFlags(createCmd, &daily, &weekly, &monthly)
	restoreCmd.Flags().BoolVarP(&yes, "yes", "y", false, "confirm restore operation without prompting")
	restoreCmd.Flags().BoolVar(&checkOnly, "check-only", false, "verify compatibility without writing state or key files")
	for _, subCmd := range []*cobra.Command{listCmd, pruneCmd} {
		subCmd.Flags().StringVar(&backupDir, "dir", "/var/lib/veil/backups", "managed backup archive directory")
		cmd.AddCommand(subCmd)
	}
	addRetentionFlags(pruneCmd, &daily, &weekly, &monthly)
	pruneCmd.Flags().BoolVar(&dryRun, "dry-run", false, "show deletions without removing archives")

	return cmd
}

func addRetentionFlags(cmd *cobra.Command, daily, weekly, monthly *int) {
	cmd.Flags().IntVar(daily, "daily", 7, "number of latest UTC days to retain")
	cmd.Flags().IntVar(weekly, "weekly", 4, "number of latest ISO weeks to retain")
	cmd.Flags().IntVar(monthly, "monthly", 12, "number of latest UTC months to retain")
}

func writeBackupArchive(path string, body []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".veil-backup-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	if _, err := temp.Write(body); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	backupPath := path + ".replace-backup"
	hadExisting := false
	if _, err := os.Stat(path); err == nil {
		if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = os.Remove(tempPath)
			return err
		}
		if err := os.Rename(path, backupPath); err != nil {
			_ = os.Remove(tempPath)
			return err
		}
		hadExisting = true
	}
	if err := os.Rename(tempPath, path); err != nil {
		if hadExisting {
			_ = os.Rename(backupPath, path)
		}
		_ = os.Remove(tempPath)
		return err
	}
	if hadExisting {
		_ = os.Remove(backupPath)
	}
	return nil
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
	fmt.Fprintf(cmd.OutOrStdout(), "Desired revision: %d\n", report.DesiredRevision)
	for _, file := range report.Files {
		fmt.Fprintf(cmd.OutOrStdout(), "- %s: %d bytes sha256:%s\n", file.Name, file.Size, file.SHA256)
	}
}

func resolvePassphrase(pass, file string) (string, error) {
	if pass != "" && file != "" {
		return "", errors.New("--passphrase and --passphrase-file are mutually exclusive")
	}
	if file != "" {
		data, err := readBackupPassphraseFile(file)
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

func readBackupPassphraseFile(path string) ([]byte, error) {
	const maxPassphraseBytes int64 = 64 * 1024
	file, err := openCLIRegularNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("passphrase file is not a regular file")
	}
	body, err := io.ReadAll(io.LimitReader(file, maxPassphraseBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxPassphraseBytes {
		return nil, errors.New("passphrase file exceeds 65536-byte limit")
	}
	return body, nil
}
