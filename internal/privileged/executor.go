package privileged

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/caddycert"
	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
	"github.com/mikkelchokolate/Veil/internal/service"
)

const (
	maxBackupReadBytes    int64 = 64 * 1024 * 1024
	maxUpdateArchiveBytes int64 = 64 * 1024 * 1024
	maxChecksumsBytes     int64 = 1024 * 1024
)

// Test hooks for functions that touch global runtime state or external
// systems. They are swapped in tests to keep unit tests hermetic.
var (
	caddyDataDir           = caddycert.DefaultCaddyDataDir
	findCaddyCertPair      = caddycert.FindPair
	osExecutable           = os.Executable
	caddyRetryInterval     = 2 * time.Second
	defaultCaddyCertOutDir = "/etc/veil/certs"
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
	SyncCaddyCert func(context.Context, SyncCaddyCertRequest) (SyncCaddyCertResult, error)
}

type CommandRunner func(context.Context, []string, time.Duration) (string, error)

type ProductionConfig struct {
	PromotionBackupRoot  string
	StatePath            string
	KeyPath              string
	BackupPassphrasePath string
	BackupRoot           string
	VeilVersion          string
	BinaryPath           string
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
			return runProductionUpdate(config, request)
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
				// Template units (foo@.service) have no runtime status of their own
				// and cannot be queried with `systemctl show`; report them as
				// inactive instead of failing the whole batch.
				if strings.HasSuffix(unit, "@.service") {
					result.Services = append(result.Services, ServiceStatus{
						Unit: unit, LoadState: "loaded", ActiveState: "inactive", SubState: "dead",
					})
					continue
				}
				command := service.NewSystemdServiceStatusCommand(unit)
				output, err := config.RunCommand(ctx, append([]string{command.Name()}, command.Args()...), command.Timeout())
				if err != nil {
					// One unit's query failure must not break the whole status page.
					detail := strings.TrimSpace(output)
					if detail == "" {
						detail = err.Error()
					}
					result.Services = append(result.Services, ServiceStatus{
						Unit: unit, LoadState: "unknown", ActiveState: "unknown", SubState: "unknown",
						Error: detail,
					})
					continue
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
			rulesResult, err := runFirewallRules(ctx, config.RunCommand, ResolvedFirewall{Rules: request.Rules})
			if err != nil {
				return FirewallResult{}, err
			}
			result.AppliedRuleIDs = append(result.AppliedRuleIDs, rulesResult.AppliedRuleIDs...)
			return result, nil
		},
		Update: config.UpdateWorkflow,
		RestartPanel: func(ctx context.Context) error {
			_, err := config.RunCommand(ctx, []string{"systemctl", "restart", "veil.service"}, 30*time.Second)
			return err
		},
		SyncCaddyCert: func(ctx context.Context, request SyncCaddyCertRequest) (SyncCaddyCertResult, error) {
			return runSyncCaddyCert(ctx, request, config)
		},
	}
}

func runProductionUpdate(config ProductionConfig, request ResolvedUpdate) (UpdateResult, error) {
	archive, err := readBoundedRegularFile(request.Path, maxUpdateArchiveBytes)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("read staged update archive: %w", err)
	}
	checksums, err := readBoundedRegularFile(request.ChecksumsPath, maxChecksumsBytes)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("read staged checksums: %w", err)
	}
	if err := updateflow.VerifyAssetChecksum(archive, updateflow.AssetName(), string(checksums)); err != nil {
		return UpdateResult{}, fmt.Errorf("verify staged update: %w", err)
	}
	binaryPath := config.BinaryPath
	if binaryPath == "" {
		binaryPath, err = osExecutable()
		if err != nil {
			return UpdateResult{}, fmt.Errorf("resolve current executable: %w", err)
		}
	}
	if _, err := updateflow.ReplaceBinaryFromArchive(binaryPath, archive, true); err != nil {
		return UpdateResult{}, fmt.Errorf("install staged update: %w", err)
	}
	_ = os.Remove(request.Path)
	_ = os.Remove(request.ChecksumsPath)
	return UpdateResult{
		ArtifactID: request.ArtifactID,
		Staged:     true,
		Installed:  true,
		Version:    request.Version,
	}, nil
}

