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
	"github.com/mikkelchokolate/Veil/internal/model"
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
	mu                             sync.Mutex
	statePath                      string
	applyRoot                      string
	liveRoot                       string
	keyPath                        string
	cipher                         *secrets.Cipher
	authToken                      string
	allowDevAnonymous              bool
	startupStateLoadFailed         bool
	setupAllowed                   bool
	setup                          SetupState
	settings                       Settings
	inbounds                       []Inbound
	rules                          []RoutingRule
	routingPreset                  string
	routingSource                  RoutingSource
	warp                           WarpConfig
	users                          []User
	orphanedUnits                  []string
	sessions                       *SessionRegistry
	audit                          *audit.Recorder
	version                        string
	backupDir                      string
	backupPassphrasePath           string
	backupMutationMu               sync.Mutex
	backupJobsMu                   sync.Mutex
	backupJobs                     map[string]BackupRestoreJob
	backupRestoreAudit             func(audit.Record) error
	backupRestoreOwnerSessionGrace time.Duration
	serviceActionMu                sync.Mutex
	updateMu                       sync.Mutex
	updateStager                   func(context.Context) (string, error)
	configurationValidator         ConfigurationValidator
	enforceConfigurationValidation bool
	privileged                     privileged.Client
	privilegedLocal                bool

	// Architecture rework (durable apply + normalized store). db is nil when no
	// StatePath is configured; the apply subsystem and revision/job tracking are
	// then disabled and handlers fall back to legacy behavior.
	db                *sql.DB
	applyRevisions    *apply.RevisionStore
	applyJobs         *apply.JobStore
	applySnapshots    *apply.SnapshotStore
	applyRunner       *apply.Runner
	clientService     *client.Service
	clientRepo        *client.Repository
	clientCreds       *client.CredentialStore
	clientMigrator    *client.Migrator
	tokenStore        *client.TokenStore
	subRenderer       *client.SubscriptionRenderer
	trafficStore      *client.TrafficStore
	trafficCollector  *client.Collector
	trafficReconciler *client.Reconciler
	// A3: normalized client state pinned from the immutable revision snapshot
	// for the duration of an apply render. When non-nil these override live
	// SQLite state so a retry of revision N renders exactly revision N.
	renderClients     []model.ClientSnapshot
	renderBindings    []model.BindingSnapshot
	renderCredentials []model.CredentialSnapshot
}
