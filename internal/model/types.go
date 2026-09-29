package model

import (
	"net/netip"
	"strings"
)

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

	// CredentialDerivationSecret is a per-install secret injected into settings
	// at state-load / snapshot-build time (derived from the management-state
	// encryption key via secrets.DeriveToken). RevokedClientCredential mixes it
	// into sentinel credentials so a revoked inbound's rendered password stays
	// unguessable even when every credential field is empty (issue #1098).
	// Runtime-only: never serialized to state, never accepted from API input —
	// settings mutations preserve the current value. Empty means the render
	// context has no per-install secret available.
	CredentialDerivationSecret string `json:"-"`
}

// CredentialDerivationLabel is the domain label passed to secrets.DeriveToken
// when producing Settings.CredentialDerivationSecret. Keep it stable:
// changing it changes every sentinel credential on every install.
const CredentialDerivationLabel = "veil-revoked-credential-v1"

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
	// DeviceLimit/IPLimit mirror the owning client's connection limits so a
	// renderer can tell whether ANY admitted runtime identity needs
	// protocol-level enforcement (Hysteria2 switches auth to the HTTP
	// callback only when at least one credential is limited). Runtime-only.
	DeviceLimit *int `json:"-"`
	IPLimit     *int `json:"-"`
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

	// LegacyProfilesSuppressed is set by the render pipeline when one or more
	// embedded Profiles were filtered out because they were migrated into the
	// normalized client domain (#1117). It preserves the "profiles exist"
	// signal — all-disabled profile sets must never revive the inbound-level
	// fallback credential — while keeping the migrated profiles themselves out
	// of rendered configs and exported links. Runtime-only; never persisted.
	LegacyProfilesSuppressed bool `json:"-"`

	// HasClientBindings reports that the normalized Client+Binding+Credential
	// store has at least one binding (enabled or not) for this inbound. Once a
	// binding exists the inbound is credential-managed: renderers and link
	// builders must treat "zero usable credentials" as revoked — never as a
	// reason to revive the legacy inbound fallback password — so disabling,
	// expiring, depleting or deleting the last normalized client fails closed
	// (issue #1098). Runtime-only; set by the management layer next to
	// RuntimeCredentials and never persisted.
	HasClientBindings bool `json:"-"`
}

// HadClientProfiles reports whether the inbound carries (or carried, before
// migration suppression) embedded client profiles, or has normalized
// bindings. Renderers/validators use it wherever "profiles exist" gates the
// inbound-level credential fallback.
func (in Inbound) HadClientProfiles() bool {
	return len(in.Profiles) > 0 || in.LegacyProfilesSuppressed || in.HasClientBindings
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

const (
	// WarpSocksListenDefault is the canonical bind/dial address of the local
	// WARP SOCKS listener: an address inside WarpSocksEgressBand.
	WarpSocksListenDefault = "127.41.0.1"
	// WarpSocksEgressBand is the reserved loopback band the protocol-unit
	// systemd egress filters pierce for the WARP SOCKS listener. It is the
	// ONLY loopback range veil-hysteria2@ and veil-olcrtc@ can reach
	// (#1097), so socksListen is contractually restricted to it: any other
	// loopback validates fine as a bind but is unreachable for the daemons
	// that must dial it (#1160). Kept in sync with the renderer's
	// egressAllowWarpSocksBand — the validation contract and the ACL pierce
	// must never drift apart.
	WarpSocksEgressBand = "127.41.0.0/16"
)

var warpSocksEgressPrefix = netip.MustParsePrefix(WarpSocksEgressBand)

// WarpSocksListenInBand reports whether listen is an IPv4 literal inside
// WarpSocksEgressBand — the only socksListen values the protocol units can
// reach under their IPAddressAllow egress filters (#1160).
func WarpSocksListenInBand(listen string) bool {
	addr, err := netip.ParseAddr(strings.TrimSpace(listen))
	if err != nil || !addr.Is4() || !warpSocksEgressPrefix.Contains(addr) {
		return false
	}
	// Exclude the band's network/broadcast-looking endpoints: unusable as a
	// bind target in practice and confusing to advertise as valid.
	return !warpSocksBandEndpoint(addr)
}

// warpSocksBandEndpoint reports whether addr is one of the band's .0/.255
// endpoints. Callers pass an already-IPv4 address.
func warpSocksBandEndpoint(addr netip.Addr) bool {
	o := addr.As4()
	return (o[2] == 0 && o[3] == 0) || (o[2] == 255 && o[3] == 255)
}

// NormalizeWarpSocksListen rewrites values that cannot function as the WARP
// SOCKS bind/dial address to WarpSocksListenDefault: unset, plus ANY
// loopback literal whose effective address sits outside the egress-pierced
// band (the pre-#1097 default 127.0.0.1, 127.0.0.5, ::1, ::ffff:127.0.0.5, ...)
// — preserved, they silently dead-end WARP upstream reachability (#1097,
// #1160). Everything else is returned untouched: non-canonical in-band
// encodings (e.g. ::ffff:127.41.0.1) and non-loopback/non-IP garbage are left
// for warp.Validate to reject loudly, never silently rewritten into a stored
// value. Whitespace is trimmed before parsing so the dial/bind view can never
// disagree with warp.Validate, which TrimSpace's the same input.
func NormalizeWarpSocksListen(listen string) string {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		return WarpSocksListenDefault
	}
	addr, err := netip.ParseAddr(listen)
	if err != nil {
		return listen
	}
	if unmapped := addr.Unmap(); unmapped.IsLoopback() &&
		(!warpSocksEgressPrefix.Contains(unmapped) || (addr.Is4() && warpSocksBandEndpoint(unmapped))) {
		return WarpSocksListenDefault
	}
	return listen
}

