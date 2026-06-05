# Exposure Policy And First-Run Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make public exposure fail closed on TLS and metrics policy, and provide a single-use local first-run admin setup flow.

**Architecture:** Extend the existing serve security resolver with an explicit exposure policy evaluated before server construction. Persist setup metadata in Management state and register setup routes that are available only for loopback `local` mode with no users.

**Tech Stack:** Go 1.25, Cobra, `net/http`, bcrypt, existing encrypted Management state store, Go tests.

---

## File Structure

- `internal/cliflow/serve/exposure_policy.go`: pure exposure decision logic.
- `internal/cliflow/serve/exposure_policy_test.go`: policy matrix.
- `internal/cliflow/serve/security.go`: resolve TLS before validating exposure.
- `internal/cli/serve.go`: unsafe override flag and resolved exposure output.
- `internal/model/types.go`: setup metadata in the persisted snapshot.
- `internal/managementstate/defaults.go`: backward-compatible setup defaults.
- `internal/managementstate/snapshot.go`: setup snapshot wiring.
- `internal/api/setup_routes.go`: setup status and completion endpoints.
- `internal/api/setup_routes_test.go`: setup route behavior.
- `internal/api/management_types.go`: setup state and server exposure metadata.
- `internal/api/management_state_lifecycle.go`: load/save setup metadata.
- `internal/api/router_composition.go`: setup route registration and auth bypass.
- `internal/api/router_security.go`: narrowly allow setup routes.
- `internal/panel/setup_views.go`: first-run setup page.
- `internal/api/panel_routes.go`: render setup before login/dashboard.
- `ROADMAP.md`, `README.md`, `docs/HARDENING.md`, `docs/openapi.yaml`: operator contract.

### Task 1: Refresh Roadmap

- [ ] **Step 1: Rewrite the completed capability section**

Remove Caddy multi-instance, QR/export, user/session screen, viewer role, token
rotation helper, and safe apply preview from future milestones. Add a
`Completed in v0.5.0` section and list only remaining work in release order.

- [ ] **Step 2: Verify documentation consistency**

Run:

```powershell
rg -n "Caddy multi-instance|real-time UI/API port|setup wizard|i18n|backup retention" ROADMAP.md README.md docs CONTEXT.md
```

Expected: completed features appear only in historical/completed text, and
remaining features use consistent wording.

- [ ] **Step 3: Commit**

```powershell
git add ROADMAP.md README.md docs CONTEXT.md
git commit -m "docs: refresh roadmap after v0.5.0"
```

### Task 2: Add Exposure Policy

- [ ] **Step 1: Write the failing policy matrix**

Create `internal/cliflow/serve/exposure_policy_test.go`:

```go
func TestExposurePolicyRejectsPublicHTTP(t *testing.T) {
    err := NewExposurePolicy().Validate(ExposureInput{
        PanelAccess: "direct", PublicListen: true, TokenConfigured: true,
        SessionAuthConfigured: true, MetricsAuthRequired: true,
    })
    if err == nil || !strings.Contains(err.Error(), "TLS") {
        t.Fatalf("expected TLS refusal, got %v", err)
    }
}

func TestExposurePolicyTreatsCaddyAsPublic(t *testing.T) {
    err := NewExposurePolicy().Validate(ExposureInput{
        PanelAccess: "caddy", SessionAuthConfigured: false,
        MetricsAuthRequired: false, ProxyTLS: true,
    })
    if err == nil {
        t.Fatal("expected Caddy exposure without auth to fail")
    }
}

func TestExposurePolicyAllowsLocalFirstRun(t *testing.T) {
    err := NewExposurePolicy().Validate(ExposureInput{PanelAccess: "local"})
    if err != nil {
        t.Fatalf("local first run: %v", err)
    }
}
```

- [ ] **Step 2: Run the test and confirm RED**

Run:

```powershell
go test ./internal/cliflow/serve -run ExposurePolicy -count=1
```

Expected: compile failure because `NewExposurePolicy` and `ExposureInput` do not
exist.

- [ ] **Step 3: Implement the pure policy**

Create `internal/cliflow/serve/exposure_policy.go` with:

