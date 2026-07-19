# Veil arch-rework: Gap Matrix (audit vs plan 2026-07-18)

Status legend: DONE / PARTIAL / MISSING / VIOLATION

## Backend (A1-A12)

### A1 OpenAPI sync
- Status: PARTIAL
- Current: docs/openapi.yaml exists, web/orval.config.ts reads it
- Missing: full route-vs-spec audit not confirmed; PUT-vs-PATCH, 200-vs-204, error format unification not verified
- Required: audit all apply/clients/traffic/s endpoints against spec; unify error format {error:{code,message,details}}
- Test: contract tests (A2)

### A2 Contract tests
- Status: PARTIAL
- Current: some api tests exist
- Missing: real-router integration tests for every stage 0-3 endpoint; SDK freshness check
- Required: replace hand-map tests; regenerate sdk/go/veilclient.gen.go
- Test: go test ./internal/api -run Contract

### A3 Immutable revisions — VIOLATION
- Current: internal/apply/snapshot.go stores ManagementSnapshot (Settings/Inbounds/Rules/Warp/Users only). managementstate/snapshot.go BuildSnapshot does NOT include Clients/Bindings/Credentials. pinStateToRevisionLocked returns nil (renders current state) when snapshot missing or decrypt fails — FORBIDDEN fallback.
- Missing: snapshot must include Clients, Bindings, enabled state, protocol settings, active credential ID/version, encrypted credential material or safe ref.
- Required: extend ManagementSnapshot + BuildSnapshot with normalized client state; remove fallback-to-current for tracked revisions; error instead.
- Test: regression revN(credA) vs revN+1(credB), retry revN proves runtime uses A.

### A4 Single client mutation transaction — VIOLATION
- Current: client.Service calls notify() after EVERY op (create/update/delete/bind/credential) → notifier triggers apply. Handler then calls applyAfterClientMutation → DOUBLE APPLY. Rollback via sequential public RemoveBinding/Delete (compensating delete).
- Missing: single SQL tx (BEGIN; save client+bindings+credentials+revision snapshot; COMMIT; one apply job; one envelope).
- Required: Repository.WithTx (added), refactor create/update client handlers to use it; remove per-op notifier apply; real ROLLBACK.
- Test: create client w/ 2 bindings, induce failure on 2nd credential → assert no client persisted, no apply job for partial state.

### A5 Transactional create — VIOLATION (same root as A4)
- Current: handleV1CreateClient creates client then loops AddBinding/SetCredential with compensating Delete on failure.
- Required: all-or-nothing via WithTx; no 201 if any binding/credential fails.

### A6 Normalized clients in renderer + auto legacy migration — VIOLATION
- Current: clientaccess.BuildClientAccess reads legacy inbound.Profiles. No auto-migration on upgrade; only manual /clients/migrate-legacy button.
- Missing: renderer consumes normalized Client+Binding+active Credential; auto upgrade flow (backup, marker/version, idempotent, verify, read-only legacy).
- Required: wire clientRepo into runtime render path; implement Migrator.AutoRun with backup+verify.
- Test: post-migration, runtime renders from normalized store; legacy profiles read-only.

### A7 Client read model
- Status: PARTIAL
- Current: client_v1_routes.go has GET /{id} with bindings
- Missing: verify bindings[] include inboundName, protocol, enabled, version, credential{configured,kind,version,rotatedAt}, capabilities{}, traffic{}, apply{}
- Test: GET /api/v1/clients/{id} returns full read model, no creds.

### A8 List/bulk
- Status: PARTIAL
- Current: handleV1ListClients has page/pageSize/search/status/inboundId/groupId/quotaState/expiresBefore/expiresAfter/sort
- Missing: aggregate summary over whole set; bulk actions enable/disable/delete/extend_expiry/set_quota/reset_traffic/attach_inbound/detach_inbound returning succeeded/skipped/failed/revision/applyJob
- Test: bulk partial result returns per-item outcome.

### A9 Traffic real — VIOLATION
- Current: trafficCollector = NewCollector(store, 0, nil) — ZERO providers registered. CollectOnce is no-op.
- Missing: real protocol TrafficProviders OR exact capability/unsupported state.
- Required: implement TrafficProvider for supported protocols (read runtime stats); if none, return honest unsupported.
- Test: collector with no provider → summary state=unsupported (already honest); with provider → real counters.

