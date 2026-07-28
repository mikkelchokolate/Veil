package privileged

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mikkelchokolate/Veil/internal/atomicfile"
	"github.com/mikkelchokolate/Veil/internal/backup"
	"github.com/mikkelchokolate/Veil/internal/caddycert"
	updateflow "github.com/mikkelchokolate/Veil/internal/cliflow/update"
	"github.com/mikkelchokolate/Veil/internal/releaseverify"
	"github.com/mikkelchokolate/Veil/internal/service"
	"github.com/mikkelchokolate/Veil/internal/statecommit"
)

const (
	maxBackupPassphraseBytes int64 = 64 * 1024
	maxUpdateArchiveBytes    int64 = 64 * 1024 * 1024
	maxChecksumsBytes        int64 = 1024 * 1024
	maxReleaseEvidenceBytes  int64 = 8 * 1024 * 1024
)

// Test hooks for functions that touch global runtime state or external
// systems. They are swapped in tests to keep unit tests hermetic.
var (
	caddyDataDir           = caddycert.DefaultCaddyDataDir
	findCaddyCertPair      = caddycert.FindPair
	osExecutable           = os.Executable
	effectiveUID           = os.Geteuid
	lookupUser             = user.Lookup
	chownPath              = chownNoFollow
	chmodPath              = chmodNoFollow
	openNoFollow           = openRegularNoFollow
	caddyRetryInterval     = 2 * time.Second
	defaultCaddyCertOutDir = "/etc/veil/certs"
)

