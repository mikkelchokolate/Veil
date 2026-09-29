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
	OperationBackupSftp         Operation = "backup_sftp"
	OperationIssueIPCert        Operation = "issue_ip_cert"
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
		OperationCaddyLoad,
		OperationBackupSftp,
		OperationIssueIPCert:
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
	UnitFileState          string `json:"unitFileState,omitempty"`
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
	// RemoteUpload carries the verified SFTP receipt when a create also
	// pushed the archive to a configured remote destination.
	RemoteUpload *BackupRemoteUpload `json:"remoteUpload,omitempty"`
	// RemotePruned/RemoteKept report the mirrored remote retention run after
	// a prune when an SFTP destination is configured.
	RemotePruned []string `json:"remotePruned,omitempty"`
	RemoteKept   []string `json:"remoteKept,omitempty"`
	// RemoteError makes a remote-destination failure loud without failing
	// the local backup operation it accompanied.
	RemoteError string `json:"remoteError,omitempty"`
}

// BackupRemoteUpload is the receipt for one verified remote upload.
type BackupRemoteUpload struct {
	Archive string `json:"archive"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

// BackupSftpAction selects one SFTP-destination operation.
type BackupSftpAction string

const (
	BackupSftpActionGet    BackupSftpAction = "get"
	BackupSftpActionSet    BackupSftpAction = "set"
	BackupSftpActionDelete BackupSftpAction = "delete"
	BackupSftpActionList   BackupSftpAction = "list"
	BackupSftpActionFetch  BackupSftpAction = "fetch"
)

// BackupSftpConfig is the destination payload for a set operation. The
// secret fields are pointer-typed write-only knobs: nil keeps the stored
// value, a non-nil value replaces it (empty clears). They are never echoed
// back — reads return the *Set flags on BackupSftpDestination instead.
type BackupSftpConfig struct {
	Enabled       bool    `json:"enabled"`
	Host          string  `json:"host"`
	Port          int     `json:"port,omitempty"`
	User          string  `json:"user"`
	RemoteDir     string  `json:"remoteDir"`
	AuthType      string  `json:"authType"`
	KeyPath       string  `json:"keyPath,omitempty"`
	KeyPassphrase *string `json:"keyPassphrase,omitempty"`
	Password      *string `json:"password,omitempty"`
	HostKey       *string `json:"hostKey,omitempty"`
}

type BackupSftpRequest struct {
	Action      BackupSftpAction  `json:"action"`
	ArchiveName string            `json:"archiveName,omitempty"`
	Config      *BackupSftpConfig `json:"config,omitempty"`
	Fence       FenceToken        `json:"fence"`
}

// BackupSftpDestination is the secret-free API view of the stored config.
type BackupSftpDestination struct {
	Configured       bool   `json:"configured"`
	Enabled          bool   `json:"enabled"`
	Host             string `json:"host,omitempty"`
	Port             int    `json:"port,omitempty"`
	User             string `json:"user,omitempty"`
	RemoteDir        string `json:"remoteDir,omitempty"`
	AuthType         string `json:"authType,omitempty"`
	KeyPath          string `json:"keyPath,omitempty"`
	PasswordSet      bool   `json:"passwordSet,omitempty"`
	KeyPassphraseSet bool   `json:"keyPassphraseSet,omitempty"`
	HostKeySet       bool   `json:"hostKeySet,omitempty"`
}

// BackupSftpStatus reports the most recent remote-operation outcomes.
type BackupSftpStatus struct {
	LastUploadAt      string `json:"lastUploadAt,omitempty"`
	LastUploadArchive string `json:"lastUploadArchive,omitempty"`
	LastFetchAt       string `json:"lastFetchAt,omitempty"`
	LastFetchArchive  string `json:"lastFetchArchive,omitempty"`
	LastPruneAt       string `json:"lastPruneAt,omitempty"`
	LastError         string `json:"lastError,omitempty"`
	LastErrorAt       string `json:"lastErrorAt,omitempty"`
}

type BackupSftpResult struct {
	Destination *BackupSftpDestination `json:"destination,omitempty"`
	Status      *BackupSftpStatus      `json:"status,omitempty"`
	// Archives lists remote archives for the list action.
	Archives []BackupArchive `json:"archives,omitempty"`
	// Archive is the locally materialized entry after a fetch action.
	Archive *BackupArchive `json:"archive,omitempty"`
}

type RotateKeyRequest struct {
	Fence FenceToken `json:"fence"`
}

type RecoverKeyRotationRequest struct {
	Fence FenceToken `json:"fence"`
}

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
	// Changed reports whether the destination bytes were actually rewritten.
	// The periodic cert-sync worker restarts the serving runtime only on a
	// real change (#1103).
	Changed bool `json:"changed,omitempty"`
	// Fallback reports that the destination was seeded with a Veil-issued
	// self-signed certificate because Caddy storage had no ACME material yet
	// (e.g. a hysteria2-only domain whose http-01 port was busy at apply
	// time). The certificate is honest fallback material, never a trusted
	// issuance (#1168).
	Fallback bool `json:"fallback,omitempty"`
}

// IssueIPCertRequest asks the helper to issue/renew the short-lived Let's
// Encrypt IP certificate that the panel (and the runtimes sharing it) serve
// in direct access mode (#1169/#1170). CertPath/KeyPath must sit inside the
// helper's allowed panel certificate directory; empty paths default to that
// directory's tls.crt/tls.key.
type IssueIPCertRequest struct {
	PublicIPv4 string `json:"publicIpv4,omitempty"`
	PublicIPv6 string `json:"publicIpv6,omitempty"`
	// HTTPPort is the HTTP-01 standalone listen port; 0 defaults to 80.
	HTTPPort int    `json:"httpPort,omitempty"`
	Email    string `json:"email,omitempty"`
	CertPath string `json:"certPath,omitempty"`
	KeyPath  string `json:"keyPath,omitempty"`
	// CAServer overrides the ACME directory (an acme.sh CA name like
	// "letsencrypt" or a controlled-CA URL); empty means Let's Encrypt.
	// Insecure skips TLS verification of the ACME endpoint — controlled test
	// CAs only.
	CAServer string `json:"caServer,omitempty"`
	Insecure bool   `json:"insecure,omitempty"`
	// CARoot is an optional PEM bundle acme.sh verifies the ACME directory
	// endpoint against (--ca-bundle) — the VEIL_ACME_CA_ROOT persisted for
	// controlled-CA installs (#1189).
	CARoot string `json:"caRoot,omitempty"`
	// HTTP01ViaCaddy reports that the managed Caddy edge owns the public
	// HTTP-01 port in the rendered plan. When the requested standalone port
	// is busy the helper parks acme.sh on the internal port that the
	// /.well-known/acme-challenge/ reverse-proxy route forwards to (#1181).
	HTTP01ViaCaddy bool `json:"http01ViaCaddy,omitempty"`
	// DeferPanelRestart moves the reloadcmd's `try-restart veil.service` onto
	// a transient systemd timer: the panel itself is the caller, so an inline
	// restart would kill it before this operation could answer.
	DeferPanelRestart bool       `json:"deferPanelRestart,omitempty"`
	Fence             FenceToken `json:"fence"`
}

type IssueIPCertResult struct {
	CertPath string `json:"certPath"`
	KeyPath  string `json:"keyPath"`
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
	BackupSftp         *BackupSftpRequest         `json:"backupSftp,omitempty"`
	IssueIPCert        *IssueIPCertRequest        `json:"issueIpCert,omitempty"`
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
		r.BackupSftp != nil,
		r.IssueIPCert != nil,
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
	case OperationBackupSftp:
		return r.BackupSftp != nil
	case OperationIssueIPCert:
		return r.IssueIPCert != nil
	default:
		return false
	}
}
