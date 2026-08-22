package model

type Settings struct {
	PanelListen              string `json:"panelListen"`
	PanelAccess              string `json:"panelAccess,omitempty"`
	WebBasePath              string `json:"webBasePath,omitempty"`
	Mode                     string `json:"mode"`
	Domain                   string `json:"domain,omitempty"`
	Email                    string `json:"email,omitempty"`
	NaiveUsername            string `json:"naiveUsername,omitempty"`
	NaivePassword            string `json:"naivePassword,omitempty"`
	Hysteria2Password        string `json:"hysteria2Password,omitempty"`
	Hysteria2Insecure        bool   `json:"hysteria2Insecure,omitempty"`
	MasqueradeURL            string `json:"masqueradeURL,omitempty"`
	FallbackRoot             string `json:"fallbackRoot,omitempty"`
	OlcrtcAuth               string `json:"olcrtcAuth,omitempty"`
	OlcrtcTransport          string `json:"olcrtcTransport,omitempty"`
	OlcrtcRoomID             string `json:"olcrtcRoomID,omitempty"`
	PanelDomain              string `json:"panelDomain,omitempty"`
	PanelEmail               string `json:"panelEmail,omitempty"`
	PanelPublicPort          int    `json:"panelPublicPort,omitempty"`
	DefaultAcmeEmail         string `json:"defaultAcmeEmail,omitempty"`
	DefaultInboundPublicPort int    `json:"defaultInboundPublicPort,omitempty"`
	AcmeChallengeMode        string `json:"acmeChallengeMode,omitempty"`
	// ProtocolFields holds protocol-specific settings populated by the dynamic
	// Panel UI. Legacy flat fields above are still supported for backward
	// compatibility and are migrated into ProtocolFields on load.
	ProtocolFields map[string]any `json:"protocolFields,omitempty"`
	// FirewallManagement controls whether Veil syncs UFW rules during apply.
	// A nil pointer means "enabled" for backward compatibility with states created
	// before this field existed.
	FirewallManagement *bool `json:"firewallManagement,omitempty"`
}

