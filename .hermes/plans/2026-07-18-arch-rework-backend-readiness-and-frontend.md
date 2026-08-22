# Veil arch-rework: Backend readiness gate + new React frontend

Goal: (A) bring the desired/applied-revision backend + normalized-clients API to a
trustworthy contract that UI can be built on; (B) build and integrate a new React SPA.

Branch: feat/arch-rework (repo /root/projects/Veil, origin github.com/mikkelchokolate/Veil)

Method: TDD (RED-GREEN-REFACTOR). No production code without a failing test first.
Go test gate: NEVER `make test` (e2e hangs). Use:
  go test $(go list ./... | grep -v 'test/e2e')
  go test -race <pkgs>; go vet <pkgs>
internal/api tests are slow (~6 min) — timeout >=400s or targeted -run.

Do not record live panel hosts, listen addresses, or WebBasePath in this repo.
Deploy atomically: cp bin/veil /usr/local/bin/.veil.new && mv over /usr/local/bin/veil,
restart veil.service veil-helper.socket veil-helper.service. Commit+push periodically.

Sources of truth (NOT stale .md): Go handlers, router registration, domain services,
SQLite migrations, runtime renderer, OpenAPI, generated SDK, tests.

## PART A — backend readiness gate (must pass before Orval/UI pages)

A1 Sync OpenAPI with real router: full route-vs-spec audit of apply/clients/traffic/s
   endpoints. Fix PUT-vs-PATCH client update, 200-vs-204 client delete, limit-vs-bucket
   history param, missing apply routes, missing binding/credential/bulk routes, GET
   endpoints wrongly marked CSRF, HEAD /s/{token}, real envelopes, real error content
   types. Pick ONE API error format {error:{code,message,details}}; if legacy endpoints
   can't be unified safely, spec must honestly describe both + fetcher normalizes.
A2 Contract tests: replace hand-map test with real-router integration tests (methods,
   status, content-type, auth/CSRF, SDK freshness). Cover every stage 0-3 endpoint.
   Then regenerate sdk/go/veilclient.gen.go.
A3 Immutable revisions: content snapshot per desired revision (settings, inbounds,
   routing, warp, clients, bindings, active credential versions, protocol binding
   settings); no plaintext creds. Apply job for revision N renders exactly N (not
   newer). Retry re-applies N. Regression test: rev41 vs rev42.
A4 Client mutations -> single op: tx save + immutable revision + apply job; return
   Client + {success,revision,applyJob}. success=false => desired saved, runtime NOT
   updated. No void notifier.
A5 Transactional create: Client+Bindings+Credentials+revision snapshot all-or-nothing.
   No 201 if a binding/credential failed. Partial creation only via explicit bulk API.
A6 Wire normalized clients into runtime renderer (Client+Binding+active Credential+
   Inbound+plugin). Legacy Inbound.Profiles must not drive runtime post-migration.
   Auto-run legacy migrator on upgrade: backup, idempotent, marker/version, verify
   before dropping legacy, read-only compat repr if needed. Don't delete legacy
   profiles before backup+migration verification.
A7 Client read model for UI: GET /{id} returns bindings[] w/ inboundName, protocol,
   enabled, version, credential{configured,kind,version,rotatedAt}, capabilities{},
   traffic{}, apply{}. No encrypted/plaintext creds. Real endpoints: GET/POST bindings,
   PATCH/DELETE binding, POST rotate-credential (server-generated via capability).
   No reveal endpoint.
A8 List/bulk: server pagination + filters (page,pageSize,search,status,inboundId,
   protocol,groupId,quotaState,expiresBefore,expiresAfter,sort); aggregate summary
   over whole set; bulk actions enable/disable/delete/extend_expiry/set_quota/
   reset_traffic/attach_inbound/detach_inbound returning succeeded/skipped/failed/
   revision/applyJob. Drop misleading reset_quota that only clears depleted flag.
A9 Traffic real: register real protocol TrafficProviders, start Collector+Reconciler,
   Stop on shutdown, provider health, last collection/error, online semantics or
   honest unsupported, reset endpoint, binding breakdown, aggregate summary, history
   resolution, retention. No fake zeros; no N+1 in /traffic/top; no silent 1000 cap.
A10 Realtime contract: choose variant 2 — unified GET /api/v1/events (apply.state,
   client.updated, client.status, traffic.delta/snapshot, telemetry.provider_state,
   subscription.revoked) with event IDs, heartbeat, reconnect, bounded queues, slow-
   consumer handling, no secrets, no 10k snapshot every 5s, coalesce traffic. Else
   fall back to variant 1 (keep /traffic/stream only + polling).