// chownNoFollow opens path with O_NOFOLLOW and applies fchown on the file
// descriptor, so a symlink swapped in after policy resolution is rejected
// rather than followed to an unintended target.
func chownNoFollow(path string, uid, gid int) error {
	f, err := openNoFollow(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Chown(uid, gid)
}

// chmodNoFollow opens path with O_NOFOLLOW and applies fchmod on the file
// descriptor, avoiding symlink-following chmod on a swapped path.
func chmodNoFollow(path string, mode os.FileMode) error {
	f, err := openNoFollow(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Chmod(mode)
}

type Executor struct {
	Promote            func(context.Context, ResolvedPromotion) (PromoteResult, error)
	ServiceAction      func(context.Context, ServiceActionRequest) error
	ServiceStatus      func(context.Context, ServiceStatusRequest) (ServiceStatusResult, error)
	Journal            func(context.Context, ResolvedJournal) (JournalResult, error)
	Backup             func(context.Context, ResolvedBackup) (BackupResult, error)
	RotateKey          func(context.Context, RotateKeyRequest) error
	RecoverKeyRotation func(context.Context) error
	Firewall           func(context.Context, ResolvedFirewall) (FirewallResult, error)
	Update             func(context.Context, ResolvedUpdate) (UpdateResult, error)
	RestartPanel       func(context.Context) error
	SyncCaddyCert      func(context.Context, SyncCaddyCertRequest) (SyncCaddyCertResult, error)
}

type CommandRunner func(context.Context, []string, time.Duration) (string, error)

type ProductionConfig struct {
	PromotionBackupRoot        string
	StatePath                  string
	KeyPath                    string
	BackupPassphrasePath       string
	BackupRoot                 string
	BackupMaxBytes             int64
	VeilVersion                string
	BinaryPath                 string
	FirewallCommands           map[string][]string
	RunCommand                 CommandRunner
	BackupWorkflow             func(context.Context, ResolvedBackup) (BackupResult, error)
	UpdateWorkflow             func(context.Context, ResolvedUpdate) (UpdateResult, error)
	RotateKeyWorkflow          func(context.Context) error
	RecoverKeyRotationWorkflow func(context.Context) error
	ReleaseVerifier            func(releaseverify.Evidence) error
	Now                        func() time.Time
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
	if config.ReleaseVerifier == nil {
		config.ReleaseVerifier = releaseverify.Verify
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
	if config.RecoverKeyRotationWorkflow == nil {
		config.RecoverKeyRotationWorkflow = func(context.Context) error {
			databasePath := ""
			if config.StatePath != "" {
				databasePath = filepath.Join(filepath.Dir(config.StatePath), "veil.db")
			}
			if err := backup.RecoverInterruptedRestore(config.StatePath, config.KeyPath, databasePath); err != nil {
				return fmt.Errorf("recover interrupted backup restore: %w", err)
			}
			return statecommit.RecoverKeyRotation(statecommit.RecoverKeyRotationOptions{StatePath: config.StatePath})
		}
	}
	return Executor{
		Promote: func(_ context.Context, request ResolvedPromotion) (PromoteResult, error) {
			return promoteResolvedArtifacts(config.PromotionBackupRoot, config.Now, request)
		},
		ServiceAction: func(ctx context.Context, request ServiceActionRequest) error {
			action := string(request.Action)
			if request.Action == ServiceActionReload {
				// Reload on an inactive unit fails with "cannot reload because it is
				// inactive". Fall back to start so first-time applies and recovery
				// paths work without dropping connections on already-active units.
				status, _ := config.RunCommand(ctx, []string{"systemctl", "is-active", request.Unit}, 5*time.Second)
				if strings.TrimSpace(status) != "active" {
					action = "start"
				}
			}
			_, err := config.RunCommand(ctx, []string{"systemctl", action, request.Unit}, 30*time.Second)
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
			lines := boundedJournalLines(output, 256*1024, 16*1024)
			return JournalResult{Unit: request.Unit, Lines: lines}, nil
		},
		Backup: config.BackupWorkflow,
		RotateKey: func(ctx context.Context, _ RotateKeyRequest) error {
			return config.RotateKeyWorkflow(ctx)
		},
		RecoverKeyRotation: config.RecoverKeyRotationWorkflow,
		Firewall: func(ctx context.Context, request ResolvedFirewall) (FirewallResult, error) {
			resolved := ResolvedFirewall{RuleIDs: append([]string(nil), request.RuleIDs...), Rules: append([]FirewallRule(nil), request.Rules...)}
			if len(resolved.RuleIDs) > 0 && len(resolved.Rules) == 0 {
				for _, id := range resolved.RuleIDs {
					command, ok := config.FirewallCommands[id]
					if !ok || len(command) < 3 || command[0] != "ufw" {
						return FirewallResult{}, fmt.Errorf("firewall rule %q has no validated production ufw command", id)
					}
					resolved.Rules = append(resolved.Rules, FirewallRule{Command: "ufw", Args: append([]string(nil), command[1:]...)})
				}
			}
			if len(resolved.Rules) > 0 && len(resolved.RuleIDs) == 0 {
				for _, rule := range resolved.Rules {
					id := "dynamic"
					if len(rule.Args) >= 2 {
						id += ":" + rule.Args[1]
					}
					resolved.RuleIDs = append(resolved.RuleIDs, id)
				}
			}
			return runFirewallRules(ctx, config.RunCommand, resolved)
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
	checksumsBundle, err := readBoundedRegularFile(request.ChecksumsBundlePath, maxReleaseEvidenceBytes)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("read staged checksum bundle: %w", err)
	}
	provenance, err := readBoundedRegularFile(request.ProvenancePath, maxReleaseEvidenceBytes)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("read staged provenance: %w", err)
	}
	provenanceBundle, err := readBoundedRegularFile(request.ProvenanceBundlePath, maxReleaseEvidenceBytes)
	if err != nil {
		return UpdateResult{}, fmt.Errorf("read staged provenance bundle: %w", err)
	}
	if config.ReleaseVerifier == nil {
		return UpdateResult{}, errors.New("release verifier is not configured")
	}
	if err := config.ReleaseVerifier(releaseverify.Evidence{
		Repository:   updateflow.RepoOwner + "/" + updateflow.RepoName,
		WorkflowPath: ".github/workflows/release.yml", ReleaseTag: request.Version,
		ArchiveName: updateflow.AssetName(), Archive: archive,
		ChecksumsName: "checksums.txt", Checksums: checksums, ChecksumsBundle: checksumsBundle,
		Provenance: provenance, ProvenanceBundle: provenanceBundle,
	}); err != nil {
		return UpdateResult{}, fmt.Errorf("verify staged release provenance: %w", err)
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
	for _, path := range []string{request.Path, request.ChecksumsPath, request.ChecksumsBundlePath, request.ProvenancePath, request.ProvenanceBundlePath} {
		_ = os.Remove(path)
	}
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

// readManagedConfigFile reads a promotion source/backup/destination config
// without following symlinks. A symlink swapped in after policy resolution
// would otherwise let the caller read a file outside the managed root (either
// directly into the promotion, or into a backup retrievable via the backup ID).
// Opening with O_NOFOLLOW closes the resolve-time-to-read TOCTOU window.
func readManagedConfigFile(path string) ([]byte, error) {
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
		return nil, errors.New("managed config is not a regular file")
	}
	return io.ReadAll(file)
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
	return reconcileUFW(ctx, runCommand, request)
}

func isUFWDuplicateRule(output string) bool {
	return strings.Contains(output, "Skipping adding existing rule") || strings.Contains(output, "already exists")
}

func promoteResolvedArtifacts(backupRoot string, now func() time.Time, request ResolvedPromotion) (PromoteResult, error) {
	if err := recoverPromotionTransaction(backupRoot); err != nil {
		return PromoteResult{}, fmt.Errorf("recover interrupted promotion: %w", err)
	}
	if request.RestoreBackupID != "" {
		return restorePromotedArtifacts(backupRoot, request.RestoreBackupID)
	}
	return executePromotionTransaction(backupRoot, now, "promotion", request.Artifacts, request.RemoveArtifacts)
}

type promotionManifest struct {
	Version       int                       `json:"version,omitempty"`
	TransactionID string                    `json:"transactionId,omitempty"`
	BackupID      string                    `json:"backupId"`
	Kind          string                    `json:"kind,omitempty"`
	Phase         string                    `json:"phase,omitempty"`
	Records       []promotionManifestRecord `json:"records"`
}

type promotionManifestRecord struct {
	TransactionID string `json:"transactionId,omitempty"`
	ArtifactID    string `json:"artifactId"`
	Destination   string `json:"destination"`
	BackupPath    string `json:"backupPath,omitempty"`
	SafetyPath    string `json:"safetyPath,omitempty"`
	HadPrevious   bool   `json:"hadPrevious"`
	OldDigest     string `json:"oldDigest,omitempty"`
	NewDigest     string `json:"newDigest,omitempty"`
	Operation     string `json:"operation,omitempty"`
	Phase         string `json:"phase,omitempty"`
	WasSymlink    bool   `json:"wasSymlink,omitempty"`
	OldLinkTarget string `json:"oldLinkTarget,omitempty"`
}

func backupPromotionDestination(root, backupID string, artifact ResolvedArtifact) (promotionManifestRecord, error) {
	record := promotionManifestRecord{ArtifactID: artifact.ID, Destination: artifact.Destination}
	// Lstat (not Stat/ReadFile) so a symlink swapped in after policy resolution
	// is detected. Reading a symlink would copy the target's content — possibly
	// outside the managed root — into the backup, which the caller could then
	// retrieve via the returned backup ID (exfiltration). Skip the content
	// backup for symlinks; the removal step deletes the link itself.
	if info, statErr := os.Lstat(artifact.Destination); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return record, nil
	}
	body, err := readManagedConfigFile(artifact.Destination)
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
	record.SafetyPath = path
	record.HadPrevious = true
	record.OldDigest = promotionDigest(body)
	return record, nil
}

func restorePromotedArtifacts(root, backupID string) (PromoteResult, error) {
	if err := recoverPromotionTransaction(root); err != nil {
		return PromoteResult{}, fmt.Errorf("recover interrupted promotion: %w", err)
	}
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
	writes := make([]ResolvedArtifact, 0, len(manifest.Records))
	removes := make([]ResolvedArtifact, 0, len(manifest.Records))
	for _, record := range manifest.Records {
		if record.ArtifactID == "" || record.Destination == "" {
			return PromoteResult{}, errors.New("promotion backup manifest has an invalid record")
		}
		if record.HadPrevious {
			if record.WasSymlink {
				if record.OldLinkTarget == "" {
					return PromoteResult{}, errors.New("promotion backup symlink metadata is invalid")
				}
				if record.OldDigest != "" && promotionDigest([]byte(record.OldLinkTarget)) != record.OldDigest {
					return PromoteResult{}, fmt.Errorf("promotion backup symlink digest mismatch for %s", record.ArtifactID)
				}
				writes = append(writes, ResolvedArtifact{ID: record.ArtifactID, Destination: record.Destination, SymlinkTarget: record.OldLinkTarget})
				continue
			}
			source := record.SafetyPath
			if source == "" {
				source = record.BackupPath
			}
			if source == "" || !pathWithin(root, source) {
				return PromoteResult{}, errors.New("promotion backup manifest safety path is invalid")
			}
			previous, err := readManagedConfigFile(source)
			if err != nil {
				return PromoteResult{}, err
			}
			if record.OldDigest != "" && promotionDigest(previous) != record.OldDigest {
				return PromoteResult{}, fmt.Errorf("promotion backup digest mismatch for %s", record.ArtifactID)
			}
			writes = append(writes, ResolvedArtifact{ID: record.ArtifactID, Source: source, Destination: record.Destination})
		} else {
			removes = append(removes, ResolvedArtifact{ID: record.ArtifactID, Destination: record.Destination})
		}
	}
	result, err := executePromotionTransaction(root, time.Now, "rollback", writes, removes)
	if err != nil {
		return PromoteResult{}, err
	}
	result.BackupID = backupID
	// Historical callers treat both restored and removed destinations as written
	// rollback artifacts. Preserve that API while the transaction manifest keeps
	// the exact operation kind.
	result.WrittenArtifacts = result.WrittenArtifacts[:0]
	for _, record := range manifest.Records {
		result.WrittenArtifacts = append(result.WrittenArtifacts, record.ArtifactID)
	}
	result.RemovedArtifacts = nil
	return result, nil
}

// chownToVeilIfRoot changes the owner of path to the veil user when the helper
// is running as root. This keeps generated config files and certificates
// readable by the protocol runtime services, which run as the veil user. A
// missing/invalid veil account or failed chown is fatal so Apply can roll back
// instead of reporting success for an unreadable runtime artifact.
func chownToVeilIfRoot(path string) error {
	if effectiveUID() != 0 {
		return nil
	}
	u, err := lookupUser("veil")
	if err != nil {
		return fmt.Errorf("resolve veil user: %w", err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("parse veil uid %q: %w", u.Uid, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("parse veil gid %q: %w", u.Gid, err)
	}
	return chownPath(path, uid, gid)
}

func ensureRuntimeArtifactOwnership(artifactID, path string) error {
	if strings.HasPrefix(filepath.ToSlash(filepath.Clean(artifactID)), "caddy/") {
		return grantPanelReadAccessToCaddyArtifact(path)
	}
	if err := chownToVeilIfRoot(filepath.Dir(path)); err != nil {
		return fmt.Errorf("set protocol config directory ownership: %w", err)
	}
	if err := chownToVeilIfRoot(path); err != nil {
		return fmt.Errorf("set protocol config ownership: %w", err)
	}
	return nil
}

// grantPanelReadAccessToCaddyArtifact keeps the Caddy config owned by root for
// the root Caddy service while making it readable by the veil group for the
// unprivileged Panel's Caddy Admin API loader. The config contains credentials,
// so it must not be world-readable.
func grantPanelReadAccessToCaddyArtifact(path string) error {
	if effectiveUID() != 0 {
		return nil
	}
	u, err := lookupUser("veil")
	if err != nil {
		return fmt.Errorf("resolve veil user: %w", err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("parse veil gid %q: %w", u.Gid, err)
	}
	dir := filepath.Dir(path)
	if err := chownPath(dir, 0, gid); err != nil {
		return fmt.Errorf("set caddy config directory ownership: %w", err)
	}
	if err := chmodPath(dir, 0o750); err != nil {
		return fmt.Errorf("set caddy config directory mode: %w", err)
	}
	if err := chownPath(path, 0, gid); err != nil {
		return fmt.Errorf("set caddy config ownership: %w", err)
	}
	if err := chmodPath(path, 0o640); err != nil {
		return fmt.Errorf("set caddy config mode: %w", err)
	}
	return nil
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
		maxBytes, err := configuredBackupMaxBytes(config)
		if err != nil {
			return BackupResult{}, err
		}
		file, err := openRegularNoFollow(request.ArchivePath)
		if err != nil {
			return BackupResult{}, err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return BackupResult{}, err
		}
		if !info.Mode().IsRegular() {
			return BackupResult{}, errors.New("backup archive is not a regular file")
		}
		if info.Size() > maxBytes {
			return BackupResult{}, fmt.Errorf("configured backup size policy exceeded: %d bytes > %d bytes", info.Size(), maxBytes)
		}
		if request.Offset > info.Size() {
			return BackupResult{}, errors.New("backup read offset exceeds archive size")
		}
		limit := request.Limit
		if limit == 0 {
			limit = maxBackupReadChunkBytes
		}
		remaining := info.Size() - request.Offset
		if limit > remaining {
			limit = remaining
		}
		data := make([]byte, int(limit))
		if limit > 0 {
			n, err := file.ReadAt(data, request.Offset)
			if err != nil && err != io.EOF {
				return BackupResult{}, err
			}
			if int64(n) != limit {
				return BackupResult{}, io.ErrUnexpectedEOF
			}
		}
		return BackupResult{
			ArchiveName: request.ArchiveName,
			Data:        data,
			More:        request.Offset+int64(len(data)) < info.Size(),
		}, nil
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
	passphraseBody, err := readBoundedRegularFile(request.BackupPassphrasePath, maxBackupPassphraseBytes)
	if err != nil {
		return BackupResult{}, fmt.Errorf("read backup passphrase: %w", err)
	}
	passphrase := strings.TrimRight(string(passphraseBody), "\r\n")
	if len(passphrase) < 16 {
		return BackupResult{}, errors.New("configured backup passphrase is too short")
	}
	maxBytes, err := configuredBackupMaxBytes(config)
	if err != nil {
		return BackupResult{}, err
	}
	switch request.Action {
	case BackupActionCreate:
		createdAt := config.Now().UTC()
		databasePath := request.DatabasePath
		if databasePath == "" {
			databasePath = filepath.Join(filepath.Dir(request.StatePath), "veil.db")
		}
		name := request.ArchiveName
		if name == "" {
			name, err = generatedBackupArchiveName(createdAt)
			if err != nil {
				return BackupResult{}, err
			}
		}
		if err := os.MkdirAll(request.BackupRoot, 0o700); err != nil {
			return BackupResult{}, err
		}
		pending, err := os.CreateTemp(request.BackupRoot, ".veil-backup-pending-*")
		if err != nil {
			return BackupResult{}, err
		}
		pendingPath := pending.Name()
		if err := pending.Close(); err != nil {
			_ = os.Remove(pendingPath)
			return BackupResult{}, err
		}
		_ = os.Remove(pendingPath)
		defer os.Remove(pendingPath)
		if err := backup.CreateBackupFileWithOptions(pendingPath, request.StatePath, request.KeyPath, passphrase, backup.ArchiveOptions{
			VeilVersion: config.VeilVersion, CreatedAt: createdAt,
			DatabasePath: databasePath, MaxBytes: maxBytes,
		}); err != nil {
			return BackupResult{}, err
		}
		report, err := backup.VerifyBackupFile(pendingPath, passphrase, maxBytes)
		if err != nil {
			return BackupResult{}, err
		}
		archivePath := filepath.Join(request.BackupRoot, name)
		// pendingPath and archivePath are in the same directory. Linking publishes
		// the verified inode atomically and fails with EEXIST instead of replacing
		// an archive created concurrently by the API, timer, or another helper.
		if err := os.Link(pendingPath, archivePath); err != nil {
			return BackupResult{}, err
		}
		if err := os.Remove(pendingPath); err != nil {
			return BackupResult{}, err
		}
		if err := syncBackupDirectory(request.BackupRoot); err != nil {
			return BackupResult{}, err
		}
		return BackupResult{ArchiveName: name, Verified: true, Verification: privilegedBackupVerification(report)}, nil
	case BackupActionVerify:
		report, err := backup.VerifyBackupFile(request.ArchivePath, passphrase, maxBytes)
		if err != nil {
			return BackupResult{}, err
		}
		return BackupResult{ArchiveName: request.ArchiveName, Verified: true, Verification: privilegedBackupVerification(report)}, nil
	case BackupActionRestore:
		databasePath := request.DatabasePath
		if databasePath == "" {
			databasePath = filepath.Join(filepath.Dir(request.StatePath), "veil.db")
		}
		restored, err := backup.RestoreBackupFileWithOptions(
			request.ArchivePath,
			request.StatePath,
			request.KeyPath,
			passphrase,
			backup.RestoreOptions{CheckOnly: request.CheckOnly, DatabasePath: databasePath, MaxBytes: maxBytes},
		)
		if err != nil {
			return BackupResult{}, err
		}
		return BackupResult{
			ArchiveName:        request.ArchiveName,
			Verified:           true,
			Restored:           !request.CheckOnly,
			Verification:       privilegedBackupVerification(restored.Verification),
			SafetyStatePath:    restored.SafetyStatePath,
			SafetyKeyPath:      restored.SafetyKeyPath,
			SafetyDatabasePath: restored.SafetyDatabasePath,
		}, nil
	default:
		return BackupResult{}, errors.New("unsupported backup operation")
	}
}

func generatedBackupArchiveName(createdAt time.Time) (string, error) {
	var suffix [16]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("generate backup archive identifier: %w", err)
	}
	return fmt.Sprintf(
		"veil_backup_%s_%s.tar.gz.enc",
		createdAt.UTC().Format("20060102_150405_000000000"),
		hex.EncodeToString(suffix[:]),
	), nil
}

func configuredBackupMaxBytes(config ProductionConfig) (int64, error) {
	if config.BackupMaxBytes != 0 {
		if config.BackupMaxBytes < 0 {
			return 0, errors.New("configured backup size policy must be positive")
		}
		return config.BackupMaxBytes, nil
	}
	return backup.ConfiguredMaxBackupBytes()
}

func privilegedBackupVerification(report backup.VerificationReport) *BackupVerificationReport {
	files := make([]BackupVerificationFile, 0, len(report.Files))
	for _, file := range report.Files {
		files = append(files, BackupVerificationFile{Name: file.Name, Size: file.Size, SHA256: file.SHA256})
	}
	createdAt := ""
	if !report.CreatedAt.IsZero() {
		createdAt = report.CreatedAt.UTC().Format(time.RFC3339)
	}
	return &BackupVerificationReport{
		FormatVersion: report.FormatVersion, EncryptionVersion: report.EncryptionVersion,
		Encrypted: report.Encrypted, Legacy: report.Legacy, CreatedAt: createdAt,
		VeilVersion: report.VeilVersion, StateSchemaVersion: report.StateSchemaVersion,
		DesiredRevision: report.DesiredRevision, Files: files,
	}
}

func syncBackupDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

// runSyncCaddyCert copies a Caddy-managed ACME certificate for domain into
// OutDir so that non-Caddy runtimes (Hysteria2, etc.) can serve a real
// Let's Encrypt certificate. It polls briefly because Caddy may still be
// issuing the certificate when the apply workflow runs.
func runSyncCaddyCert(ctx context.Context, request SyncCaddyCertRequest, config ProductionConfig) (SyncCaddyCertResult, error) {
	if request.Domain == "" {
		return SyncCaddyCertResult{}, newError(ErrorInvalidRequest, "domain is required")
	}
	if !dnsLabelPattern.MatchString(request.Domain) {
		return SyncCaddyCertResult{}, newError(ErrorInvalidRequest, "domain must be a valid DNS label")
	}
	if request.OutDir == "" {
		request.OutDir = defaultCaddyCertOutDir
	}
	if filepath.IsAbs(request.OutDir) && !strings.HasPrefix(filepath.Clean(request.OutDir), caddyCertRoot) {
		return SyncCaddyCertResult{}, newError(ErrorForbiddenOperation, "certificate output directory is not allowed")
	}
	if strings.Contains(request.OutDir, "..") {
		return SyncCaddyCertResult{}, newError(ErrorInvalidRequest, "certificate output directory must not contain '..'")
	}
	request.OutDir = filepath.Clean(request.OutDir)
	if !strings.HasPrefix(request.OutDir, caddyCertRoot) {
		return SyncCaddyCertResult{}, newError(ErrorForbiddenOperation, "certificate output directory is not allowed")
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
	if err := chownToVeilIfRoot(request.OutDir); err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("set certificate directory ownership: %w", err)
	}
	certOut := filepath.Join(request.OutDir, request.Domain+".crt")
	keyOut := filepath.Join(request.OutDir, request.Domain+".key")
	if err := atomicfile.Write(certOut, certData, 0o600, 0o700); err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("write certificate: %w", err)
	}
	if err := chownToVeilIfRoot(certOut); err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("set certificate ownership: %w", err)
	}
	if err := atomicfile.Write(keyOut, keyData, 0o600, 0o700); err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("write key: %w", err)
	}
	if err := chownToVeilIfRoot(keyOut); err != nil {
		return SyncCaddyCertResult{}, fmt.Errorf("set certificate key ownership: %w", err)
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
