package privileged

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const ProtocolVersion = 1

type Operation string

const (
	OperationPromote            Operation = "promote"
	OperationServiceAction      Operation = "service_action"
	OperationServiceStatus      Operation = "service_status"
	OperationJournal            Operation = "journal"
	OperationBackupCreate       Operation = "backup_create"
	OperationBackupList         Operation = "backup_list"
	OperationBackupVerify       Operation = "backup_verify"
	OperationBackupRead         Operation = "backup_read"
	OperationBackupPrune        Operation = "backup_prune"
	OperationBackupRestore      Operation = "backup_restore"
	OperationBackupDelete       Operation = "backup_delete"
	OperationRotateKey          Operation = "rotate_key"
	OperationRecoverKeyRotation Operation = "recover_key_rotation"
	OperationFirewallApply      Operation = "firewall_apply"
	OperationStageUpdate        Operation = "stage_update"
	OperationRestartPanel       Operation = "restart_panel"
	OperationSyncCaddyCert      Operation = "sync_caddy_cert"
	OperationCaddyLoad          Operation = "caddy_load"
)

func (o Operation) Valid() bool {
	switch o {
	case OperationPromote,
		OperationServiceAction,
		OperationServiceStatus,
		OperationJournal,
		OperationBackupCreate,
		OperationBackupList,
		OperationBackupVerify,
		OperationBackupRead,
		OperationBackupPrune,
		OperationBackupRestore,
		OperationBackupDelete,
		OperationRotateKey,
		OperationRecoverKeyRotation,
		OperationFirewallApply,
		OperationStageUpdate,
		OperationRestartPanel,
		OperationSyncCaddyCert,
		OperationCaddyLoad:
		return true
	default:
		return false
	}
}

type ServiceAction string

const (
	ServiceActionStart   ServiceAction = "start"
	ServiceActionStop    ServiceAction = "stop"
	ServiceActionRestart ServiceAction = "restart"
	ServiceActionReload  ServiceAction = "reload"
	ServiceActionEnable  ServiceAction = "enable"
	ServiceActionDisable ServiceAction = "disable"
)

type BackupAction string

const (
	BackupActionCreate  BackupAction = "create"
	BackupActionList    BackupAction = "list"
	BackupActionVerify  BackupAction = "verify"
	BackupActionRead    BackupAction = "read"
	BackupActionPrune   BackupAction = "prune"
	BackupActionRestore BackupAction = "restore"
	BackupActionDelete  BackupAction = "delete"
)

type FenceToken struct {
	Owner          string `json:"owner"`
	Generation     uint64 `json:"generation"`
	LeaseExpiresAt int64  `json:"leaseExpiresAt"`
	OperationID    string `json:"operationId"`
}

type PromoteRequest struct {
	ArtifactIDs       []string   `json:"artifactIds,omitempty"`
	RemoveArtifactIDs []string   `json:"removeArtifactIds,omitempty"`
	RestoreBackupID   string     `json:"restoreBackupId,omitempty"`
	Fence             FenceToken `json:"fence"`
}

type PromoteResult struct {
	BackupID         string   `json:"backupId,omitempty"`
	BackupArtifacts  []string `json:"backupArtifacts,omitempty"`
	WrittenArtifacts []string `json:"writtenArtifacts,omitempty"`
	RemovedArtifacts []string `json:"removedArtifacts,omitempty"`
}

type ServiceActionRequest struct {
	Unit   string        `json:"unit"`
	Action ServiceAction `json:"action"`
	Fence  FenceToken    `json:"fence"`
}

type ServiceStatusRequest struct {
	Units []string `json:"units"`
}

type ServiceStatus struct {
	Unit                   string `json:"unit"`
	LoadState              string `json:"loadState,omitempty"`
	ActiveState            string `json:"activeState,omitempty"`
	SubState               string `json:"subState,omitempty"`
	MainPID                int    `json:"mainPid,omitempty"`
	ExecMainStartMonotonic uint64 `json:"execMainStartMonotonic,omitempty"`
	ExecutableDigest       string `json:"executableDigest,omitempty"`
	Error                  string `json:"error,omitempty"`
}

type ServiceStatusResult struct {
	Services []ServiceStatus `json:"services"`
}

