package privileged

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/service"
)

type Executor struct {
	Promote       func(context.Context, ResolvedPromotion) (PromoteResult, error)
	ServiceAction func(context.Context, ServiceActionRequest) error
	ServiceStatus func(context.Context, ServiceStatusRequest) (ServiceStatusResult, error)
	Journal       func(context.Context, ResolvedJournal) (JournalResult, error)
	Backup        func(context.Context, ResolvedBackup) (BackupResult, error)
	RotateKey     func(context.Context, RotateKeyRequest) error
	Firewall      func(context.Context, ResolvedFirewall) (FirewallResult, error)
	Update        func(context.Context, ResolvedUpdate) (UpdateResult, error)
	RestartPanel  func(context.Context) error
}

type CommandRunner func(context.Context, []string, time.Duration) (string, error)

type ProductionConfig struct {
	PromotionBackupRoot  string
	StatePath            string
	KeyPath              string
	BackupPassphrasePath string
	BackupRoot           string
	VeilVersion          string
	FirewallCommands     map[string][]string
	RunCommand           CommandRunner
	BackupWorkflow       func(context.Context, ResolvedBackup) (BackupResult, error)
	UpdateWorkflow       func(context.Context, ResolvedUpdate) (UpdateResult, error)
	RotateKeyWorkflow    func(context.Context) error
	Now                  func() time.Time
}

func DefaultProductionConfig(policy Policy, version string) ProductionConfig {
	return ProductionConfig{
		PromotionBackupRoot:  filepath.Join(policy.StateRoot, "promotion-backups"),
		StatePath:            policy.StatePath,
		KeyPath:              policy.KeyPath,
		BackupPassphrasePath: policy.BackupPassphrasePath,
		BackupRoot:           policy.BackupRoot,
		VeilVersion:          version,
	}
}

func NewProductionExecutor(config ProductionConfig) Executor {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.RunCommand == nil {
		config.RunCommand = runProductionCommand
	}
	if config.BackupWorkflow == nil {
		config.BackupWorkflow = func(ctx context.Context, request ResolvedBackup) (BackupResult, error) {
			return runProductionBackup(ctx, config, request)
		}
	}
	if config.UpdateWorkflow == nil {
		config.UpdateWorkflow = func(_ context.Context, request ResolvedUpdate) (UpdateResult, error) {
			info, err := os.Stat(request.Path)
			if err != nil {
				return UpdateResult{}, err
			}
			if !info.Mode().IsRegular() {
				return UpdateResult{}, errors.New("update artifact is not a regular file")
			}
			return UpdateResult{ArtifactID: request.ArtifactID, Staged: true}, nil
		}
	}
	if config.RotateKeyWorkflow == nil {
		config.RotateKeyWorkflow = func(context.Context) error {
			return rotateStateKey(config.StatePath, config.KeyPath, config.Now)
		}
	}
	return Executor{
		Promote: func(_ context.Context, request ResolvedPromotion) (PromoteResult, error) {
			return promoteResolvedArtifacts(config.PromotionBackupRoot, config.Now, request)
		},
		ServiceAction: func(ctx context.Context, request ServiceActionRequest) error {
			_, err := config.RunCommand(ctx, []string{"systemctl", string(request.Action), request.Unit}, 30*time.Second)
			return err
		},
		ServiceStatus: func(ctx context.Context, request ServiceStatusRequest) (ServiceStatusResult, error) {
			result := ServiceStatusResult{Services: make([]ServiceStatus, 0, len(request.Units))}
			for _, unit := range request.Units {
				command := service.NewSystemdServiceStatusCommand(unit)
				output, err := config.RunCommand(ctx, append([]string{command.Name()}, command.Args()...), command.Timeout())
				if err != nil {
					return ServiceStatusResult{}, err
				}
				status := service.NewSystemdServiceStatusParser().Parse(unit, output)
				result.Services = append(result.Services, ServiceStatus{
					Unit:        status.Unit,
					LoadState:   status.LoadState,
					ActiveState: status.ActiveState,
					SubState:    status.SubState,
				})
			}
			return result, nil
		},
		Journal: func(ctx context.Context, request ResolvedJournal) (JournalResult, error) {
			output, err := config.RunCommand(ctx, []string{
				"journalctl", "-u", request.Unit, "--no-pager", "-n", strconv.Itoa(request.Lines), "-o", "short-iso",
			}, 10*time.Second)
			if err != nil {
				return JournalResult{}, err
			}
			lines := []string{}
			for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
				if line != "" {
					lines = append(lines, line)
				}
			}
			return JournalResult{Unit: request.Unit, Lines: lines}, nil
		},
		Backup: config.BackupWorkflow,
		RotateKey: func(ctx context.Context, _ RotateKeyRequest) error {
			return config.RotateKeyWorkflow(ctx)
		},
		Firewall: func(ctx context.Context, request ResolvedFirewall) (FirewallResult, error) {
			result := FirewallResult{}
			for _, id := range request.RuleIDs {
				command, ok := config.FirewallCommands[id]
				if !ok || len(command) == 0 {
					return FirewallResult{}, fmt.Errorf("firewall rule %q has no production command", id)
				}
				if _, err := config.RunCommand(ctx, append([]string(nil), command...), 30*time.Second); err != nil {
					return FirewallResult{}, err
				}
				result.AppliedRuleIDs = append(result.AppliedRuleIDs, id)
			}
			return result, nil
		},
		Update: config.UpdateWorkflow,
		RestartPanel: func(ctx context.Context) error {
			_, err := config.RunCommand(ctx, []string{"systemctl", "restart", "veil.service"}, 30*time.Second)
			return err
		},
	}
}