type ClientProfile struct {
	Name     string `json:"name"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// RuntimeCredential carries per-client credential material resolved from the
// normalized Client+Binding+Credential store for a single inbound. It is a
// runtime-only carrier: it is never persisted or serialized (json:"-"), and is
// merged into the access model at render time so normalized clients reach the
// live config alongside legacy inbound-embedded profiles.
type RuntimeCredential struct {
	Name     string `json:"-"`
	Username string `json:"-"`
	Password string `json:"-"`
}

type Inbound struct {
	Name              string          `json:"name"`
	Protocol          string          `json:"protocol"`
	Transport         string          `json:"transport"`
	Port              int             `json:"port"`
	Enabled           bool            `json:"enabled"`
	Password          string          `json:"password,omitempty"`
	Profiles          []ClientProfile `json:"profiles,omitempty"`
	NaiveUsername     string          `json:"naiveUsername,omitempty"`
	NaivePassword     string          `json:"naivePassword,omitempty"`
	Hysteria2Password string          `json:"hysteria2Password,omitempty"`
	Hysteria2Insecure bool            `json:"hysteria2Insecure,omitempty"`
	MasqueradeURL     string          `json:"masqueradeURL,omitempty"`
	FallbackRoot      string          `json:"fallbackRoot,omitempty"`
	OlcrtcAuth        string          `json:"olcrtcAuth,omitempty"`
	OlcrtcTransport   string          `json:"olcrtcTransport,omitempty"`
	OlcrtcRoomID      string          `json:"olcrtcRoomID,omitempty"`
	// ProtocolFields holds protocol-specific inbound fields populated by the
	// dynamic Panel UI. Legacy flat fields above remain for backward compatibility.
	ProtocolFields map[string]any `json:"protocolFields,omitempty"`

	// RuntimeCredentials carries per-client credentials resolved from the
	// normalized client store for this inbound at render time. Runtime-only;
	// never persisted or serialized. The access model merges these so normalized
	// clients are rendered into the live config.
	RuntimeCredentials []RuntimeCredential `json:"-"`
}

type RoutingRule struct {
	Name     string `json:"name"`
	Match    string `json:"match"`
	Outbound string `json:"outbound"`
	Enabled  bool   `json:"enabled"`
}

type RoutingPreset struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Source      RoutingSource `json:"source"`
	Rules       []RoutingRule `json:"rules"`
}

type RoutingPresetResponse struct {
	ActivePreset string          `json:"activePreset,omitempty"`
	Source       RoutingSource   `json:"source"`
	Rules        []RoutingRule   `json:"rules"`
	Presets      []RoutingPreset `json:"presets,omitempty"`
}

type RoutingSource struct {
	Repository string              `json:"repository,omitempty"`
	Files      []RoutingSourceFile `json:"files,omitempty"`
}

type RoutingSourceFile struct {
	Name                  string `json:"name"`
	URL                   string `json:"url"`
	SHA256URL             string `json:"sha256Url,omitempty"`
	PinnedSHA256          string `json:"pinnedSha256,omitempty"`
	SignatureURL          string `json:"signatureUrl,omitempty"`
	CertificateIdentity   string `json:"certificateIdentity,omitempty"`
	CertificateOIDCIssuer string `json:"certificateOidcIssuer,omitempty"`
}

type WarpConfig struct {
	Enabled       bool   `json:"enabled"`
	LicenseKey    string `json:"licenseKey,omitempty"`
	Endpoint      string `json:"endpoint"`
	PrivateKey    string `json:"privateKey,omitempty"`
	LocalAddress  string `json:"localAddress,omitempty"`
	PeerPublicKey string `json:"peerPublicKey,omitempty"`
	Reserved      []int  `json:"reserved,omitempty"`
	SocksListen   string `json:"socksListen,omitempty"`
	SocksPort     int    `json:"socksPort,omitempty"`
	MTU           int    `json:"mtu,omitempty"`
}

type ClientLinksResponse struct {
	SchemaVersion              string           `json:"schemaVersion"`
	Domain                     string           `json:"domain"`
	SubscriptionURL            string           `json:"subscriptionUrl"`
	Base64SubscriptionURL      string           `json:"base64SubscriptionUrl"`
	RawSubscriptionURL         string           `json:"rawSubscriptionUrl"`
	DefaultSubscriptionFormat  string           `json:"defaultSubscriptionFormat"`
	Base64SubscriptionFilename string           `json:"base64SubscriptionFilename"`
	RawSubscriptionFilename    string           `json:"rawSubscriptionFilename"`
	SubscriptionContentType    string           `json:"subscriptionContentType"`
	SubscriptionFormats        []string         `json:"subscriptionFormats"`
	Count                      int              `json:"count"`
	Links                      []ClientLink     `json:"links"`
	Artifacts                  []ClientArtifact `json:"artifacts,omitempty"`
}

type ClientLink struct {
	Name      string `json:"name"`
	Protocol  string `json:"protocol"`
	Transport string `json:"transport"`
	Port      int    `json:"port"`
	URI       string `json:"uri"`
	Config    string `json:"config,omitempty"`
}

type ClientArtifact struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Kind     string `json:"kind"`
	Filename string `json:"filename"`
	Content  string `json:"content"`
}

type ValidationIssue struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	Field       string `json:"field,omitempty"`
	InboundID   string `json:"inboundId,omitempty"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
	Source      string `json:"source"`
}

type ApplyOperation struct {
	Type              string `json:"type"`
	Source            string `json:"source,omitempty"`
	Destination       string `json:"destination,omitempty"`
	Unit              string `json:"unit,omitempty"`
	InterruptionRisk  string `json:"interruptionRisk"`
	RollbackAvailable bool   `json:"rollbackAvailable"`
	ValidationSource  string `json:"validationSource"`
}

