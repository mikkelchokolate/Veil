# Localization And Accessibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship persisted English/Russian Panel localization plus keyboard, dialog, and 360 CSS-pixel accessibility verification.

**Architecture:** Store an optional `locale` on each Panel user and expose a CSRF-protected self-service locale endpoint. Resolve the initial locale as user preference, `veil_locale` cookie, `Accept-Language`, then English. A shared browser runtime owns stable translation keys, translates static and dynamically inserted DOM content, persists selection, and provides dialog focus management without changing API error messages.

**Tech Stack:** Go 1.25, embedded HTML/CSS/JavaScript, `net/http`, existing Management state codec, Go unit/integration tests, Playwright through the in-app Browser.

---

### Task 1: Persist User Locale And Resolve Requests

**Files:**
- Create: `internal/panel/locale.go`
- Create: `internal/panel/locale_test.go`
- Modify: `internal/model/types.go`
- Modify: `internal/managementstate/mutation.go`
- Modify: `internal/managementstate/mutation_test.go`
- Modify: `internal/api/auth_session.go`
- Modify: `internal/api/auth_session_test.go`
- Modify: `internal/api/management.go`
- Modify: `internal/api/router_security.go`
- Modify: `internal/api/router_security_test.go`
- Modify: `internal/api/setup_routes.go`
- Modify: `internal/api/setup_routes_test.go`

- [ ] **Step 1: Write failing locale resolver tests**

Cover normalization to `en`/`ru` and precedence:

```go
func TestResolveLocalePrecedence(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "veil_locale", Value: "en"})
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	if got := ResolveLocale("ru", req); got != "ru" {
		t.Fatalf("locale=%q", got)
	}
}
```

- [ ] **Step 2: Run resolver tests and verify RED**

Run: `go test ./internal/panel -run Locale -count=1`

Expected: FAIL because `ResolveLocale` does not exist.

- [ ] **Step 3: Implement locale normalization and request precedence**

`NormalizeLocale` accepts `ru`, `ru-*`, `en`, and `en-*`; all other values
become `en`. `ResolveLocale` checks the stored preference, cookie, and weighted
header in that order.

- [ ] **Step 4: Write failing persistence and self-service endpoint tests**

Tests must prove:

- locale survives state encode/decode;
- create/update accepts only `en` or `ru`;
- `POST /api/auth/locale` updates the current user's preference and cookie;
- viewer sessions may update only their own locale with CSRF;
- static API tokens cannot mutate a named user's preference through this route.

- [ ] **Step 5: Run API and state tests and verify RED**

Run: `go test ./internal/managementstate ./internal/api -run 'Locale|SelfService' -count=1`

Expected: FAIL on missing user field, route, and viewer exception.

- [ ] **Step 6: Implement persisted locale and endpoint**

Add `Locale string 'json:"locale,omitempty"'` to `model.User`. Preserve the
existing locale when user updates omit it. Register `/api/auth/locale`, require
an authenticated cookie session plus CSRF, update only the session username,
set a `veil_locale` SameSite cookie, return `{locale:"en|ru"}`, and record
`auth.locale.update` without revoking sessions.

- [ ] **Step 7: Include locale in setup, login, auth status, and user responses**

Setup stores the selected locale on the first admin. Login and auth status
return it. Admin user create/update/list payloads include it while passwords
remain excluded.

- [ ] **Step 8: Run focused and package tests**

Run: `go test ./internal/panel ./internal/managementstate ./internal/api -count=1`

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/panel/locale.go internal/panel/locale_test.go internal/model/types.go internal/managementstate internal/api
git commit -m "feat: persist panel locale preferences"
```

### Task 2: Add Shared English And Russian Catalogs

**Files:**
- Create: `internal/panel/localization_runtime.go`
- Create: `internal/panel/localization_runtime_test.go`
- Modify: `internal/panel/renderer.go`
- Modify: `internal/panel/renderer_test.go`
- Modify: `internal/panel/login_views.go`
- Modify: `internal/panel/setup_views.go`
- Modify: `internal/panel/login_views_test.go`
- Modify: `internal/panel/setup_views_test.go`
- Modify: `internal/api/panel_routes.go`
- Modify: `internal/api/panel_rendering_shell_test.go`

- [ ] **Step 1: Write failing catalog and rendering tests**

Require stable keys for shell navigation, setup, login, users, backups,
validation, apply preview, diagnostics, actions, placeholders, modal labels,
and dynamic status phrases. Assert Russian and English values are non-empty and
that rendered pages contain `lang="ru"`, a locale selector, and the shared
runtime.

- [ ] **Step 2: Run rendering tests and verify RED**

Run: `go test ./internal/panel ./internal/api -run 'Localization|Locale|Renderer|Login|Setup' -count=1`

Expected: FAIL because catalogs and locale-aware render signatures are absent.

- [ ] **Step 3: Implement the shared catalog runtime**

Expose:

```go
func LocalizationRuntimeJS() string
func SupportedLocales() []string
```

The JavaScript must provide `veilT(key, values)`, translate exact catalogued
text nodes and `placeholder`, `title`, and `aria-label` attributes, observe
dynamically inserted content, update `document.documentElement.lang`, and keep
API/server error bodies unchanged.

- [ ] **Step 4: Make all three Panel pages locale-aware**

Add `locale` to `Renderer.HTML`, `LoginHTML`, and `SetupHTML`. Insert a compact
English/Russian selector on the main top bar and authentication/setup pages.
Use the server-resolved locale before first paint.

- [ ] **Step 5: Persist locale selection**

Unauthenticated login/setup selection writes `veil_locale` and reloads. The
authenticated Panel calls `/api/auth/locale` with CSRF, updates the cookie, and
reloads. Login copies the returned user preference into the cookie before
opening the Panel; setup sends the selected locale with completion.

- [ ] **Step 6: Translate variable UI phrases through stable keys**

Replace concatenated operator-facing phrases such as edit-user titles,
confirmation prompts, self-revoke labels, apply warning summaries, and copy
status with `veilT`. Exact API errors and raw JSON remain English.

- [ ] **Step 7: Run Panel/API tests**

Run: `go test ./internal/panel ./internal/api -count=1`

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add internal/panel internal/api
git commit -m "feat: localize panel in English and Russian"
```