type JournalRequest struct {
	Unit  string `json:"unit"`
	Lines int    `json:"lines"`
}

type JournalResult struct {
	Unit  string   `json:"unit"`
	Lines []string `json:"lines"`
}

type BackupRequest struct {
	Action               BackupAction `json:"action"`
	ArchiveName          string       `json:"archiveName,omitempty"`
	Daily                int          `json:"daily,omitempty"`
	Weekly               int          `json:"weekly,omitempty"`
	Monthly              int          `json:"monthly,omitempty"`
	CheckOnly            bool         `json:"checkOnly,omitempty"`
	AllowVersionMismatch bool         `json:"allowVersionMismatch,omitempty"`
	Offset               int64        `json:"offset,omitempty"`
	Limit                int64        `json:"limit,omitempty"`
	TransactionID        string       `json:"transactionId,omitempty"`
	Fence                FenceToken   `json:"fence"`
}

type BackupArchive struct {
	Name      string `json:"name"`
	Size      int64  `json:"size,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	Encrypted bool   `json:"encrypted,omitempty"`
}

type BackupResult struct {
	ArchiveName        string                    `json:"archiveName,omitempty"`
	Archives           []BackupArchive           `json:"archives,omitempty"`
	Verification       *BackupVerificationReport `json:"verification,omitempty"`
	Verified           bool                      `json:"verified,omitempty"`
	Restored           bool                      `json:"restored,omitempty"`
	Phase              string                    `json:"phase,omitempty"`
	Outcome            string                    `json:"outcome,omitempty"`
	Pruned             []string                  `json:"pruned,omitempty"`
	Kept               []string                  `json:"kept,omitempty"`
	SafetyStatePath    string                    `json:"safetyStatePath,omitempty"`
	SafetyKeyPath      string                    `json:"safetyKeyPath,omitempty"`
	SafetyDatabasePath string                    `json:"safetyDatabasePath,omitempty"`
	Data               []byte                    `json:"data,omitempty"`
	More               bool                      `json:"more,omitempty"`
	TransactionID      string                    `json:"transactionId,omitempty"`
	ContentDigest      string                    `json:"contentDigest,omitempty"`
	InodeGeneration    string                    `json:"inodeGeneration,omitempty"`
	BoundSize          int64                     `json:"boundSize,omitempty"`
	Warning            string                    `json:"warning,omitempty"`
}

type RotateKeyRequest struct{}

type RecoverKeyRotationRequest struct{}

// FirewallRule is a single firewall allow rule executed by the privileged helper.
type FirewallRule struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

type FirewallAction string

const (
	FirewallActionApply    FirewallAction = "apply"
	FirewallActionPrepare  FirewallAction = "prepare"
	FirewallActionCommit   FirewallAction = "commit"
	FirewallActionRollback FirewallAction = "rollback"
)

type FirewallRequest struct {
	RuleIDs       []string       `json:"ruleIds,omitempty"`
	Rules         []FirewallRule `json:"rules,omitempty"`
	Action        FirewallAction `json:"action,omitempty"`
	TransactionID string         `json:"transactionId,omitempty"`
	Fence         FenceToken     `json:"fence"`
}

type FirewallResult struct {
	AppliedRuleIDs []string `json:"appliedRuleIds,omitempty"`
	TransactionID  string   `json:"transactionId,omitempty"`
	Prepared       bool     `json:"prepared,omitempty"`
}

type UpdateRequest struct {
	ArtifactID string     `json:"artifactId"`
	Version    string     `json:"version,omitempty"`
	Fence      FenceToken `json:"fence"`
}

type UpdateResult struct {
	ArtifactID         string `json:"artifactId"`
	Staged             bool   `json:"staged"`
	Installed          bool   `json:"installed,omitempty"`
	Version            string `json:"version,omitempty"`
	TransactionID      string `json:"transactionId,omitempty"`
	ExpectedDigest     string `json:"expectedDigest,omitempty"`
	OldDigest          string `json:"oldDigest,omitempty"`
	InstalledInode     string `json:"installedInode,omitempty"`
	ActivationManifest string `json:"activationManifest,omitempty"`
	CommitPhase        string `json:"commitPhase,omitempty"`
}

type RestartPanelRequest struct {
	Fence FenceToken `json:"fence"`
}

type SyncCaddyCertRequest struct {
	Domain string     `json:"domain"`
	OutDir string     `json:"outDir"`
	Fence  FenceToken `json:"fence"`
}

type CaddyLoadRequest struct {
	Config []byte     `json:"config"`
	Fence  FenceToken `json:"fence"`
}

type SyncCaddyCertResult struct {
	CertPath string `json:"certPath,omitempty"`
	KeyPath  string `json:"keyPath,omitempty"`
	Found    bool   `json:"found"`
}

type RequestEnvelope struct {
	Version            int                        `json:"version"`
	RequestID          string                     `json:"requestId"`
	Operation          Operation                  `json:"operation"`
	Promote            *PromoteRequest            `json:"promote,omitempty"`
	ServiceAction      *ServiceActionRequest      `json:"serviceAction,omitempty"`
	ServiceStatus      *ServiceStatusRequest      `json:"serviceStatus,omitempty"`
	Journal            *JournalRequest            `json:"journal,omitempty"`
	Backup             *BackupRequest             `json:"backup,omitempty"`
	RotateKey          *RotateKeyRequest          `json:"rotateKey,omitempty"`
	RecoverKeyRotation *RecoverKeyRotationRequest `json:"recoverKeyRotation,omitempty"`
	Firewall           *FirewallRequest           `json:"firewall,omitempty"`
	Update             *UpdateRequest             `json:"update,omitempty"`
	RestartPanel       *RestartPanelRequest       `json:"restartPanel,omitempty"`
	SyncCaddyCert      *SyncCaddyCertRequest      `json:"syncCaddyCert,omitempty"`
	CaddyLoad          *CaddyLoadRequest          `json:"caddyLoad,omitempty"`
}

type ResponseEnvelope struct {
	Version   int             `json:"version"`
	RequestID string          `json:"requestId"`
	OK        bool            `json:"ok"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *Error          `json:"error,omitempty"`
}