func readBoundedRegularFile(path string, maxBytes int64) ([]byte, error) {
	file, err := openRegularNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("managed file is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("managed file exceeds size limit")
	}
	return data, nil
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

func runFirewallRules(ctx context.Context, runCommand CommandRunner, request ResolvedFirewall) (FirewallResult, error) {
	result := FirewallResult{}
	for _, id := range request.RuleIDs {
		_ = id
	}
	if len(request.Rules) == 0 {
		return result, nil
	}
	status, err := runCommand(ctx, []string{"ufw", "status"}, 15*time.Second)
	if err != nil {
		return FirewallResult{}, fmt.Errorf("read ufw status: %w", err)
	}
	if !strings.Contains(status, "Status: active") {
		if _, err := runCommand(ctx, []string{"ufw", "--force", "enable"}, 15*time.Second); err != nil {
			return FirewallResult{}, fmt.Errorf("enable ufw: %w", err)
		}
	}
	for _, rule := range request.Rules {
		output, err := runCommand(ctx, append([]string{"ufw"}, rule.Args...), 15*time.Second)
		if err != nil && !isUFWDuplicateRule(output) {
			return FirewallResult{}, fmt.Errorf("ufw %v: %w", rule.Args, err)
		}
		if len(rule.Args) >= 2 {
			result.AppliedRuleIDs = append(result.AppliedRuleIDs, rule.Args[1])
		}
	}
	return result, nil
}

func isUFWDuplicateRule(output string) bool {
	return strings.Contains(output, "Skipping adding existing rule") || strings.Contains(output, "already exists")
}

func promoteResolvedArtifacts(backupRoot string, now func() time.Time, request ResolvedPromotion) (PromoteResult, error) {
	if request.RestoreBackupID != "" {
		return restorePromotedArtifacts(backupRoot, request.RestoreBackupID)
	}
	result := PromoteResult{}
	backupID := now().UTC().Format("20060102T150405.000000000Z")
	manifest := promotionManifest{BackupID: backupID}
	for _, artifact := range request.Artifacts {
		body, err := os.ReadFile(artifact.Source)
		if err != nil {
			return PromoteResult{}, err
		}
		record, err := backupPromotionDestination(backupRoot, backupID, artifact)
		if err != nil {
			return PromoteResult{}, err
		}
		if err := atomicfile.Write(artifact.Destination, body, 0o600, 0o700); err != nil {
			return PromoteResult{}, err
		}
		result.WrittenArtifacts = append(result.WrittenArtifacts, artifact.ID)
		manifest.Records = append(manifest.Records, record)
		if record.BackupPath != "" {
			result.BackupArtifacts = append(result.BackupArtifacts, record.BackupPath)
		}
	}
	for _, artifact := range request.RemoveArtifacts {
		record, err := backupPromotionDestination(backupRoot, backupID, artifact)
		if err != nil {
			return PromoteResult{}, err
		}
		if err := os.Remove(artifact.Destination); err != nil && !errors.Is(err, os.ErrNotExist) {
			return PromoteResult{}, err
		}
		result.RemovedArtifacts = append(result.RemovedArtifacts, artifact.ID)
		manifest.Records = append(manifest.Records, record)
		if record.BackupPath != "" {
			result.BackupArtifacts = append(result.BackupArtifacts, record.BackupPath)
		}
	}
	if len(result.WrittenArtifacts) > 0 || len(result.RemovedArtifacts) > 0 {
		body, err := json.Marshal(manifest)
		if err != nil {
			return PromoteResult{}, err
		}
		if err := atomicfile.Write(filepath.Join(backupRoot, backupID, "manifest.json"), body, 0o600, 0o700); err != nil {
			return PromoteResult{}, err
		}
		result.BackupID = backupID
	}
	return result, nil
}

type promotionManifest struct {
	BackupID string                    `json:"backupId"`
	Records  []promotionManifestRecord `json:"records"`
}

type promotionManifestRecord struct {
	ArtifactID  string `json:"artifactId"`
	Destination string `json:"destination"`
	BackupPath  string `json:"backupPath,omitempty"`
	HadPrevious bool   `json:"hadPrevious"`
}

func backupPromotionDestination(root, backupID string, artifact ResolvedArtifact) (promotionManifestRecord, error) {
	record := promotionManifestRecord{ArtifactID: artifact.ID, Destination: artifact.Destination}
	body, err := os.ReadFile(artifact.Destination)
	if errors.Is(err, os.ErrNotExist) {
		return record, nil
	}
	if err != nil {
		return promotionManifestRecord{}, err
	}
	if root == "" {
		return promotionManifestRecord{}, errors.New("promotion backup root is required")
	}
	path := filepath.Join(root, backupID, filepath.Clean(artifact.ID))
	if err := atomicfile.Write(path, body, 0o600, 0o700); err != nil {
		return promotionManifestRecord{}, err
	}
	record.BackupPath = path
	record.HadPrevious = true
	return record, nil
}

func restorePromotedArtifacts(root, backupID string) (PromoteResult, error) {
	body, err := os.ReadFile(filepath.Join(root, backupID, "manifest.json"))
	if err != nil {
		return PromoteResult{}, err
	}
	var manifest promotionManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return PromoteResult{}, err
	}
	if manifest.BackupID != backupID {
		return PromoteResult{}, errors.New("promotion backup manifest mismatch")
	}
	result := PromoteResult{BackupID: backupID}
	for _, record := range manifest.Records {
		if record.HadPrevious {
			previous, err := os.ReadFile(record.BackupPath)
			if err != nil {
				return PromoteResult{}, err
			}
			if err := atomicfile.Write(record.Destination, previous, 0o600, 0o700); err != nil {
				return PromoteResult{}, err
			}
		} else if err := os.Remove(record.Destination); err != nil && !errors.Is(err, os.ErrNotExist) {
			return PromoteResult{}, err
		}
		result.WrittenArtifacts = append(result.WrittenArtifacts, record.ArtifactID)
	}
	return result, nil
}

