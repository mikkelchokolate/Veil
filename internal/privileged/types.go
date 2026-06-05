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
	OperationPromote       Operation = "promote"
	OperationServiceAction Operation = "service_action"
	OperationServiceStatus Operation = "service_status"
	OperationJournal       Operation = "journal"
	OperationBackupCreate  Operation = "backup_create"
	OperationBackupList    Operation = "backup_list"
	OperationBackupVerify  Operation = "backup_verify"
	OperationBackupRead    Operation = "backup_read"
	OperationBackupPrune   Operation = "backup_prune"
	OperationBackupRestore Operation = "backup_restore"
	OperationRotateKey     Operation = "rotate_key"
	OperationFirewallApply Operation = "firewall_apply"
	OperationStageUpdate   Operation = "stage_update"
	OperationRestartPanel  Operation = "restart_panel"
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
		OperationRotateKey,
		OperationFirewallApply,
		OperationStageUpdate,
		OperationRestartPanel:
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
)

type PromoteRequest struct {
	ArtifactIDs       []string `json:"artifactIds,omitempty"`
	RemoveArtifactIDs []string `json:"removeArtifactIds,omitempty"`
	RestoreBackupID   string   `json:"restoreBackupId,omitempty"`
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
}

type ServiceStatusRequest struct {
	Units []string `json:"units"`
}

type ServiceStatus struct {
	Unit        string `json:"unit"`
	LoadState   string `json:"loadState,omitempty"`
	ActiveState string `json:"activeState,omitempty"`
	SubState    string `json:"subState,omitempty"`
	Error       string `json:"error,omitempty"`
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
}

type BackupArchive struct {
	Name      string `json:"name"`
	Size      int64  `json:"size,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	Encrypted bool   `json:"encrypted,omitempty"`
}

type BackupResult struct {
	ArchiveName     string          `json:"archiveName,omitempty"`
	Archives        []BackupArchive `json:"archives,omitempty"`
	Verified        bool            `json:"verified,omitempty"`
	Restored        bool            `json:"restored,omitempty"`
	Pruned          []string        `json:"pruned,omitempty"`
	Kept            []string        `json:"kept,omitempty"`
	SafetyStatePath string          `json:"safetyStatePath,omitempty"`
	SafetyKeyPath   string          `json:"safetyKeyPath,omitempty"`
	Data            []byte          `json:"data,omitempty"`
}

type RotateKeyRequest struct{}

type FirewallRequest struct {
	RuleIDs []string `json:"ruleIds"`
}

type FirewallResult struct {
	AppliedRuleIDs []string `json:"appliedRuleIds,omitempty"`
}

type UpdateRequest struct {
	ArtifactID string `json:"artifactId"`
	Version    string `json:"version,omitempty"`
}

type UpdateResult struct {
	ArtifactID string `json:"artifactId"`
	Staged     bool   `json:"staged"`
	Installed  bool   `json:"installed,omitempty"`
	Version    string `json:"version,omitempty"`
}

type RestartPanelRequest struct{}

type RequestEnvelope struct {
	Version       int                   `json:"version"`
	RequestID     string                `json:"requestId"`
	Operation     Operation             `json:"operation"`
	Promote       *PromoteRequest       `json:"promote,omitempty"`
	ServiceAction *ServiceActionRequest `json:"serviceAction,omitempty"`
	ServiceStatus *ServiceStatusRequest `json:"serviceStatus,omitempty"`
	Journal       *JournalRequest       `json:"journal,omitempty"`
	Backup        *BackupRequest        `json:"backup,omitempty"`
	RotateKey     *RotateKeyRequest     `json:"rotateKey,omitempty"`
	Firewall      *FirewallRequest      `json:"firewall,omitempty"`
	Update        *UpdateRequest        `json:"update,omitempty"`
	RestartPanel  *RestartPanelRequest  `json:"restartPanel,omitempty"`
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
		r.Firewall != nil,
		r.Update != nil,
		r.RestartPanel != nil,
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
	case OperationBackupCreate, OperationBackupList, OperationBackupVerify, OperationBackupRead, OperationBackupPrune, OperationBackupRestore:
		return r.Backup != nil
	case OperationRotateKey:
		return r.RotateKey != nil
	case OperationFirewallApply:
		return r.Firewall != nil
	case OperationStageUpdate:
		return r.Update != nil
	case OperationRestartPanel:
		return r.RestartPanel != nil
	default:
		return false
	}
}
