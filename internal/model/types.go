package model

type Settings struct {
	PanelListen       string `json:"panelListen"`
	PanelAccess       string `json:"panelAccess,omitempty"`
	WebBasePath       string `json:"webBasePath,omitempty"`
	Mode              string `json:"mode"`
	Domain            string `json:"domain,omitempty"`
	Email             string `json:"email,omitempty"`
	NaiveUsername     string `json:"naiveUsername,omitempty"`
	NaivePassword     string `json:"naivePassword,omitempty"`
	Hysteria2Password string `json:"hysteria2Password,omitempty"`
	Hysteria2Insecure bool   `json:"hysteria2Insecure,omitempty"`
	MasqueradeURL     string `json:"masqueradeURL,omitempty"`
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
	Name      string `json:"name"`
	URL       string `json:"url"`
	SHA256URL string `json:"sha256Url,omitempty"`
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
	SchemaVersion int           `json:"schemaVersion,omitempty"`
	Setup         SetupState    `json:"setup"`
	Settings      Settings      `json:"settings"`
	Inbounds      []Inbound     `json:"inbounds"`
	Rules         []RoutingRule `json:"routingRules"`
	RoutingPreset string        `json:"routingPreset,omitempty"`
	RoutingSource RoutingSource `json:"routingSource,omitempty"`
	Warp          WarpConfig    `json:"warp"`
	Users         []User        `json:"users,omitempty"`
}