### Task 3: Complete Keyboard And Mobile Accessibility

**Files:**
- Create: `internal/panel/accessibility_test.go`
- Modify: `internal/panel/renderer.go`
- Modify: `internal/panel/login_views.go`
- Modify: `internal/panel/setup_views.go`
- Modify: `internal/panel/panel_client_links_card.go`
- Modify: `internal/panel/panel_inbound_form.go`
- Modify: `internal/panel/panel_routing_card.go`
- Modify: `internal/panel/panel_utility_actions.go`

- [ ] **Step 1: Write failing semantic accessibility tests**

Assert:

- a skip link targets `main`;
- navigation has an accessible label;
- logout is a real button;
- `:focus-visible` and `prefers-reduced-motion` rules exist;
- every modal has `role="dialog"`, `aria-modal="true"`, a labelled title, and
  an accessible close button;
- async status regions use `aria-live`;
- mobile CSS has bounded modal width, one-column forms, scrollable tables, and
  no fixed minimum viewport width.

- [ ] **Step 2: Run accessibility tests and verify RED**

Run: `go test ./internal/panel -run Accessibility -count=1`

Expected: FAIL on missing semantic and dialog behavior.

- [ ] **Step 3: Implement shell semantics and focus styling**

Add a visible-on-focus skip link, `main-content`, nav label, `aria-current`,
global focus ring, reduced-motion handling, minimum 44px touch targets on
mobile, and a keyboard-operable logout button.

- [ ] **Step 4: Implement shared modal focus management**

When an overlay becomes active, set `aria-hidden=false`, remember the previous
focus, focus the first control, trap Tab within the dialog, close on Escape,
and restore focus. Inactive overlays use `aria-hidden=true`.

- [ ] **Step 5: Tighten 360px layout**

At 760px and 420px, keep selectors and status text wrapped, inputs at
`min-width:0`, actions in one column, modals within the viewport, and wide
tables horizontally scrollable without expanding the page.

- [ ] **Step 6: Run Panel tests**

Run: `go test ./internal/panel -count=1`

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/panel
git commit -m "fix: improve panel keyboard and mobile access"
```

### Task 4: Update Contract, Documentation, And Browser Verification

**Files:**
- Modify: `docs/openapi.yaml`
- Modify: `sdk/go/veilclient.gen.go`
- Modify: `sdk/go/contract_test.go`
- Modify: `README.md`
- Modify: `ROADMAP.md`
- Modify: `CHANGELOG.md`
- Modify: `docs/operations.md`

- [ ] **Step 1: Extend the OpenAPI contract**

Document `/api/auth/locale`, locale fields on setup/login/status/user
payloads, valid `en`/`ru` enums, CSRF/session requirements, examples, and the
viewer self-service exception.

- [ ] **Step 2: Regenerate and test the SDK**

Run:

```bash
go generate ./sdk/go
go test ./sdk/go ./internal/api -run 'Generated|OpenAPI' -count=1
npx --yes @redocly/cli@1.25.15 lint docs/openapi.yaml
```

Expected: PASS; the known OpenAPI 3.1 generator compatibility warning is
allowed.

- [ ] **Step 3: Start a temporary local Panel**

Use temporary state/key/apply directories and an unused loopback port. Complete
setup through the browser, then sign in.

- [ ] **Step 4: Verify desktop and 360px mobile flows**

Using the in-app Browser:

- switch English → Russian → English and reload;
- verify stored user preference wins over a conflicting cookie;
- navigate every tab with keyboard;
- open and close each modal with Enter, Tab, Shift+Tab, and Escape;
- confirm no horizontal page overflow at 360x800;
- verify tables scroll inside their containers;
- verify setup, login, users, validation, apply preview, backups, and locale
  controls remain visible and non-overlapping.

- [ ] **Step 5: Update release documentation**

Move localization/accessibility/mobile verification from roadmap to completed
v0.6.0 work. Document locale precedence and the self-service endpoint.

- [ ] **Step 6: Run full verification**

Run:

```bash
gofmt -w internal
go test ./... -count=1
go vet ./...
go generate ./sdk/go
git diff --exit-code -- sdk/go/veilclient.gen.go
npx --yes @redocly/cli@1.25.15 lint docs/openapi.yaml
git diff --check
```

Expected: every command exits zero.

- [ ] **Step 7: Commit**

```bash
git add docs/openapi.yaml sdk README.md ROADMAP.md CHANGELOG.md docs/operations.md
git commit -m "docs: complete panel localization release notes"
```
