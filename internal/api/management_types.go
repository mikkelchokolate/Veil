package api

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/mikkelchokolate/Veil/internal/apply"
	"github.com/mikkelchokolate/Veil/internal/applyflow"
	"github.com/mikkelchokolate/Veil/internal/audit"
	"github.com/mikkelchokolate/Veil/internal/client"
	"github.com/mikkelchokolate/Veil/internal/managementstate"
	"github.com/mikkelchokolate/Veil/internal/model"
	"github.com/mikkelchokolate/Veil/internal/observability"
	"github.com/mikkelchokolate/Veil/internal/privileged"
	"github.com/mikkelchokolate/Veil/internal/secrets"
)

type Settings = model.Settings
type ClientProfile = model.ClientProfile
type RuntimeCredential = model.RuntimeCredential
type Inbound = model.Inbound
type RoutingRule = model.RoutingRule
type RoutingPreset = model.RoutingPreset
type RoutingPresetResponse = model.RoutingPresetResponse
type RoutingSource = model.RoutingSource
type RoutingSourceFile = model.RoutingSourceFile
type WarpConfig = model.WarpConfig
type ClientLinksResponse = model.ClientLinksResponse
type ClientLink = model.ClientLink
type ClientArtifact = model.ClientArtifact
type ApplyPlanResponse = model.ApplyPlanResponse
type ApplyRequest = model.ApplyRequest
type ApplyResponse = model.ApplyResponse
type ApplyHistoryEntry = model.ApplyHistoryEntry
type ConfigValidationResult = model.ConfigValidationResult
type ServiceActionResult = model.ServiceActionResult
type ServiceHealthResult = model.ServiceHealthResult

type User = model.User
type SetupState = model.SetupState

type livePromotionRecord = applyflow.PromotionRecord

type managementSnapshot = model.ManagementSnapshot