type ApplyPlanResponse struct {
	Valid      bool              `json:"valid"`
	Errors     []string          `json:"errors,omitempty"`
	Configs    []string          `json:"configs"`
	Actions    []string          `json:"actions"`
	Runtimes   []string          `json:"runtimes,omitempty"`
	Issues     []ValidationIssue `json:"issues"`
	Operations []ApplyOperation  `json:"operations"`
}

type ApplyRequest struct {
	Confirm       bool `json:"confirm"`
	ApplyLive     bool `json:"applyLive"`
	ApplyServices bool `json:"applyServices"`
}

type ApplyResponse struct {
	Applied         bool                     `json:"applied"`
	LiveApplied     bool                     `json:"liveApplied"`
	ServicesApplied bool                     `json:"servicesApplied"`
	RolledBack      bool                     `json:"rolledBack,omitempty"`
	Plan            ApplyPlanResponse        `json:"plan"`
	WrittenFiles    []string                 `json:"writtenFiles"`
	LiveFiles       []string                 `json:"liveFiles,omitempty"`
	BackupFiles     []string                 `json:"backupFiles,omitempty"`
	RollbackFiles   []string                 `json:"rollbackFiles,omitempty"`
	Validations     []ConfigValidationResult `json:"validations,omitempty"`
	ServiceActions  []ServiceActionResult    `json:"serviceActions,omitempty"`
	HealthChecks    []ServiceHealthResult    `json:"healthChecks,omitempty"`
	RollbackActions []ServiceActionResult    `json:"rollbackActions,omitempty"`

	// Runtime mutation evidence is intentionally not serialized in the public
	// response. The durable Runner consumes it to decide whether finalization or
	// recovery is safe; HTTP status and response flags are not convergence proof.
	MutationStarted        bool `json:"mutationStarted,omitempty"`
	ArtifactsChanged       bool `json:"artifactsChanged,omitempty"`
	ServicesChanged        bool `json:"servicesChanged,omitempty"`
	FirewallChanged        bool `json:"firewallChanged,omitempty"`
	ArtifactsRestored      bool `json:"artifactsRestored,omitempty"`
	ServicesRestored       bool `json:"servicesRestored,omitempty"`
	FirewallRestored       bool `json:"firewallRestored,omitempty"`
	PostRollbackHealthPass bool `json:"postRollbackHealthPass,omitempty"`
	RollbackComplete       bool `json:"rollbackComplete,omitempty"`
	Ambiguous              bool `json:"ambiguous,omitempty"`
}

type ApplyHistoryEntry struct {
	ID              string                   `json:"id"`
	Timestamp       string                   `json:"timestamp"`
	Stage           string                   `json:"stage"`
	Success         bool                     `json:"success"`
	Applied         bool                     `json:"applied"`
	LiveApplied     bool                     `json:"liveApplied"`
	ServicesApplied bool                     `json:"servicesApplied"`
	RolledBack      bool                     `json:"rolledBack,omitempty"`
	Plan            ApplyPlanResponse        `json:"plan"`
	WrittenFiles    []string                 `json:"writtenFiles,omitempty"`
	LiveFiles       []string                 `json:"liveFiles,omitempty"`
	BackupFiles     []string                 `json:"backupFiles,omitempty"`
	RollbackFiles   []string                 `json:"rollbackFiles,omitempty"`
	Validations     []ConfigValidationResult `json:"validations,omitempty"`
	ServiceActions  []ServiceActionResult    `json:"serviceActions,omitempty"`
	HealthChecks    []ServiceHealthResult    `json:"healthChecks,omitempty"`
	RollbackActions []ServiceActionResult    `json:"rollbackActions,omitempty"`
}