```go
type ExposureInput struct {
    PanelAccess          string
    PublicListen         bool
    TokenConfigured      bool
    SessionAuthConfigured bool
    MetricsAuthRequired  bool
    NativeTLS            bool
    ProxyTLS             bool
    AllowUnsafePublicHTTP bool
}

type ExposurePolicy struct{}

func NewExposurePolicy() ExposurePolicy { return ExposurePolicy{} }

func (ExposurePolicy) Validate(in ExposureInput) error {
    exposed := in.PublicListen || in.PanelAccess == "caddy" || in.PanelAccess == "direct"
    if !exposed {
        return nil
    }
    if !in.TokenConfigured || !in.SessionAuthConfigured {
        return fmt.Errorf("public Panel exposure requires API token and user/session auth")
    }
    if !in.MetricsAuthRequired {
        return fmt.Errorf("public Panel exposure requires authenticated metrics")
    }
    if !in.NativeTLS && !in.ProxyTLS && !in.AllowUnsafePublicHTTP {
        return fmt.Errorf("public Panel exposure requires TLS; unsafe HTTP requires an explicit override")
    }
    return nil
}
```

- [ ] **Step 4: Integrate with `Security.Resolve`**

Resolve TLS and auto-TLS before calling the policy. Add
`AllowUnsafePublicHTTP` to `SecurityOptions` and `Config`. Treat
`PanelAccess == "caddy"` as proxy TLS and as public for metrics policy.

- [ ] **Step 5: Add CLI override**

Add `--unsafe-allow-public-http` and `VEIL_UNSAFE_ALLOW_PUBLIC_HTTP=1`. Pass it
through `serveWorkflowOptions`; remove the warning-only behavior because public
HTTP is now refused unless the override is explicit.

- [ ] **Step 6: Verify GREEN**

Run:

```powershell
go test ./internal/cliflow/serve ./internal/cli -count=1
```

Expected: PASS.

- [ ] **Step 7: Commit**

```powershell
git add internal/cliflow/serve internal/cli/serve.go
git commit -m "feat: enforce public exposure tls policy"
```

### Task 3: Persist Setup Metadata

- [ ] **Step 1: Write the failing state round-trip test**

Add to `internal/managementstate/store_test.go`:

```go
func TestStoreRoundTripsSetupMetadata(t *testing.T) {
    path := filepath.Join(t.TempDir(), "state.json")
    store := NewStore(path, nil)
    want := model.SetupState{Completed: true, CompletedAt: "2026-06-05T12:00:00Z"}
    if err := store.Save(model.ManagementSnapshot{Setup: want}); err != nil {
        t.Fatal(err)
    }
    got, ok, err := store.Load()
    if err != nil || !ok || got.Setup != want {
        t.Fatalf("setup = %+v, ok=%v, err=%v", got.Setup, ok, err)
    }
}
```

- [ ] **Step 2: Run and confirm RED**

Run:

```powershell
go test ./internal/managementstate -run SetupMetadata -count=1
```

Expected: compile failure because `model.SetupState` and `Setup` do not exist.

- [ ] **Step 3: Add setup state**

Add:

```go
type SetupState struct {
    Completed   bool   `json:"completed"`
    CompletedAt string `json:"completedAt,omitempty"`
}
```

Wire it through `ManagementSnapshot`, snapshot inputs/targets, defaults, API
state, and lifecycle methods. During load, treat any snapshot containing an
admin user as completed even if old state has no setup field.

- [ ] **Step 4: Verify GREEN**

Run:

```powershell
go test ./internal/model ./internal/managementstate ./internal/api -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/model internal/managementstate internal/api
git commit -m "feat: persist panel setup state"
```

### Task 4: Add Local Setup API

- [ ] **Step 1: Write failing HTTP tests**

Create `internal/api/setup_routes_test.go` covering:

```go
func TestSetupCompleteCreatesFirstAdminOnLocalListener(t *testing.T) {
    state := newTestSetupState(t, ServerInfo{PanelAccess: "local", PanelListen: "127.0.0.1:2096"})
    req := httptest.NewRequest(http.MethodPost, "/api/setup/complete",
        strings.NewReader(`{"username":"admin","password":"a-long-secure-password"}`))
    req.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()
    state.handleSetupComplete(rec, req)
    if rec.Code != http.StatusCreated {
        t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
    }
}

func TestSetupCompleteRejectsCaddyAndPublicListeners(t *testing.T) {
    cases := []ServerInfo{
        {PanelAccess: "caddy", PanelListen: "127.0.0.1:2096"},
        {PanelAccess: "direct", PanelListen: "0.0.0.0:2096", PublicListen: true},
    }
    for _, info := range cases {
        state := newTestSetupState(t, info)
        req := httptest.NewRequest(http.MethodPost, "/api/setup/complete",
            strings.NewReader(`{"username":"admin","password":"a-long-secure-password"}`))
        req.Header.Set("Content-Type", "application/json")
        rec := httptest.NewRecorder()
        state.handleSetupComplete(rec, req)
        if rec.Code != http.StatusForbidden {
            t.Fatalf("info=%+v status=%d body=%s", info, rec.Code, rec.Body.String())
        }
    }
}

func TestSetupCompleteIsSingleUse(t *testing.T) {
    state := newTestSetupState(t, ServerInfo{PanelAccess: "local", PanelListen: "127.0.0.1:2096"})
    body := `{"username":"admin","password":"a-long-secure-password"}`
    first := httptest.NewRequest(http.MethodPost, "/api/setup/complete", strings.NewReader(body))
    first.Header.Set("Content-Type", "application/json")
    firstRec := httptest.NewRecorder()
    state.handleSetupComplete(firstRec, first)
    second := httptest.NewRequest(http.MethodPost, "/api/setup/complete", strings.NewReader(body))
    second.Header.Set("Content-Type", "application/json")
    secondRec := httptest.NewRecorder()
    state.handleSetupComplete(secondRec, second)
    if firstRec.Code != http.StatusCreated || secondRec.Code != http.StatusConflict {
        t.Fatalf("first=%d second=%d", firstRec.Code, secondRec.Code)
    }
}
```

- [ ] **Step 2: Run and confirm RED**

Run:

```powershell
go test ./internal/api -run Setup -count=1
```

Expected: compile failure because setup handlers do not exist.

- [ ] **Step 3: Implement setup routes**

Add `GET /api/setup/status` and `POST /api/setup/complete`. Completion validates
username and password length, hashes with bcrypt, updates users/setup under one
lock, and calls `SaveLocked`. On save failure restore the previous in-memory
state before returning 500.

- [ ] **Step 4: Restrict auth bypass**

Allow unauthenticated setup routes only when `SetupAllowed` is true in
`ServerInfo`. All other unauthenticated API routes keep their current policy.

- [ ] **Step 5: Verify GREEN**

Run:

```powershell
go test ./internal/api -run "Setup|AuthMiddleware" -count=1
```

Expected: PASS.

- [ ] **Step 6: Commit**

```powershell
git add internal/api
git commit -m "feat: add local first-run setup api"
```

### Task 5: Add Setup UI

- [ ] **Step 1: Write failing rendering tests**

Create `internal/panel/setup_views_test.go` asserting that setup HTML contains
username/password fields, exposure guidance, backup acknowledgement, accessible
labels, and no external scripts.

Add an API rendering test asserting the setup page is rendered before the
dashboard when setup is allowed and no users exist.

- [ ] **Step 2: Run and confirm RED**

Run:

```powershell
go test ./internal/panel ./internal/api -run Setup -count=1
```

Expected: compile/test failure because `SetupHTML` is absent.

- [ ] **Step 3: Implement `SetupHTML`**

Render a compact four-step form: admin credentials, exposure explanation,
backup acknowledgement, and completion. Submit JSON to
`/api/setup/complete`, then redirect to the login page.

- [ ] **Step 4: Verify GREEN**

Run:

```powershell
go test ./internal/panel ./internal/api -run Setup -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit**

```powershell
git add internal/panel internal/api/panel_routes.go
git commit -m "feat: add first-run setup interface"
```

### Task 6: Document And Verify The Contract

- [ ] **Step 1: Update operator docs and OpenAPI**

Document startup refusal, unsafe override, Caddy-as-public metrics behavior,
setup endpoints, schemas, examples, and migration from pre-v0.6 state.

- [ ] **Step 2: Run complete phase verification**

Run:

```powershell
gofmt -w internal/cliflow/serve internal/cli internal/model internal/managementstate internal/api internal/panel
go test ./...
make openapi-lint
git diff --check
```

Expected: all commands exit 0.

- [ ] **Step 3: Commit**

```powershell
git add README.md ROADMAP.md docs internal
git commit -m "docs: document secure first-run exposure"
```