func runProductionBackup(_ context.Context, config ProductionConfig, request ResolvedBackup) (BackupResult, error) {
	switch request.Action {
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
	case BackupActionRead:
		file, err := os.Open(request.ArchivePath)
		if err != nil {
			return BackupResult{}, err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, maxBackupReadBytes+1))
		if err != nil {
			return BackupResult{}, err
		}
		if int64(len(data)) > maxBackupReadBytes {
			return BackupResult{}, errors.New("backup archive exceeds helper size limit")
		}
		return BackupResult{ArchiveName: request.ArchiveName, Data: data}, nil
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
	}
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
	case BackupActionVerify:
		data, err := os.ReadFile(request.ArchivePath)
		if err != nil {
			return BackupResult{}, err
		}
		if _, err := backup.VerifyBackup(data, passphrase); err != nil {
			return BackupResult{}, err
		}
		return BackupResult{ArchiveName: request.ArchiveName, Verified: true}, nil
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

// runSyncCaddyCert copies a Caddy-managed ACME certificate for domain into
// OutDir so that non-Caddy runtimes (Hysteria2, etc.) can serve a real
// Let's Encrypt certificate. It polls briefly because Caddy may still be
// issuing the certificate when the apply workflow runs.
func runSyncCaddyCert(ctx context.Context, request SyncCaddyCertRequest, config ProductionConfig) (SyncCaddyCertResult, error) {
	if request.Domain == "" {
		return SyncCaddyCertResult{}, newError(ErrorInvalidRequest, "domain is required")
	}
	if request.OutDir == "" {
		request.OutDir = defaultCaddyCertOutDir
	}
	pair, err := findCaddyCertWithRetry(ctx, request.Domain)
	if err != nil {
		return SyncCaddyCertResult{Found: false}, nil
	}
	certData, err := os.ReadFile(pair.CertPath)
	if err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("read Caddy certificate: %w", err)
	}
	keyData, err := os.ReadFile(pair.KeyPath)
	if err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("read Caddy key: %w", err)
	}
	if err := os.MkdirAll(request.OutDir, 0o700); err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("create cert output directory: %w", err)
	}
	certOut := filepath.Join(request.OutDir, request.Domain+".crt")
	keyOut := filepath.Join(request.OutDir, request.Domain+".key")
	if err := atomicfile.Write(certOut, certData, 0o600, 0o700); err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("write certificate: %w", err)
	}
	if err := atomicfile.Write(keyOut, keyData, 0o600, 0o700); err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("write key: %w", err)
	}
	return SyncCaddyCertResult{Found: true, CertPath: certOut, KeyPath: keyOut}, nil
}

func findCaddyCertWithRetry(ctx context.Context, domain string) (caddycert.Pair, error) {
	// Fast path: cert already exists.
	if pair, err := findCaddyCertPair(caddyDataDir, domain); err == nil {
		return pair, nil
	}
	// Caddy may still be issuing; poll briefly.
	ticker := time.NewTicker(caddyRetryInterval)
	defer ticker.Stop()
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		deadline, _ = ctx.Deadline()
	}
	for {
		select {
		case <-ctx.Done():
			return caddycert.Pair{}, ctx.Err()
		case <-ticker.C:
			if pair, err := findCaddyCertPair(caddyDataDir, domain); err == nil {
				return pair, nil
			}
			if time.Now().After(deadline.Add(-500 * time.Millisecond)) {
				return caddycert.Pair{}, caddycert.ErrCertificateNotFound
			}
		}
	}
}