func (r RequestEnvelope) Validate() error {
	if r.Version != ProtocolVersion {
		return fmt.Errorf("unsupported protocol version %d", r.Version)
	}
	if strings.TrimSpace(r.RequestID) == "" {
		return errors.New("requestId is required")
	}
	if !r.Operation.Valid() {
		return fmt.Errorf("unsupported operation %q", r.Operation)
	}
	payloads := []bool{
		r.Promote != nil,
		r.ServiceAction != nil,
		r.ServiceStatus != nil,
		r.Journal != nil,
		r.Backup != nil,
		r.RotateKey != nil,
		r.RecoverKeyRotation != nil,
		r.Firewall != nil,
		r.Update != nil,
		r.RestartPanel != nil,
		r.SyncCaddyCert != nil,
		r.CaddyLoad != nil,
	}
	count := 0
	for _, present := range payloads {
		if present {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("exactly one request payload is required, got %d", count)
	}
	if !r.payloadMatchesOperation() {
		return fmt.Errorf("payload does not match operation %q", r.Operation)
	}
	return nil
}

func (r RequestEnvelope) payloadMatchesOperation() bool {
	switch r.Operation {
	case OperationPromote:
		return r.Promote != nil
	case OperationServiceAction:
		return r.ServiceAction != nil
	case OperationServiceStatus:
		return r.ServiceStatus != nil
	case OperationJournal:
		return r.Journal != nil
	case OperationBackupCreate, OperationBackupList, OperationBackupVerify, OperationBackupRead, OperationBackupPrune, OperationBackupRestore, OperationBackupDelete:
		return r.Backup != nil
	case OperationRotateKey:
		return r.RotateKey != nil
	case OperationRecoverKeyRotation:
		return r.RecoverKeyRotation != nil
	case OperationFirewallApply:
		return r.Firewall != nil
	case OperationStageUpdate:
		return r.Update != nil
	case OperationRestartPanel:
		return r.RestartPanel != nil
	case OperationSyncCaddyCert:
		return r.SyncCaddyCert != nil
	case OperationCaddyLoad:
		return r.CaddyLoad != nil
	default:
		return false
	}
}