type managementState struct {
	mu                         sync.Mutex
	clientLifecycleMu          sync.RWMutex
	clientRequestMu            sync.RWMutex
	passwordHasher             PasswordHasher
	databaseOpener             func(string) (*sql.DB, error)
	lifecycleCtx               context.Context
	lifecycleCancel            context.CancelFunc
	statePath                  string
	applyRoot                  string
	liveRoot                   string
	systemdWantsDir            string
	keyPath                    string
	cipher                     *secrets.Cipher
	authToken                  string
	allowDevAnonymous          bool
	startupStateLoadFailed     bool
	startupStateLoadErr        error
	startupPrivilegedFailure   bool
	storageDegradedErr         error
	subscriptionLimiter        subscriptionRateLimiter
	appliedProjectionMu        sync.Mutex
	appliedProjectionRevision  uint64
	appliedProjections         map[string]managementSnapshot
	runtimeVerificationUnknown bool
	requirePrivilegedHelper    bool
	requireApplyTracking       bool
	setupAllowed               bool
	setup                      SetupState
	serveWebBasePath           string
	servePanelListen           string
	servePanelAccess           string
	// defaultInput reproduces the serve-time inputs BuildDefaultState used at
	// construction. Backup restore rewinds the mutable snapshot-managed fields
	// to these defaults so the post-restore reload replaces state instead of
	// merging over stale pre-restore values (#1053).
	defaultInput  managementstate.DefaultInput
	settings      Settings
	inbounds      []Inbound
	rules         []RoutingRule
	routingPreset string
	routingSource RoutingSource
	warp          WarpConfig
	users         []User
	// usersEverExisted is the "provisioned" latch: once a panel user exists
	// it stays set even if users later drop to zero via rollback/restore, so
	// loopback dev-anonymous admin and the NaivePassword fallback can never
	// be re-enabled on an already-configured instance (#1100). The matching
	// marker file beside state.json makes the latch durable across restarts.
	usersEverExisted              bool
	usersProvisionedMarkerWritten bool
	orphanedUnits                 []string
	// previousServiceStates captures each touched unit's "active|unitFileState"
	// before an apply mutates it, so a promotion rollback restores the exact
	// lifecycle state instead of unconditionally enable+starting units that
	// may have been stopped or disabled (#1135).
	previousServiceStates map[string]string
	sessions              *SessionRegistry
	loginUsernameLimiter  *observability.RateLimiterEngine
	// loginGlobalLimiter is the process-wide per-username login budget that
	// backs delayGlobalUsernameAttempt; per-(client,username) buckets alone
	// cannot stop a spray distributed across many IPv6 prefixes (#1101).
	loginGlobalLimiter *observability.RateLimiterEngine
	httpRateLimiter    *observability.RateLimiter
	idempotency        *idempotencyStore
	loginBackoff       map[string]loginBackoffState
	loginBackoffNow    func() time.Time
	audit              *audit.Recorder
	auditHealthMu      sync.RWMutex
	auditDegraded      bool
	// auditSpoolDurable is true when the last degraded append was durably
	// accepted by the critical spool, so /health can report an honest
	// audit_spool status instead of a blanket durability_unverified (#981).
	auditSpoolDurable    bool
	version              string
	backupDir            string
	backupPassphrasePath string
	// Mutations (create/prune/delete/restore) take the write lock; downloads
	// take the read lock so a concurrent mutation cannot remove or replace an
	// archive mid-transfer while parallel downloads remain possible (#963).
	backupMutationMu               sync.RWMutex
	backupJobsMu                   sync.Mutex
	backupJobs                     map[string]BackupRestoreJob
	backupJobsPath                 string
	backupRestoreAudit             func(audit.Record) error
	backupRestoreOwnerSessionGrace time.Duration
	serviceActionMu                sync.Mutex
	updateMu                       sync.Mutex
	updateWG                       sync.WaitGroup
	updateStager                   func(context.Context, bool) (string, error)
	configurationValidator         ConfigurationValidator
	enforceConfigurationValidation bool
	privileged                     privileged.Client
	privilegedLocal                bool
	// metrics is the collector served at /metrics. RouterComposition assigns it
	// after constructing the state; nil in bare test-constructed states.
	metrics *observability.MetricsCollector

	// Architecture rework (durable apply + normalized store). db is nil when no
	// StatePath is configured; the apply subsystem and revision/job tracking are
	// then disabled and handlers fall back to legacy behavior.
	db                   *sql.DB
	applyRevisions       *apply.RevisionStore
	applyJobs            *apply.JobStore
	applySnapshots       *apply.SnapshotStore
	applyRunner          *apply.Runner
	clientService        *client.Service
	clientRepo           *client.Repository
	clientCreds          *client.CredentialStore
	clientMigrator       *client.Migrator
	tokenStore           *client.TokenStore
	subRenderer          *client.SubscriptionRenderer
	trafficStore         *client.TrafficStore
	trafficCollector     *client.Collector
	trafficReconciler    *client.Reconciler
	expirationReconciler *expirationReconciler
	certSyncWorker       *certSyncWorker
	sse                  *sseBroadcaster
	// Internal Hysteria2 auth callback (#1173): the listener is started by
	// initClientSubsystem, survives restores (it only reads live state per
	// request), and is closed by Close. hy2AuthListenAddr overrides the
	// default bind address and hy2AuthOnline replaces the production
	// /online reader — both are test seams, empty/nil in production.
	hy2Auth           *hy2AuthServer
	hy2AuthListenAddr string
	hy2AuthOnline     func(ctx context.Context, settings model.Settings, inbound model.Inbound, identities map[string]string) (map[string]int64, []string, error)
	hy2IPTracker      *hy2IPTracker
	// hy2SessionTracker bridges the auth-ok → /online-registration gap so
	// concurrent admissions cannot race past deviceLimit (#1173).
	hy2SessionTracker       *hy2SessionTracker
	clientSubsystemStopping bool
	applyReadinessMu        sync.Mutex
	applyReadinessCache     clientApplyReadinessCache
	// A3: normalized client state pinned from the immutable revision snapshot
	// for the duration of an apply render. When non-nil these override live
	// SQLite state so a retry of revision N renders exactly revision N.
	renderClients     []model.ClientSnapshot
	renderBindings    []model.BindingSnapshot
	renderCredentials []model.CredentialSnapshot
	renderEffectiveAt int64
}