func runProductionCommand(ctx context.Context, command []string, timeout time.Duration) (string, error) {
	if len(command) == 0 {
		return "", errors.New("command is empty")
	}
	commandCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		commandCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()
	output, err := exec.CommandContext(commandCtx, command[0], command[1:]...).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message != "" {
			return "", fmt.Errorf("%s: %w", message, err)
		}
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func promoteResolvedArtifacts(backupRoot string, now func() time.Time, request ResolvedPromotion) (PromoteResult, error) {
	result := PromoteResult{}
	backupID := now().UTC().Format("20060102T150405.000000000Z")
	for _, artifact := range request.Artifacts {
		body, err := os.ReadFile(artifact.Source)
		if err != nil {
			return PromoteResult{}, err
		}
		if err := backupPromotionDestination(backupRoot, backupID, artifact); err != nil {
			return PromoteResult{}, err
		}
		if err := atomicfile.Write(artifact.Destination, body, 0o600, 0o700); err != nil {
			return PromoteResult{}, err
		}
		result.WrittenArtifacts = append(result.WrittenArtifacts, artifact.ID)
	}
	for _, artifact := range request.RemoveArtifacts {
		if err := backupPromotionDestination(backupRoot, backupID, artifact); err != nil {
			return PromoteResult{}, err
		}
		if err := os.Remove(artifact.Destination); err != nil && !errors.Is(err, os.ErrNotExist) {
			return PromoteResult{}, err
		}
		result.RemovedArtifacts = append(result.RemovedArtifacts, artifact.ID)
	}
	if len(result.WrittenArtifacts) > 0 || len(result.RemovedArtifacts) > 0 {
		result.BackupID = backupID
	}
	return result, nil
}

func backupPromotionDestination(root, backupID string, artifact ResolvedArtifact) error {
	body, err := os.ReadFile(artifact.Destination)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if root == "" {
		return errors.New("promotion backup root is required")
	}
	path := filepath.Join(root, backupID, filepath.Clean(artifact.ID))
	return atomicfile.Write(path, body, 0o600, 0o700)
}

func runProductionBackup(_ context.Context, config ProductionConfig, request ResolvedBackup) (BackupResult, error) {
	passphraseBody, err := os.ReadFile(request.BackupPassphrasePath)
	if err != nil {
		return BackupResult{}, fmt.Errorf("read backup passphrase: %w", err)
	}
	passphrase := strings.TrimRight(string(passphraseBody), "\r\n")
	if len(passphrase) < 16 {
		return BackupResult{}, errors.New("configured backup passphrase is too short")
	}
	switch request.Action {
	case BackupActionCreate:
		data, err := backup.CreateBackupWithOptions(request.StatePath, request.KeyPath, passphrase, backup.ArchiveOptions{
			VeilVersion: config.VeilVersion,
			CreatedAt:   config.Now().UTC(),
		})
		if err != nil {
			return BackupResult{}, err
		}
		if _, err := backup.VerifyBackup(data, passphrase); err != nil {
			return BackupResult{}, err
		}
		name := request.ArchiveName
		if name == "" {
			name = "veil_backup_" + config.Now().UTC().Format("20060102_150405") + ".tar.gz.enc"
		}
		if err := atomicfile.Write(filepath.Join(request.BackupRoot, name), data, 0o600, 0o700); err != nil {
			return BackupResult{}, err
		}
		return BackupResult{ArchiveName: name, Verified: true}, nil
	case BackupActionList:
		entries, err := backup.ListArchives(request.BackupRoot)
		if err != nil {
			return BackupResult{}, err
		}
		result := BackupResult{Archives: make([]BackupArchive, 0, len(entries))}
		for _, entry := range entries {
			result.Archives = append(result.Archives, BackupArchive{
				Name: entry.Name, Size: entry.Size, CreatedAt: entry.CreatedAt.Format(time.RFC3339), Encrypted: entry.Encrypted,
			})
		}
		return result, nil
	case BackupActionVerify:
		data, err := os.ReadFile(request.ArchivePath)
		if err != nil {
			return BackupResult{}, err
		}
		if _, err := backup.VerifyBackup(data, passphrase); err != nil {
			return BackupResult{}, err
		}
		return BackupResult{ArchiveName: request.ArchiveName, Verified: true}, nil
	case BackupActionPrune:
		policy := backup.RetentionPolicy{Daily: request.Daily, Weekly: request.Weekly, Monthly: request.Monthly}
		if request.Daily == 0 && request.Weekly == 0 && request.Monthly == 0 {
			policy = backup.DefaultRetentionPolicy()
		}
		pruned, err := backup.PruneArchives(request.BackupRoot, policy, false)
		if err != nil {
			return BackupResult{}, err
		}
		return BackupResult{Pruned: pruned.Deleted, Kept: pruned.Kept}, nil
	case BackupActionRestore:
		data, err := os.ReadFile(request.ArchivePath)
		if err != nil {
			return BackupResult{}, err
		}
		restored, err := backup.RestoreBackupWithOptions(
			data,
			request.StatePath,
			request.KeyPath,
			passphrase,
			backup.RestoreOptions{CheckOnly: request.CheckOnly},
		)
		if err != nil {
			return BackupResult{}, err
		}
		return BackupResult{
			ArchiveName:     request.ArchiveName,
			Verified:        true,
			Restored:        !request.CheckOnly,
			SafetyStatePath: restored.SafetyStatePath,
			SafetyKeyPath:   restored.SafetyKeyPath,
		}, nil
	default:
		return BackupResult{}, errors.New("unsupported backup operation")
	}
}