// SocksDialAddr is the address protocol upstreams dial to reach the local
// WARP SOCKS listener. It defaults to 127.41.0.1 — the reserved loopback band
// the per-unit egress filter allow-lists for protocol daemons — so renderers
// never diverge from the configured sing-box bind (#576, #1097). Out-of-band
// loopback values normalize to the band default so dial-side readers that
// bypass SetDefaults never emit a loopback the egress filter denies (#1160);
// warp.Validate restricts SocksListen to the band, so this is always a safe
// reachable dial target.
func (c WarpConfig) SocksDialAddr() string {
	return NormalizeWarpSocksListen(c.SocksListen)
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
	ID              string `json:"id"`
	Timestamp       string `json:"timestamp"`
	Stage           string `json:"stage"`
	Success         bool   `json:"success"`
	Applied         bool   `json:"applied"`
	LiveApplied     bool   `json:"liveApplied"`
	ServicesApplied bool   `json:"servicesApplied"`
	RolledBack      bool   `json:"rolledBack,omitempty"`
	// Ambiguous marks entries whose runtime outcome could not be proven —
	// they carry stage "ambiguous" and are never labeled live/services (#968).
	Ambiguous       bool                     `json:"ambiguous,omitempty"`
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
	// TOTP second factor (issue #1172). TOTPSecret is the active shared
	// secret, TOTPPendingSecret is the enroll-time secret that has not been
	// confirmed by a first valid code yet — both are AES-256-GCM encrypted at
	// rest by the managementstate SecretPolicy. TOTPRecoveryHashes holds
	// SHA-256 hashes of the single-use recovery codes (never the codes
	// themselves).
	TOTPEnabled        bool     `json:"totpEnabled,omitempty"`
	TOTPSecret         string   `json:"totpSecret,omitempty"`
	TOTPPendingSecret  string   `json:"totpPendingSecret,omitempty"`
	TOTPRecoveryHashes []string `json:"totpRecoveryHashes,omitempty"`
}

// HasUsableTOTPSecret reports whether the user carries second-factor material
// that still needs to survive a role/password update: an enabled factor, an
// in-flight enrollment, or unused recovery codes.
func (u User) HasUsableTOTPSecret() bool {
	return u.TOTPEnabled || u.TOTPSecret != "" || u.TOTPPendingSecret != "" || len(u.TOTPRecoveryHashes) > 0
}

// PreserveTOTP copies the second-factor fields from prior so generic user
// mutations (role/password/locale through UpdateUser) never silently wipe an
// enrolled factor. The dedicated TOTP endpoints write these fields through
// SetUserTOTP instead.
func (u *User) PreserveTOTP(prior User) {
	u.TOTPEnabled = prior.TOTPEnabled
	u.TOTPSecret = prior.TOTPSecret
	u.TOTPPendingSecret = prior.TOTPPendingSecret
	u.TOTPRecoveryHashes = append([]string(nil), prior.TOTPRecoveryHashes...)
}

// ClearTOTP drops every second-factor field (admin reset / self-disable).
func (u *User) ClearTOTP() {
	u.TOTPEnabled = false
	u.TOTPSecret = ""
	u.TOTPPendingSecret = ""
	u.TOTPRecoveryHashes = nil
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
	IPLimit          *int    `json:"ipLimit,omitempty"`
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