### A10 Realtime contract — MISSING
- Current: only /traffic/stream SSE
- Missing: unified GET /api/v1/events (apply.state, client.updated, client.status, traffic.delta/snapshot, telemetry.provider_state, subscription.revoked) with event IDs, heartbeat, reconnect, bounded queues.
- Required: implement or fall back to variant 1 with technical justification.
- Test: SSE stream delivers coalesced events with IDs.

### A11 Subscription contract
- Status: PARTIAL
- Current: subscription.go exists
- Missing: verify only base64+raw (no Clash/sing-box promises); endpoint metadata/capabilities; headers use real traffic totals; token metadata separates label vs createdBy
- Test: GET /api/v1/subscriptions/capabilities returns honest formats.

### A12 Gate DoD
- Status: NOT MET (A3/A4/A5/A6/A9/A10 violations)

## Frontend (B1-B13)

### B1 Orval types — VIOLATION
- Current: pages define local interfaces (ClientDetail, BindingView, ApplyJob, TrafficSummary, Settings, Inbound, RoutingRule, etc.) instead of Orval-generated types.
- Required: use generated types from src/api/generated/models; fix OpenAPI if types missing/incorrect, regenerate.
- Test: pnpm typecheck passes with generated types only.

### B2 Auth
- Status: DONE (login/setup/session, CSRF, RBAC, HTTP-only cookie)

### B3 SPA + WebBasePath
- Status: DONE (works at / and /<secret>/, SPA fallback, /s/{token} public)

### B4 Shell
- Status: DONE (all pages present, Apply status indicator)

### B5 Apply UI — PARTIAL
- Current: ApplyPage with state/drift/jobs
- Missing: /apply/jobs/$jobId route, operations, validation results, health checks, rollback details, retry, reconcile, apply plan, legacy history, persistent mismatch warning
- Test: navigate to job detail, see operations + retry button.

### B6 Clients — VIOLATION
- Current: manual <table>, not TanStack Table
- Missing: server pagination, typed URL filters, search debounce, protocol/inbound/group/quota/expiry filters, apply state, online state, server sorting, row selection, column visibility, responsive, detailed bulk result. Client details: edit, enable/disable, delete, attach/detach inbound, enable/disable binding, credential gen/replace/rotate, one-time secret dialog, subscriptions, traffic, audit.
- Test: TanStack Table with server-side pagination + filters; bulk action partial result dialog.

### B7 Credentials
- Status: PARTIAL (gen/replace/rotate exist; verify one-time dialog + no persistent reveal)

### B8 Subscriptions
- Status: DONE (token list/create/rotate/revoke, one-time dialog, QR, public page)

### B9 Traffic — VIOLATION
- Current: no Apache ECharts
- Missing: historical time series, supported ranges, upload/download, client/inbound/protocol breakdown, telemetry provider state, stale/failed states, client binding breakdown
- Test: ECharts renders with real telemetry; shows unsupported state when no provider.

### B10 Inbounds — PARTIAL
- Current: InboundsPage exists
- Missing: create/edit/delete/enable/disable, protocol-specific fields, validation+apply feedback, attach normalized clients, legacy profiles read-only
- Test: create inbound via UI, see apply feedback.

### B11 Legacy migration order
- Status: PARTIAL (SPA is main, legacy removed)

### B12 CSP
- Status: DONE (no unsafe-inline, no CDN)

### B13 Tests — VIOLATION
- Current: 2 vitest tests (clients, apply), no Playwright, no browser mode
- Missing: Playwright config + critical flows (setup, login/logout, admin/viewer, create client 2 bindings, tx failure, apply ok/failed+retry, mismatch, token create/rotate/revoke, public sub page, traffic supported/unsupported, bulk partial, optimistic-lock, SPA refresh, /s/{token} outside base, no creds in storage, legacy migration)
- Test: pnpm test:e2e runs Playwright flows.

### B-router — VIOLATION
- Current: code-based router, not file-based; no Zod search validation
- Required: file-based TanStack Router, Zod search schemas, lazy loading, error boundaries

### B-ui — VIOLATION
- Current: ad-hoc global classes (card, btn, data-table, input, badge)
- Required: shadcn/ui + Radix/Base UI + Tailwind + Lucide design system

### B-i18n — VIOLATION
- Current: English only
- Required: en + ru localization matching backend locales

### B-parity — VIOLATION
- Current: Inbounds/Routing/WARP/Backups/Users/Settings read-only
- Required: full mutations (create/edit/delete/enable/disable, routing rules, WARP config, backup create/download/verify/restore/prune, user CRUD+role+sessions, settings edit+key rotation+panel update)

## Out of scope findings
- (none yet)