type ConfigValidationResult struct {
	Name    string   `json:"name"`
	Config  string   `json:"config"`
	Command []string `json:"command"`
	Valid   bool     `json:"valid"`
	Skipped bool     `json:"skipped,omitempty"`
	Output  string   `json:"output,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type ServiceActionResult struct {
	Name    string   `json:"name"`
	Command []string `json:"command"`
	Success bool     `json:"success"`
	Output  string   `json:"output,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type ServiceHealthResult struct {
	Name    string   `json:"name"`
	Command []string `json:"command"`
	Healthy bool     `json:"healthy"`
	Output  string   `json:"output,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type User struct {
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
	Role         string `json:"role"` // "admin" or "viewer"
	Locale       string `json:"locale,omitempty"`
}

type SetupState struct {
	Completed   bool   `json:"completed"`
	CompletedAt string `json:"completedAt,omitempty"`
}

type ManagementSnapshot struct {
	SchemaVersion int `json:"schemaVersion,omitempty"`
	// EffectiveAt is the deterministic policy-evaluation time for this
	// immutable revision. Replays must not substitute the current wall clock.
	EffectiveAt   int64         `json:"effectiveAt"`
	Setup         SetupState    `json:"setup"`
	Settings      Settings      `json:"settings"`
	Inbounds      []Inbound     `json:"inbounds"`
	Rules         []RoutingRule `json:"routingRules"`
	RoutingPreset string        `json:"routingPreset,omitempty"`
	RoutingSource RoutingSource `json:"routingSource,omitempty"`
	Warp          WarpConfig    `json:"warp"`
	Users         []User        `json:"users,omitempty"`
	// A3: normalized client state that affects runtime rendering. Snapshot
	// must freeze Clients, Bindings, and active credential references so an
	// apply job for revision N renders exactly the configuration committed as
	// revision N, never newer mutable state.
	Clients     []ClientSnapshot     `json:"clients,omitempty"`
	Bindings    []BindingSnapshot    `json:"bindings,omitempty"`
	Credentials []CredentialSnapshot `json:"credentials,omitempty"`
}

// ClientSnapshot is the immutable per-revision view of a normalized client.
type ClientSnapshot struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Email            *string `json:"email,omitempty"`
	Enabled          bool    `json:"enabled"`
	GroupID          *string `json:"groupId,omitempty"`
	QuotaBytes       *int64  `json:"quotaBytes,omitempty"`
	QuotaResetPolicy string  `json:"quotaResetPolicy"`
	QuotaResetAt     *int64  `json:"quotaResetAt,omitempty"`
	ExpiresAt        *int64  `json:"expiresAt,omitempty"`
	DeviceLimit      *int    `json:"deviceLimit,omitempty"`
	Notes            string  `json:"notes,omitempty"`
	Depleted         bool    `json:"depleted"`
	CreatedAt        int64   `json:"createdAt,omitempty"`
	UpdatedAt        int64   `json:"updatedAt,omitempty"`
	Version          int     `json:"version"`
}

// BindingSnapshot is the immutable per-revision view of a client->inbound
// binding, including enabled state and protocol settings.
type BindingSnapshot struct {
	ID               string `json:"id"`
	ClientID         string `json:"clientId"`
	InboundID        string `json:"inboundId"`
	RuntimeIdentity  string `json:"runtimeIdentity"`
	Enabled          bool   `json:"enabled"`
	ProtocolSettings string `json:"protocolSettings,omitempty"`
	CreatedAt        int64  `json:"createdAt,omitempty"`
	UpdatedAt        int64  `json:"updatedAt,omitempty"`
	Version          int    `json:"version"`
}

// CredentialSnapshot is the immutable per-revision reference to the active
// credential for a binding. It stores the encrypted material (never plaintext)
// so a retry of revision N renders with exactly the credential that was active
// at revision N, even if a newer revision rotated it.
type CredentialSnapshot struct {
	ID                string `json:"id"`
	BindingID         string `json:"bindingId"`
	Kind              string `json:"kind"`
	EncryptedValue    []byte `json:"encryptedValue"`
	KeyVersion        int    `json:"keyVersion"`
	CredentialVersion int    `json:"credentialVersion"`
	CreatedAt         int64  `json:"createdAt,omitempty"`
	RotatedAt         *int64 `json:"rotatedAt,omitempty"`
}