A11 Subscription contract: only base64+raw (no Clash/sing-box until real); endpoint
   metadata/capabilities so UI doesn't hardcode; headers use real traffic totals;
   token metadata separates label vs createdBy (not one column); preview only via same
   renderer; public HTML page stays server-rendered (no admin bundle).
A12 Gate DoD: spec==routes, SDK updated, immutable rev snapshot, client mutations bump
   desired rev + return apply outcome, tx create, renderer uses normalized clients,
   legacy migration runs, client detail has bindings+capabilities, traffic real or
   honest unsupported, realtime defined, go test green.

## PART B — new React frontend (only after gate)

Stack: React, TS strict, Vite, TanStack Router/Query/Table, RHF, Zod, Orval,
shadcn/ui (Base UI/Radix), Tailwind, Lucide, Apache ECharts, native EventSource,
Vitest + Browser Mode + MSW + Playwright, Biome, pnpm. NO Redux/Axios/Next/MUI/AntD/AG Grid.

B1 Orval from verified OpenAPI: chain Go API -> OpenAPI -> Orval -> TS DTO -> fetch ->
   RQ hooks -> Zod -> MSW. Never hand-edit generated files.
B2 Auth: /api/auth/{login,status,logout}; CSRF from status in central fetcher; HTTP-only
   veil_session; credentials same-origin; X-CSRF-Token only for cookie mutations; no
   admin tokens in localStorage; session expiry handling; admin/viewer RBAC; setup flow.
B3 SPA + WebBasePath: works at / and /<secret>/; correct Vite base, asset URLs, direct
   nav, refresh, SPA fallback, API not index.html, hashed asset caching, index.html
   no-cache, /s/{token} stays public root route outside panel base.
B4 Shell: Overview, Clients, Inbounds, Routing, Traffic, WARP, System, Backups, Users,
   Settings; global Apply Status on all authenticated pages.
B5 Apply UI: honest synchronous semantics (desired saved / runtime apply ok|failed,
   runtime remains rev N); no fake queue/202; Apply state indicator, jobs list, job
   details, Retry, Reconcile, Apply plan, legacy history.
B6 Clients: /clients, /clients/new, /clients/$id; table w/ server pagination, URL
   filters, sorting, bulk select, aggregate summary, effective status, bindings,
   traffic, quota, expiry, last online, apply state; form General/Limits/Access/Review;
   details Overview/Access/Subscription/Traffic/Audit (no Sessions tab).
B7 Credentials: Generate/Replace/Rotate + one-time copy; no persistent Reveal; never
   cache creds.
B8 Subscriptions: only real formats (Base64/Raw); token list/create/rotate/revoke,
   one-time plaintext dialog, copy public URL, QR via local endpoint, public page. No
   Clash/sing-box promises.
B9 Traffic: dashboard over honest telemetry states (zero/unsupported/unavailable/
   stale/failed/collecting); periods match backend aggregation; no fake 90d graph.
B10 Inbounds: after backend migration, remove editable legacy profiles from form; show
    attached normalized clients; attach/create/detach; legacy profiles read-only for
    compat; never save client via two APIs.
B11 Legacy migration order: SPA shell -> auth/setup -> apply -> clients -> subscriptions
    -> traffic -> inbounds -> rest -> parity tests -> switch new SPA as main -> delete
    giant inline Go HTML/CSS/JS -> tighten CSP. Legacy route allowed until parity.
B12 CSP: remove unsafe-inline + google fonts CDN; compiled assets; system/local fonts;
    same-origin; no external CDN.
B13 Tests: pnpm typecheck/check/test/test:browser/build/test:e2e; go test, -race, vet.
    Playwright: setup, login/logout, admin/viewer, create client 2 bindings, tx failure,
    apply ok, apply failed+retry, desired/applied mismatch, token create/rotate/revoke,
    public sub page, traffic supported/unsupported, bulk partial, optimistic-lock
    conflict, SPA refresh under WebBasePath, /s/{token} outside base, no creds/tokens
    in storage, legacy profile migration.

Final DoD: gate passed, spec==handlers, Orval clean-gen, immutable revs, client
mutations return rev/apply, renderer uses normalized clients, UI via real APIs, apply
failures shown honestly, clients/bindings/subscriptions managed in new UI, traffic UI
honest, SPA WebBasePath works, /s/{token} intact, legacy no longer main post-parity,
all frontend+Go tests green.

Final report must list: backend gaps fixed pre-UI, OpenAPI divergences found, endpoints
changed, frontend pages implemented, what's still legacy, real test commands run,
bundle sizes, known limitations.
