# Inbound/Caddy Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace per-inbound Caddy instances and global Panel-coupled domains with a single `veil-caddy.service`, per-inbound naive/hysteria2 domains, explicit bind-key ownership, and transport-aware Caddy JSON rendering.

**Architecture:** Introduce a small `internal/bindregistry` for `(address, port, protocol)` ownership, an `internal/caddyassembly` render-plan builder, a `internal/renderer/caddyjson` JSON generator, and an `internal/caddyadmin` Admin API loader. Existing protocol packages keep their validator/renderer interfaces but migrate to per-inbound `ProtocolFields` and contribute bind keys to the global registry. Apply flow orchestrates Caddy JSON reload, hysteria2 cert sync, and rollback.

**Tech Stack:** Go 1.26.4, Caddy with `github.com/klzgrad/forwardproxy` naive module, systemd, Caddy Admin API (`POST /load`), text/template for debug Caddyfile artifacts.

## Global Constraints

- There is exactly one physical Caddy process: `veil-caddy.service`.
- Caddy authoritative runtime config is JSON loaded via Admin API; Caddyfile is only a debug/export artifact.
- Every public TCP/UDP listener must be represented in the global `BindKey` registry.
- Caddy automatic HTTP-to-HTTPS redirects are disabled unless explicitly modeled as bind owners.
- ACME issuer is configured for the selected `AcmeChallengeMode` only; no unplanned challenge fallback is allowed.
- naive inbound transport is one of `tcp`, `quic`, `dual`; `quic` is exposed only after a behavior-based `H3Only` probe.
- Domain-level ACME email: conflicting explicit emails for the same domain are rejected; fallback order is explicit → `DefaultAcmeEmail` → `PanelEmail` → error.
- Legacy naive inbounds are migrated all-at-once; new managed naive inbounds are blocked while unresolved legacy inbounds exist.
- Apply follows TDD: failing test → minimal implementation → passing test → commit for every task.

## File Structure

### New files

- `internal/bindregistry/bindkey.go` — `BindKey`, `BindOwner`, conflict detection, address normalization.
- `internal/bindregistry/conflicts.go` — `ValidateNoConflicts`, conflict reporting.
- `internal/caddyassembly/domain.go` — `CaddyDomainOwners`, `CaddyDomainCertSpec`, per-domain ACME email resolution.
- `internal/caddyassembly/challenge.go` — ACME challenge bind planning.
- `internal/caddyassembly/plan.go` — `CaddyRenderPlan`, `CaddyBindOwner`, `AcmeChallengeOwner`, plan builder.
- `internal/caddycapabilities/capabilities.go` — Probe Caddy binary for forward_proxy / HTTP3 / H3Only.
- `internal/renderer/caddyjson.go` — Generate Caddy JSON config from `CaddyRenderPlan`.
- `internal/caddyadmin/client.go` — Admin API `POST /load` with previous-config backup.
- `internal/inbounds/legacy_migration.go` — Legacy state detection and migration helpers.

### Modified files

- `internal/model/types.go` — Add `PanelPublicPort`, `DefaultAcmeEmail`, `DefaultInboundPublicPort`, `AcmeChallengeMode`; keep legacy `Domain`/`Email` for backward compat.
- `internal/settings/settings_validation.go` — Validate new settings fields, Panel caddy mode public port.
- `internal/protocols/naiveproxy/plugin.go` — Typed readers for `domain`, `email`, `publicPort`, `transport`, `fallbackRoot`.
- `internal/protocols/naiveproxy/validator.go` — Bind-key conflict checks, transport validation.
- `internal/protocols/naiveproxy/renderer.go` — Migrate to `CaddyRenderPlan` contribution; remove per-inbound Caddyfile rendering.
- `internal/protocols/naiveproxy/client_access.go` — Export `https://` / `quic://` URIs based on transport.
- `internal/protocols/naiveproxy/ui.go` — Dynamic form schema with new fields.
- `internal/protocols/hysteria2/plugin.go` — Typed reader for inbound `domain`.
- `internal/protocols/hysteria2/renderer.go` — Use inbound domain for cert path.
- `internal/protocols/naiveproxy/runtime.go` — Return single `veil-caddy.service` runtime descriptor.
- `internal/renderer/systemd.go` — Remove `veil-caddy@.service` template rendering; emit single `veil-caddy.service`.
- `internal/generatedconfig/inbound_renderer.go` — Remove per-inbound Caddyfile rendering; keep hysteria2 YAML.
- `internal/generatedconfig/artifact_catalog.go` — Add Caddy JSON artifact spec.
- `internal/caddycert/caddycert.go` — Poll Caddy storage, atomic copy to `/etc/veil/certs/{domain}.*`.
- `internal/api/apply_plan_builder.go` — Build `CaddyRenderPlan`, plan ACME binds, validate conflicts.
- `internal/api/management_apply_context.go` — Load JSON via Admin API, trigger hysteria2 cert sync, rollback on failure.
- `internal/panelaccess/profile.go` — Use `PanelPublicPort` for Panel Caddy bind.

### Tests

- Each new package gets `*_test.go` covering every exported function.
- Existing tests for `naiveproxy`, `hysteria2`, `settings`, `panelaccess`, `apply_plan_builder`, `management_apply_context`, and `applyflow` are updated or replaced to match the new model.
- End-to-end tests in `test/e2e/management_flow_test.go` are extended with a naive inbound create/delete scenario.

---

## Task 1: Bind-key model and address normalization

**Files:**
- Create: `internal/bindregistry/bindkey.go`
- Test: `internal/bindregistry/bindkey_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  ```go
  type ListenNetwork string
  type BindKey struct{ Address string; Port int; Network ListenNetwork }
  type BindOwnerKind string
  type BindOwner struct{ Kind BindOwnerKind; ServiceName string; InboundName string }
  func NormalizeAddress(addr string) string
  func IsWildcard(addr string) bool
  func (k BindKey) Canonical() BindKey
  func (k BindKey) Overlaps(other BindKey) bool
  ```

- [ ] **Step 1: Write the failing test**

```go
package bindregistry

import "testing"

func TestNormalizeAddress(t *testing.T) {
    cases := []struct {
        in, want string
    }{
        {"0.0.0.0", "0.0.0.0"},
        {"", "0.0.0.0"},
        {"::", "::"},
        {" 0.0.0.0 ", "0.0.0.0"},
    }
    for _, c := range cases {
        got := NormalizeAddress(c.in)
        if got != c.want {
            t.Errorf("NormalizeAddress(%q) = %q, want %q", c.in, got, c.want)
        }
    }
}

func TestBindKeyOverlap(t *testing.T) {
    wildcardTCP443 := BindKey{Address: "0.0.0.0", Port: 443, Network: ListenTCP}
    specificTCP443 := BindKey{Address: "192.168.1.10", Port: 443, Network: ListenTCP}
    if !wildcardTCP443.Overlaps(specificTCP443) {
        t.Error("wildcard IPv4 must overlap specific IPv4 on same port/protocol")
    }
    udp443 := BindKey{Address: "0.0.0.0", Port: 443, Network: ListenUDP}
    if wildcardTCP443.Overlaps(udp443) {
        t.Error("TCP and UDP on same port must not conflict")
    }
    otherPort := BindKey{Address: "192.168.1.10", Port: 8443, Network: ListenTCP}
    if specificTCP443.Overlaps(otherPort) {
        t.Error("different ports must not conflict")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/bindregistry -run 'TestNormalizeAddress|TestBindKeyOverlap' -v
```

Expected: FAIL — `bindkey.go` does not exist.

- [ ] **Step 3: Write minimal implementation**

```go
package bindregistry

import "strings"

type ListenNetwork string

const (
    ListenTCP ListenNetwork = "tcp"
    ListenUDP ListenNetwork = "udp"
)

type BindKey struct {
    Address string
    Port    int
    Network ListenNetwork
}

type BindOwnerKind string

const (
    BindOwnerPanelDirect   BindOwnerKind = "panel_direct"
    BindOwnerPanelCaddy    BindOwnerKind = "panel_caddy"
    BindOwnerNaive         BindOwnerKind = "naive"
    BindOwnerHysteria2     BindOwnerKind = "hysteria2"
    BindOwnerLegacyCaddy   BindOwnerKind = "legacy_caddy"
    BindOwnerAcmeChallenge BindOwnerKind = "acme_challenge"
)

type BindOwner struct {
    Kind        BindOwnerKind
    ServiceName string
    InboundName string
}

func NormalizeAddress(addr string) string {
    addr = strings.TrimSpace(addr)
    if addr == "" {
        return "0.0.0.0"
    }
    return addr
}

func IsWildcard(addr string) bool {
    return addr == "0.0.0.0" || addr == "::" || addr == ""
}

func (k BindKey) Canonical() BindKey {
    return BindKey{Address: NormalizeAddress(k.Address), Port: k.Port, Network: k.Network}
}

func (k BindKey) Overlaps(other BindKey) bool {
    a := k.Canonical()
    b := other.Canonical()
    if a.Port != b.Port || a.Network != b.Network {
        return false
    }
    if a.Address == b.Address {
        return true
    }
    if IsWildcard(a.Address) || IsWildcard(b.Address) {
        return true
    }
    return false
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/bindregistry -run 'TestNormalizeAddress|TestBindKeyOverlap' -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/bindregistry/bindkey.go internal/bindregistry/bindkey_test.go
git commit -m "feat(bindregistry): BindKey model, owner kinds, address normalization"
```

---

## Task 2: Global bind conflict validation

**Files:**
- Create: `internal/bindregistry/conflicts.go`
- Test: `internal/bindregistry/conflicts_test.go`

**Interfaces:**
- Consumes: `BindKey`, `BindOwner` from Task 1.
- Produces:
  ```go
  type Conflict struct {
      Key     BindKey
      Owners  []BindOwner
      Message string
  }
  func ValidateNoConflicts(owners map[BindKey]BindOwner) []Conflict
  ```

- [ ] **Step 1: Write the failing test**

```go
package bindregistry

import "testing"

func TestValidateNoConflicts(t *testing.T) {
    owners := map[BindKey]BindOwner{
        {Address: "0.0.0.0", Port: 443, Network: ListenTCP}: {Kind: BindOwnerPanelCaddy},
        {Address: "192.168.1.10", Port: 443, Network: ListenTCP}: {Kind: BindOwnerNaive, InboundName: "naive-1"},
    }
    conflicts := ValidateNoConflicts(owners)
    if len(conflicts) == 0 {
        t.Fatal("expected conflict between wildcard Panel and specific naive on TCP 443")
    }
    if conflicts[0].Owners[0].Kind != BindOwnerPanelCaddy {
        t.Error("first owner should be Panel Caddy")
    }
}

func TestValidateNoConflictsNoCollision(t *testing.T) {
    owners := map[BindKey]BindOwner{
        {Address: "0.0.0.0", Port: 443, Network: ListenTCP}: {Kind: BindOwnerPanelCaddy},
        {Address: "0.0.0.0", Port: 443, Network: ListenUDP}: {Kind: BindOwnerHysteria2},
    }
    if conflicts := ValidateNoConflicts(owners); len(conflicts) != 0 {
        t.Fatalf("expected no TCP/UDP conflict, got %v", conflicts)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/bindregistry -run TestValidateNoConflicts -v
```

Expected: FAIL — `conflicts.go` missing.

- [ ] **Step 3: Write minimal implementation**

```go
package bindregistry

import "fmt"

type Conflict struct {
    Key     BindKey
    Owners  []BindOwner
    Message string
}

func ValidateNoConflicts(owners map[BindKey]BindOwner) []Conflict {
    canonical := make(map[BindKey]BindOwner, len(owners))
    for k, o := range owners {
        canonical[k.Canonical()] = o
    }
    var conflicts []Conflict
    for k, owner := range canonical {
        var overlapping []BindOwner
        for otherK, otherOwner := range canonical {
            if otherK == k {
                continue
            }
            if k.Overlaps(otherK) {
                overlapping = append(overlapping, otherOwner)
            }
        }
        if len(overlapping) > 0 {
            conflicts = append(conflicts, Conflict{
                Key:     k,
                Owners:  append([]BindOwner{owner}, overlapping...),
                Message: fmt.Sprintf("%s %s:%d is claimed by multiple owners", k.Network, k.Address, k.Port),
            })
        }
    }
    return conflicts
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/bindregistry -run TestValidateNoConflicts -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/bindregistry/conflicts.go internal/bindregistry/conflicts_test.go
git commit -m "feat(bindregistry): global bind conflict validation"
```

---

## Task 3: Domain-level certificate ownership and ACME email resolution

**Files:**
- Create: `internal/caddyassembly/domain.go`
- Test: `internal/caddyassembly/domain_test.go`

**Interfaces:**
- Consumes: `model.Settings`, `model.Inbound`.
- Produces:
  ```go
  type CaddyDomainOwners struct {
      Panel               bool
      NaiveInboundNames   []string
      HysteriaInboundNames []string
  }
  type CaddyDomainCertSpec struct {
      Domain string
      Email  string
      Owners CaddyDomainOwners
  }
  func ResolveDomainCertSpecs(settings model.Settings, inbounds []model.Inbound) (map[string]CaddyDomainCertSpec, error)
  ```

- [ ] **Step 1: Write the failing test**

```go
package caddyassembly

import (
    "testing"

    "github.com/mikkelchokolate/Veil/internal/model"
)

func TestResolveDomainCertSpecsConflictingEmail(t *testing.T) {
    settings := model.Settings{PanelAccess: "direct"}
    inbounds := []model.Inbound{
        {Name: "n1", Protocol: "naiveproxy", ProtocolFields: map[string]any{"domain": "x.com", "email": "a@x.com"}},
        {Name: "n2", Protocol: "naiveproxy", ProtocolFields: map[string]any{"domain": "x.com", "email": "b@x.com"}},
    }
    _, err := ResolveDomainCertSpecs(settings, inbounds)
    if err == nil {
        t.Fatal("expected conflicting email error")
    }
}

func TestResolveDomainCertSpecsFallback(t *testing.T) {
    settings := model.Settings{PanelAccess: "direct", DefaultAcmeEmail: "admin@x.com"}
    inbounds := []model.Inbound{
        {Name: "n1", Protocol: "naiveproxy", ProtocolFields: map[string]any{"domain": "x.com"}},
    }
    specs, err := ResolveDomainCertSpecs(settings, inbounds)
    if err != nil {
        t.Fatal(err)
    }
    if specs["x.com"].Email != "admin@x.com" {
        t.Errorf("expected fallback email, got %q", specs["x.com"].Email)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/caddyassembly -run 'TestResolveDomainCertSpecs' -v
```

Expected: FAIL — `domain.go` missing.

- [ ] **Step 3: Write minimal implementation**

```go
package caddyassembly

import (
    "errors"
    "fmt"
    "strings"

    "github.com/mikkelchokolate/Veil/internal/model"
)

type CaddyDomainOwners struct {
    Panel                bool
    NaiveInboundNames    []string
    HysteriaInboundNames []string
}

type CaddyDomainCertSpec struct {
    Domain string
    Email  string
    Owners CaddyDomainOwners
}

func ResolveDomainCertSpecs(settings model.Settings, inbounds []model.Inbound) (map[string]CaddyDomainCertSpec, error) {
    owners := make(map[string]*CaddyDomainOwners)
    emails := make(map[string]map[string]struct{})

    if settings.PanelAccess == "caddy" && settings.Domain != "" {
        domain := strings.ToLower(settings.Domain)
        ensureOwner(owners, domain).Panel = true
        addEmail(emails, domain, settings.Email)
    }

    for _, inb := range inbounds {
        if inb.Protocol == "naiveproxy" {
            domain := strings.ToLower(stringField(inb.ProtocolFields, "domain"))
            if domain == "" {
                continue
            }
            ensureOwner(owners, domain).NaiveInboundNames = append(ensureOwner(owners, domain).NaiveInboundNames, inb.Name)
            addEmail(emails, domain, stringField(inb.ProtocolFields, "email"))
        }
        if inb.Protocol == "hysteria2" {
            domain := strings.ToLower(stringField(inb.ProtocolFields, "domain"))
            if domain == "" {
                continue
            }
            ensureOwner(owners, domain).HysteriaInboundNames = append(ensureOwner(owners, domain).HysteriaInboundNames, inb.Name)
            addEmail(emails, domain, stringField(inb.ProtocolFields, "email"))
        }
    }

    specs := make(map[string]CaddyDomainCertSpec, len(owners))
    for domain, o := range owners {
        resolved, err := resolveEmail(domain, emails[domain], settings)
        if err != nil {
            return nil, err
        }
        specs[domain] = CaddyDomainCertSpec{Domain: domain, Email: resolved, Owners: *o}
    }
    return specs, nil
}

func ensureOwner(m map[string]*CaddyDomainOwners, domain string) *CaddyDomainOwners {
    if m[domain] == nil {
        m[domain] = &CaddyDomainOwners{}
    }
    return m[domain]
}

func addEmail(m map[string]map[string]struct{}, domain, email string) {
    email = strings.TrimSpace(email)
    if email == "" {
        return
    }
    if m[domain] == nil {
        m[domain] = make(map[string]struct{})
    }
    m[domain][email] = struct{}{}
}

func resolveEmail(domain string, explicit map[string]struct{}, settings model.Settings) (string, error) {
    if len(explicit) > 1 {
        return "", fmt.Errorf("domain %s has conflicting ACME emails", domain)
    }
    for e := range explicit {
        return e, nil
    }
    if settings.DefaultAcmeEmail != "" {
        return settings.DefaultAcmeEmail, nil
    }
    if settings.PanelEmail != "" {
        return settings.PanelEmail, nil
    }
    return "", errors.New("no ACME email resolved for domain " + domain)
}

func stringField(m map[string]any, key string) string {
    if m == nil {
        return ""
    }
    v, ok := m[key].(string)
    if !ok {
        return ""
    }
    return v
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/caddyassembly -run 'TestResolveDomainCertSpecs' -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/caddyassembly/domain.go internal/caddyassembly/domain_test.go
git commit -m "feat(caddyassembly): domain cert ownership and ACME email resolution"
```

---

## Task 4: Extend Settings model and validation

**Files:**
- Modify: `internal/model/types.go`
- Modify: `internal/settings/settings_validation.go`
- Test: `internal/settings/settings_validation_test.go` (extend existing)

**Interfaces:**
- Produces: `Settings` with new fields; validation for `PanelPublicPort`, `DefaultAcmeEmail`, `DefaultInboundPublicPort`, `AcmeChallengeMode`.

- [ ] **Step 1: Write the failing test**

```go
package settings

import (
    "testing"

    "github.com/mikkelchokolate/Veil/internal/model"
)

func TestNormalizeAndValidateNewFields(t *testing.T) {
    current := model.Settings{PanelListen: "0.0.0.0:8080", Mode: "server"}
    s := model.Settings{
        PanelListen:              "0.0.0.0:8080",
        Mode:                     "server",
        PanelAccess:              "caddy",
        PanelDomain:              "panel.example.com",
        PanelEmail:               "admin@example.com",
        PanelPublicPort:          8443,
        DefaultAcmeEmail:         "acme@example.com",
        DefaultInboundPublicPort: 443,
        AcmeChallengeMode:        "tls-alpn-01",
    }
    if err := NewSettingsValidation().NormalizeAndValidate(&s, current); err != nil {
        t.Fatal(err)
    }
    if s.PanelPublicPort != 8443 {
        t.Errorf("PanelPublicPort = %d", s.PanelPublicPort)
    }
    if s.DefaultInboundPublicPort != 443 {
        t.Errorf("DefaultInboundPublicPort = %d", s.DefaultInboundPublicPort)
    }
}

func TestNormalizeAndValidateInvalidChallengeMode(t *testing.T) {
    current := model.Settings{PanelListen: "0.0.0.0:8080", Mode: "server"}
    s := model.Settings{
        PanelListen:       "0.0.0.0:8080",
        Mode:              "server",
        PanelAccess:       "direct",
        AcmeChallengeMode: "ftp-01",
    }
    if err := NewSettingsValidation().NormalizeAndValidate(&s, current); err == nil {
        t.Fatal("expected invalid challenge mode error")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/settings -run 'TestNormalizeAndValidateNewFields|TestNormalizeAndValidateInvalidChallengeMode' -v
```

Expected: FAIL — new fields unknown or validation missing.

- [ ] **Step 3: Write minimal implementation**

In `internal/model/types.go`, add fields to `Settings`:

```go
    PanelPublicPort        int    `json:"panelPublicPort,omitempty"`
    DefaultAcmeEmail       string `json:"defaultAcmeEmail,omitempty"`
    DefaultInboundPublicPort int  `json:"defaultInboundPublicPort,omitempty"`
    AcmeChallengeMode      string `json:"acmeChallengeMode,omitempty"`
```

In `internal/settings/settings_validation.go`, add inside `NormalizeAndValidate` after PanelAccess switch:

```go
    if settings.PanelPublicPort == 0 {
        settings.PanelPublicPort = current.PanelPublicPort
    }
    if settings.PanelPublicPort == 0 {
        settings.PanelPublicPort = 443
    }
    if settings.DefaultInboundPublicPort == 0 {
        settings.DefaultInboundPublicPort = current.DefaultInboundPublicPort
    }
    if settings.DefaultInboundPublicPort == 0 {
        settings.DefaultInboundPublicPort = 443
    }
    if settings.AcmeChallengeMode == "" {
        settings.AcmeChallengeMode = current.AcmeChallengeMode
    }
    if settings.AcmeChallengeMode == "" {
        settings.AcmeChallengeMode = "tls-alpn-01"
    }
    switch settings.AcmeChallengeMode {
    case "http-01", "tls-alpn-01", "dns-01":
    default:
        return errors.New("acmeChallengeMode must be http-01, tls-alpn-01, or dns-01")
    }
    if settings.PanelPublicPort < 1 || settings.PanelPublicPort > 65535 {
        return errors.New("panelPublicPort must be between 1 and 65535")
    }
    if settings.DefaultInboundPublicPort < 1 || settings.DefaultInboundPublicPort > 65535 {
        return errors.New("defaultInboundPublicPort must be between 1 and 65535")
    }
    if settings.DefaultAcmeEmail != "" {
        if err := hostenv.ValidateEmail(settings.DefaultAcmeEmail); err != nil {
            return errors.New("defaultAcmeEmail: " + err.Error())
        }
    }
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/settings -run 'TestNormalizeAndValidateNewFields|TestNormalizeAndValidateInvalidChallengeMode' -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/model/types.go internal/settings/settings_validation.go internal/settings/settings_validation_test.go
git commit -m "feat(settings): PanelPublicPort, DefaultAcmeEmail, DefaultInboundPublicPort, AcmeChallengeMode"
```

---

## Task 5: Naive protocol field helpers and UI schema

**Files:**
- Modify: `internal/protocols/naiveproxy/plugin.go`
- Modify: `internal/protocols/naiveproxy/ui.go`
- Test: `internal/protocols/naiveproxy/naiveproxy_test.go` (extend)

**Interfaces:**
- Produces:
  ```go
  func NaiveDomain(inbound model.Inbound) string
  func NaiveEmail(inbound model.Inbound) string
  func NaivePublicPort(settings model.Settings, inbound model.Inbound) int
  func NaiveTransport(inbound model.Inbound) string
  func NaiveFallbackRoot(settings model.Settings, inbound model.Inbound) string
  ```

- [ ] **Step 1: Write the failing test**

```go
package naiveproxy

import (
    "testing"

    "github.com/mikkelchokolate/Veil/internal/model"
)

func TestNaiveProtocolFieldHelpers(t *testing.T) {
    settings := model.Settings{DefaultInboundPublicPort: 8443, FallbackRoot: "/var/lib/veil/www"}
    inbound := model.Inbound{
        Protocol: "naiveproxy",
        ProtocolFields: map[string]any{
            "domain":     "p.example.com",
            "email":      "a@example.com",
            "publicPort": 9443,
            "transport":  "dual",
        },
    }
    if got := NaiveDomain(inbound); got != "p.example.com" {
        t.Errorf("domain = %q", got)
    }
    if got := NaivePublicPort(settings, inbound); got != 9443 {
        t.Errorf("publicPort = %d", got)
    }
    if got := NaiveTransport(inbound); got != "dual" {
        t.Errorf("transport = %q", got)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/protocols/naiveproxy -run TestNaiveProtocolFieldHelpers -v
```

Expected: FAIL — helpers not defined.

- [ ] **Step 3: Write minimal implementation**

In `internal/protocols/naiveproxy/plugin.go`, append:

```go
func NaiveDomain(inbound model.Inbound) string {
    return stringField(inbound.ProtocolFields, "domain")
}

func NaiveEmail(inbound model.Inbound) string {
    return stringField(inbound.ProtocolFields, "email")
}

func NaivePublicPort(settings model.Settings, inbound model.Inbound) int {
    if v, ok := inbound.ProtocolFields["publicPort"]; ok {
        if n, ok := v.(float64); ok {
            return int(n)
        }
        if n, ok := v.(int); ok {
            return n
        }
    }
    if inbound.Port != 0 {
        return inbound.Port
    }
    if settings.DefaultInboundPublicPort != 0 {
        return settings.DefaultInboundPublicPort
    }
    return 443
}

func NaiveTransport(inbound model.Inbound) string {
    t := stringField(inbound.ProtocolFields, "transport")
    if t == "" {
        return "tcp"
    }
    return t
}

func NaiveFallbackRoot(settings model.Settings, inbound model.Inbound) string {
    root := stringField(inbound.ProtocolFields, "fallbackRoot")
    if root == "" {
        root = inbound.FallbackRoot
    }
    if root == "" {
        root = settings.FallbackRoot
    }
    if root == "" {
        root = "/var/lib/veil/www"
    }
    return root
}

func stringField(m map[string]any, key string) string {
    if m == nil {
        return ""
    }
    v, ok := m[key].(string)
    if !ok {
        return ""
    }
    return v
}
```

Update `internal/protocols/naiveproxy/ui.go` `InboundFieldSchema()` to include `domain`, `email`, `publicPort`, `transport` fields before existing credential/fallback fields. Example field schema (reuse existing `schema.FieldSchema` patterns):

```go
{
    Name:        "domain",
    Type:        "string",
    Label:       "Domain",
    Required:    true,
    HelpText:    "Public domain used for TLS/SNI and client export.",
},
{
    Name:        "email",
    Type:        "string",
    Label:       "ACME email",
    Required:    false,
    HelpText:    "Optional explicit ACME contact for this domain.",
},
{
    Name:        "publicPort",
    Type:        "number",
    Label:       "Public port",
    Required:    false,
    Default:     443,
    HelpText:    "Port Caddy listens on for this inbound.",
},
{
    Name:        "transport",
    Type:        "select",
    Label:       "Transport",
    Required:    true,
    Options:     []string{"tcp", "quic", "dual"},
    Default:     "tcp",
    HelpText:    "tcp=HTTPS/H2, quic=HTTP/3/QUIC, dual=both.",
},
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/protocols/naiveproxy -run TestNaiveProtocolFieldHelpers -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/protocols/naiveproxy/plugin.go internal/protocols/naiveproxy/ui.go internal/protocols/naiveproxy/naiveproxy_test.go
git commit -m "feat(naiveproxy): typed ProtocolFields helpers and UI schema for domain/port/transport"
```

---

## Task 6: Naive validator bind-key checks

**Files:**
- Modify: `internal/protocols/naiveproxy/validator.go`
- Test: `internal/protocols/naiveproxy/validator_test.go` (create if missing)

**Interfaces:**
- Consumes: `bindregistry` from Tasks 1-2, helpers from Task 5.
- Produces: `ValidationIssue`s for missing domain, invalid public port/transport, bind conflicts.

- [ ] **Step 1: Write the failing test**

```go
package naiveproxy

import (
    "testing"

    "github.com/mikkelchokolate/Veil/internal/model"
)

func TestValidateInboundMissingDomain(t *testing.T) {
    v := Validator{}
    issues := v.ValidateInbound(model.Settings{}, model.Inbound{Protocol: "naiveproxy", ProtocolFields: map[string]any{}})
    if len(issues) == 0 {
        t.Fatal("expected missing domain issue")
    }
}

func TestValidateInboundInvalidTransport(t *testing.T) {
    v := Validator{}
    inbound := model.Inbound{
        Protocol: "naiveproxy",
        ProtocolFields: map[string]any{
            "domain":    "x.com",
            "transport": "udp",
        },
    }
    issues := v.ValidateInbound(model.Settings{}, inbound)
    found := false
    for _, i := range issues {
        if i.Field == "transport" {
            found = true
        }
    }
    if !found {
        t.Fatal("expected invalid transport issue")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/protocols/naiveproxy -run 'TestValidateInbound' -v
```

Expected: FAIL — new validation not implemented.

- [ ] **Step 3: Write minimal implementation**

In `internal/protocols/naiveproxy/validator.go`, extend `ValidateInbound`. Add helper:

```go
func (v Validator) ValidateInbound(settings model.Settings, inbound model.Inbound) []model.ValidationIssue {
    var issues []model.ValidationIssue
    domain := NaiveDomain(inbound)
    if domain == "" {
        issues = append(issues, model.ValidationIssue{
            Code:     "naive_domain_required",
            Severity: "error",
            Field:    "domain",
            Message:  "Naive inbound requires a public domain.",
            Source:   "naiveproxy",
        })
    }
    transport := NaiveTransport(inbound)
    switch transport {
    case "tcp", "quic", "dual":
    default:
        issues = append(issues, model.ValidationIssue{
            Code:     "naive_transport_invalid",
            Severity: "error",
            Field:    "transport",
            Message:  "transport must be tcp, quic, or dual",
            Source:   "naiveproxy",
        })
    }
    port := NaivePublicPort(settings, inbound)
    if port < 1 || port > 65535 {
        issues = append(issues, model.ValidationIssue{
            Code:     "naive_public_port_invalid",
            Severity: "error",
            Field:    "publicPort",
            Message:  "publicPort must be between 1 and 65535",
            Source:   "naiveproxy",
        })
    }
    // credential presence preserved from existing validator
    if !v.HasCredential(settings, inbound) {
        issues = append(issues, model.ValidationIssue{
            Code:     "naive_credential_required",
            Severity: "error",
            Field:    "profiles",
            Message:  "At least one username/password profile is required.",
            Source:   "naiveproxy",
        })
    }
    return issues
}
```

Keep existing `ValidateSettings` unchanged for this task.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/protocols/naiveproxy -run 'TestValidateInbound' -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/protocols/naiveproxy/validator.go internal/protocols/naiveproxy/validator_test.go
git commit -m "feat(naiveproxy): validator checks domain, transport, publicPort"
```

---

## Task 6b: Hysteria2 inbound domain reader and renderer update

**Files:**
- Modify: `internal/protocols/hysteria2/plugin.go`
- Modify: `internal/protocols/hysteria2/renderer.go`
- Modify: `internal/generatedconfig/inbound_renderer.go`
- Test: `internal/protocols/hysteria2/hysteria2_test.go` (extend)

**Interfaces:**
- Produces:
  ```go
  func Hysteria2Domain(inbound model.Inbound) string
  ```
- Renderer uses inbound domain (not settings global domain) for cert path.

- [ ] **Step 1: Write the failing test**

```go
package hysteria2

import (
    "testing"

    "github.com/mikkelchokolate/Veil/internal/model"
)

func TestHysteria2DomainFromProtocolFields(t *testing.T) {
    inbound := model.Inbound{
        Protocol: "hysteria2",
        ProtocolFields: map[string]any{"domain": "hy.example.com"},
    }
    if got := Hysteria2Domain(inbound); got != "hy.example.com" {
        t.Errorf("domain = %q", got)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/protocols/hysteria2 -run TestHysteria2DomainFromProtocolFields -v
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

In `internal/protocols/hysteria2/plugin.go`, append:

```go
func Hysteria2Domain(inbound model.Inbound) string {
    if inbound.ProtocolFields == nil {
        return ""
    }
    v, ok := inbound.ProtocolFields["domain"].(string)
    if !ok {
        return ""
    }
    return v
}
```

In `internal/protocols/hysteria2/renderer.go` (or `internal/generatedconfig/inbound_renderer.go`), change cert path resolution from `r.settings.Domain` to `Hysteria2Domain(inbound)`. When domain is empty, leave `CertPath`/`KeyPath` empty.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/protocols/hysteria2 -run TestHysteria2DomainFromProtocolFields -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/protocols/hysteria2/plugin.go internal/protocols/hysteria2/renderer.go internal/generatedconfig/inbound_renderer.go internal/protocols/hysteria2/hysteria2_test.go
git commit -m "feat(hysteria2): per-inbound domain and cert path"
```

## Task 7: ACME challenge bind planning

**Files:**
- Create: `internal/caddyassembly/challenge.go`
- Test: `internal/caddyassembly/challenge_test.go`

**Interfaces:**
- Consumes: `bindregistry`, `CaddyDomainCertSpec` from Task 3.
- Produces:
  ```go
  type AcmeChallengeOwner struct {
      ChallengeMode string
      Domains       []string
  }
  func PlanAcmeChallengeBinds(
      challengeMode string,
      domains map[string]CaddyDomainCertSpec,
      owners map[bindregistry.BindKey]bindregistry.BindOwner,
  ) (map[bindregistry.BindKey]AcmeChallengeOwner, []model.ValidationIssue)
  ```

- [ ] **Step 1: Write the failing test**

```go
package caddyassembly

import (
    "testing"

    "github.com/mikkelchokolate/Veil/internal/bindregistry"
)

func TestPlanAcmeChallengeBindsHTTP01(t *testing.T) {
    domains := map[string]CaddyDomainCertSpec{
        "x.com": {Domain: "x.com", Email: "a@x.com"},
    }
    owners := map[bindregistry.BindKey]bindregistry.BindOwner{}
    planned, issues := PlanAcmeChallengeBinds("http-01", domains, owners)
    if len(issues) > 0 {
        t.Fatalf("unexpected issues: %v", issues)
    }
    key := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
    if _, ok := planned[key]; !ok {
        t.Fatal("expected TCP :80 challenge bind")
    }
}

func TestPlanAcmeChallengeBindsDNS01NoBind(t *testing.T) {
    domains := map[string]CaddyDomainCertSpec{
        "x.com": {Domain: "x.com", Email: "a@x.com"},
    }
    owners := map[bindregistry.BindKey]bindregistry.BindOwner{}
    planned, issues := PlanAcmeChallengeBinds("dns-01", domains, owners)
    if len(issues) > 0 {
        t.Fatalf("unexpected issues: %v", issues)
    }
    if len(planned) != 0 {
        t.Fatalf("dns-01 should add no binds, got %v", planned)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/caddyassembly -run 'TestPlanAcmeChallengeBinds' -v
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

```go
package caddyassembly

import (
    "github.com/mikkelchokolate/Veil/internal/bindregistry"
    "github.com/mikkelchokolate/Veil/internal/model"
)

type AcmeChallengeOwner struct {
    ChallengeMode string
    Domains       []string
}

func PlanAcmeChallengeBinds(
    challengeMode string,
    domains map[string]CaddyDomainCertSpec,
    owners map[bindregistry.BindKey]bindregistry.BindOwner,
) (map[bindregistry.BindKey]AcmeChallengeOwner, []model.ValidationIssue) {
    result := make(map[bindregistry.BindKey]AcmeChallengeOwner)
    var issues []model.ValidationIssue

    add := func(key bindregistry.BindKey, domain string) {
        owner := result[key]
        owner.ChallengeMode = challengeMode
        owner.Domains = append(owner.Domains, domain)
        result[key] = owner
    }

    for _, spec := range domains {
        switch challengeMode {
        case "http-01":
            key := bindregistry.BindKey{Address: "0.0.0.0", Port: 80, Network: bindregistry.ListenTCP}
            if existing, ok := owners[key]; ok && existing.Kind != bindregistry.BindOwnerAcmeChallenge {
                issues = append(issues, model.ValidationIssue{
                    Code:     "acme_http01_port_in_use",
                    Severity: "error",
                    Message:  "TCP :80 is required for http-01 but is owned by another service",
                    Source:   "caddyassembly",
                })
                continue
            }
            add(key, spec.Domain)
        case "tls-alpn-01":
            key := bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}
            if existing, ok := owners[key]; ok && existing.Kind != bindregistry.BindOwnerAcmeChallenge {
                // TLS-ALPN-01 can reuse a compatible Caddy TCP :443 listener; if owner is not Caddy, reject.
                if existing.Kind != bindregistry.BindOwnerPanelCaddy && existing.Kind != bindregistry.BindOwnerNaive {
                    issues = append(issues, model.ValidationIssue{
                        Code:     "acme_tlsalpn_port_in_use",
                        Severity: "error",
                        Message:  "TCP :443 is required for tls-alpn-01 but is owned by a non-Caddy service",
                        Source:   "caddyassembly",
                    })
                    continue
                }
            }
            add(key, spec.Domain)
        case "dns-01":
            // no bind
        }
    }
    return result, issues
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/caddyassembly -run 'TestPlanAcmeChallengeBinds' -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/caddyassembly/challenge.go internal/caddyassembly/challenge_test.go
git commit -m "feat(caddyassembly): ACME challenge bind planning"
```

---

## Task 8: Caddy capability detection

**Files:**
- Create: `internal/caddycapabilities/capabilities.go`
- Test: `internal/caddycapabilities/capabilities_test.go`

**Interfaces:**
- Produces:
  ```go
  type CaddyCapabilities struct {
      ForwardProxy bool
      HTTP3        bool
      H3Only       bool
  }
  func Probe(binaryPath string) (CaddyCapabilities, error)
  ```

- [ ] **Step 1: Write the failing test**

```go
package caddycapabilities

import "testing"

func TestProbeParsesModuleList(t *testing.T) {
    // A mock binary that prints a module list matching Caddy's `caddy list-modules --json` shape.
    // For the plan we test parsing of a known JSON fragment.
    input := `[
      {"name":"http.handlers.forward_proxy"},
      {"name":"http"}
    ]`
    caps, err := parseModuleList([]byte(input))
    if err != nil {
        t.Fatal(err)
    }
    if !caps.ForwardProxy {
        t.Error("expected ForwardProxy=true")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/caddycapabilities -run TestProbeParsesModuleList -v
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

```go
package caddycapabilities

import (
    "encoding/json"
    "fmt"
    "os/exec"
)

type CaddyCapabilities struct {
    ForwardProxy bool
    HTTP3        bool
    H3Only       bool
}

type caddyModule struct {
    Name string `json:"name"`
}

func Probe(binaryPath string) (CaddyCapabilities, error) {
    if binaryPath == "" {
        binaryPath = "caddy"
    }
    out, err := exec.Command(binaryPath, "list-modules", "--json").Output()
    if err != nil {
        return CaddyCapabilities{}, fmt.Errorf("caddy list-modules failed: %w", err)
    }
    caps, err := parseModuleList(out)
    if err != nil {
        return CaddyCapabilities{}, err
    }
    // HTTP3 is available in standard Caddy builds; this flag is set true when
    // the base http app module is present. H3Only is intentionally left false
    // here and verified behaviorally before `quic` transport is accepted.
    caps.HTTP3 = hasModule(modules, "http")
    return caps, nil
}

func hasModule(modules []caddyModule, name string) bool {
    for _, m := range modules {
        if m.Name == name {
            return true
        }
    }
    return false
}

func parseModuleList(data []byte) (CaddyCapabilities, error) {
    var modules []caddyModule
    if err := json.Unmarshal(data, &modules); err != nil {
        return CaddyCapabilities{}, err
    }
    var caps CaddyCapabilities
    for _, m := range modules {
        switch m.Name {
        case "http.handlers.forward_proxy":
            caps.ForwardProxy = true
        case "http":
            // HTTP base present
        }
    }
    return caps, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/caddycapabilities -run TestProbeParsesModuleList -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/caddycapabilities/capabilities.go internal/caddycapabilities/capabilities_test.go
git commit -m "feat(caddycapabilities): probe Caddy binary for forward_proxy module"
```

---

## Task 9: Caddy render plan assembly

**Files:**
- Create: `internal/caddyassembly/plan.go`
- Test: `internal/caddyassembly/plan_test.go`

**Interfaces:**
- Consumes: `bindregistry`, `CaddyDomainCertSpec` from Task 3, `AcmeChallengeOwner` from Task 7.
- Produces:
  ```go
  type CaddyBindOwnerKind string
  type CaddyBindOwner struct{ Kind CaddyBindOwnerKind; Domain string; InboundName string }
  type CaddyRenderPlan struct {
      Servers        map[bindregistry.BindKey]CaddyBindOwner
      ACMEChallenges map[bindregistry.BindKey]AcmeChallengeOwner
      Domains        map[string]CaddyDomainCertSpec
  }
  func BuildRenderPlan(
      settings model.Settings,
      inbounds []model.Inbound,
      challengeBinds map[bindregistry.BindKey]AcmeChallengeOwner,
  ) (CaddyRenderPlan, map[bindregistry.BindKey]bindregistry.BindOwner, error)
  ```

- [ ] **Step 1: Write the failing test**

```go
package caddyassembly

import (
    "testing"

    "github.com/mikkelchokolate/Veil/internal/bindregistry"
    "github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildRenderPlanPanelAndNaive(t *testing.T) {
    settings := model.Settings{
        PanelAccess: "caddy",
        PanelDomain: "panel.example.com",
        PanelPublicPort: 443,
    }
    inbounds := []model.Inbound{
        {
            Name:     "naive-1",
            Protocol: "naiveproxy",
            ProtocolFields: map[string]any{
                "domain": "proxy.example.com",
                "transport": "tcp",
                "publicPort": 8443,
            },
        },
    }
    plan, owners, err := BuildRenderPlan(settings, inbounds, nil)
    if err != nil {
        t.Fatal(err)
    }
    panelKey := bindregistry.BindKey{Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}
    if owners[panelKey].Kind != bindregistry.BindOwnerPanelCaddy {
        t.Error("expected Panel Caddy owner on TCP 443")
    }
    if plan.Servers[panelKey].Kind != CaddyOwnerPanel {
        t.Error("expected Panel server in render plan")
    }
    naiveKey := bindregistry.BindKey{Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}
    if owners[naiveKey].Kind != bindregistry.BindOwnerNaive {
        t.Error("expected naive owner on TCP 8443")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/caddyassembly -run TestBuildRenderPlanPanelAndNaive -v
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

```go
package caddyassembly

import (
    "github.com/mikkelchokolate/Veil/internal/bindregistry"
    "github.com/mikkelchokolate/Veil/internal/model"
    "github.com/mikkelchokolate/Veil/internal/protocols/naiveproxy"
)

type CaddyBindOwnerKind string

const (
    CaddyOwnerPanel CaddyBindOwnerKind = "panel"
    CaddyOwnerNaive CaddyBindOwnerKind = "naive"
)

type CaddyBindOwner struct {
    Kind        CaddyBindOwnerKind
    Domain      string
    InboundName string
}

type CaddyRenderPlan struct {
    Servers        map[bindregistry.BindKey]CaddyBindOwner
    ACMEChallenges map[bindregistry.BindKey]AcmeChallengeOwner
    Domains        map[string]CaddyDomainCertSpec
}

func BuildRenderPlan(
    settings model.Settings,
    inbounds []model.Inbound,
    challengeBinds map[bindregistry.BindKey]AcmeChallengeOwner,
) (CaddyRenderPlan, map[bindregistry.BindKey]bindregistry.BindOwner, error) {
    owners := make(map[bindregistry.BindKey]bindregistry.BindOwner)
    servers := make(map[bindregistry.BindKey]CaddyBindOwner)

    if settings.PanelAccess == "caddy" && settings.PanelDomain != "" {
        key := bindregistry.BindKey{Address: "0.0.0.0", Port: settings.PanelPublicPort, Network: bindregistry.ListenTCP}
        owners[key] = bindregistry.BindOwner{Kind: bindregistry.BindOwnerPanelCaddy, ServiceName: "veil-caddy.service"}
        servers[key] = CaddyBindOwner{Kind: CaddyOwnerPanel, Domain: settings.PanelDomain}
    }

    for _, inb := range inbounds {
        if inb.Protocol != "naiveproxy" || !inb.Enabled {
            continue
        }
        transport := naiveproxy.NaiveTransport(inb)
        port := naiveproxy.NaivePublicPort(settings, inb)
        domain := naiveproxy.NaiveDomain(inb)
        addNaiveBinds(transport, port, domain, inb.Name, owners, servers)
    }

    domains, err := ResolveDomainCertSpecs(settings, inbounds)
    if err != nil {
        return CaddyRenderPlan{}, nil, err
    }

    return CaddyRenderPlan{
        Servers:        servers,
        ACMEChallenges: challengeBinds,
        Domains:        domains,
    }, owners, nil
}

func addNaiveBinds(transport string, port int, domain, name string, owners map[bindregistry.BindKey]bindregistry.BindOwner, servers map[bindregistry.BindKey]CaddyBindOwner) {
    if transport == "tcp" || transport == "dual" {
        key := bindregistry.BindKey{Address: "0.0.0.0", Port: port, Network: bindregistry.ListenTCP}
        owners[key] = bindregistry.BindOwner{Kind: bindregistry.BindOwnerNaive, ServiceName: "veil-caddy.service", InboundName: name}
        servers[key] = CaddyBindOwner{Kind: CaddyOwnerNaive, Domain: domain, InboundName: name}
    }
    if transport == "quic" || transport == "dual" {
        key := bindregistry.BindKey{Address: "0.0.0.0", Port: port, Network: bindregistry.ListenUDP}
        owners[key] = bindregistry.BindOwner{Kind: bindregistry.BindOwnerNaive, ServiceName: "veil-caddy.service", InboundName: name}
        servers[key] = CaddyBindOwner{Kind: CaddyOwnerNaive, Domain: domain, InboundName: name}
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/caddyassembly -run TestBuildRenderPlanPanelAndNaive -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/caddyassembly/plan.go internal/caddyassembly/plan_test.go
git commit -m "feat(caddyassembly): CaddyRenderPlan builder from settings and inbounds"
```

---

## Task 10: Caddy JSON renderer

**Files:**
- Create: `internal/renderer/caddyjson.go`
- Test: `internal/renderer/caddyjson_test.go`

**Interfaces:**
- Consumes: `caddyassembly.CaddyRenderPlan`, `caddycapabilities.CaddyCapabilities`.
- Produces: `RenderCaddyJSON(plan caddyassembly.CaddyRenderPlan, caps caddycapabilities.CaddyCapabilities) ([]byte, error)`.

- [ ] **Step 1: Write the failing test**

```go
package renderer

import (
    "encoding/json"
    "testing"

    "github.com/mikkelchokolate/Veil/internal/bindregistry"
    "github.com/mikkelchokolate/Veil/internal/caddyassembly"
    "github.com/mikkelchokolate/Veil/internal/caddycapabilities"
)

func TestRenderCaddyJSONPanelOnly(t *testing.T) {
    plan := caddyassembly.CaddyRenderPlan{
        Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
            {Address: "0.0.0.0", Port: 443, Network: bindregistry.ListenTCP}: {
                Kind:   caddyassembly.CaddyOwnerPanel,
                Domain: "panel.example.com",
            },
        },
        Domains: map[string]caddyassembly.CaddyDomainCertSpec{
            "panel.example.com": {Domain: "panel.example.com", Email: "admin@example.com"},
        },
    }
    data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{})
    if err != nil {
        t.Fatal(err)
    }
    var cfg map[string]any
    if err := json.Unmarshal(data, &cfg); err != nil {
        t.Fatal(err)
    }
    apps := cfg["apps"].(map[string]any)
    if _, ok := apps["http"]; !ok {
        t.Error("expected http app")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/renderer -run TestRenderCaddyJSONPanelOnly -v
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

`internal/renderer/caddyjson.go` skeleton:

```go
package renderer

import (
    "encoding/json"

    "github.com/mikkelchokolate/Veil/internal/bindregistry"
    "github.com/mikkelchokolate/Veil/internal/caddyassembly"
    "github.com/mikkelchokolate/Veil/internal/caddycapabilities"
)

func RenderCaddyJSON(plan caddyassembly.CaddyRenderPlan, caps caddycapabilities.CaddyCapabilities) ([]byte, error) {
    cfg := caddyConfig{Apps: map[string]any{}}
    cfg.Apps["http"] = renderHTTPApp(plan, caps)
    cfg.Apps["tls"] = renderTLSApp(plan)
    return json.MarshalIndent(cfg, "", "  ")
}

type caddyConfig struct {
    Apps map[string]any `json:"apps"`
}

func renderHTTPApp(plan caddyassembly.CaddyRenderPlan, caps caddycapabilities.CaddyCapabilities) map[string]any {
    servers := make(map[string]any)
    for key, owner := range plan.Servers {
        serverName := serverNameFor(key)
        servers[serverName] = renderServer(key, owner, caps)
    }
    for key, owner := range plan.ACMEChallenges {
        serverName := serverNameFor(key) + "-acme"
        servers[serverName] = renderAcmeChallengeServer(key, owner)
    }
    return map[string]any{"servers": servers}
}

func renderTLSApp(plan caddyassembly.CaddyRenderPlan) map[string]any {
    byIssuer := make(map[string][]string)
    for _, spec := range plan.Domains {
        issuerKey := spec.Email + "/" + challengeForDomain(plan, spec.Domain)
        byIssuer[issuerKey] = append(byIssuer[issuerKey], spec.Domain)
    }
    var policies []map[string]any
    for _, domains := range byIssuer {
        policies = append(policies, map[string]any{
            "subjects": domains,
            "issuer":   map[string]any{"email": domains[0], "module": "acme"},
        })
    }
    return map[string]any{"automation": map[string]any{"policies": policies}}
}

func renderServer(key bindregistry.BindKey, owner caddyassembly.CaddyBindOwner, caps caddycapabilities.CaddyCapabilities) map[string]any {
    // Naive server: no host matcher, forward_proxy first, file_server fallback.
    // Panel server: host matcher, reverse_proxy to panel loopback.
    // Implementation expanded in Task 11.
    return map[string]any{
        "listen": []string{listenString(key)},
        "routes": []map[string]any{},
    }
}

func renderAcmeChallengeServer(key bindregistry.BindKey, owner caddyassembly.AcmeChallengeOwner) map[string]any {
    return map[string]any{
        "listen": []string{listenString(key)},
        "routes": []map[string]any{},
    }
}

func serverNameFor(key bindregistry.BindKey) string {
    return string(key.Network) + "-" + key.Address + "-" + portString(key.Port)
}

func listenString(key bindregistry.BindKey) string {
    return ":" + portString(key.Port)
}

func portString(p int) string { return strconv.Itoa(p) }
```

(Import `strconv` and add Panel/Naive route details in Task 11.)

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/renderer -run TestRenderCaddyJSONPanelOnly -v
```

Expected: PASS after fixing imports.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/renderer/caddyjson.go internal/renderer/caddyjson_test.go
git commit -m "feat(renderer): Caddy JSON renderer skeleton"
```

---

## Task 11: Naive/Panels servers, TLS automation, and implicit-listener suppression

**Files:**
- Modify: `internal/renderer/caddyjson.go`
- Test: `internal/renderer/caddyjson_test.go`

**Interfaces:**
- Produces complete JSON with:
  - Panel host-matched reverse_proxy.
  - naive `forward_proxy` + `file_server` without host matcher.
  - TLS automation grouped by `(email, challenge mode)`.
  - `auto_https` `disable_redirects` on every server.

- [ ] **Step 1: Write the failing test**

```go
package renderer

import (
    "testing"

    "github.com/mikkelchokolate/Veil/internal/bindregistry"
    "github.com/mikkelchokolate/Veil/internal/caddyassembly"
    "github.com/mikkelchokolate/Veil/internal/caddycapabilities"
)

func TestRenderCaddyJSONNaiveForwardProxyOrder(t *testing.T) {
    plan := caddyassembly.CaddyRenderPlan{
        Servers: map[bindregistry.BindKey]caddyassembly.CaddyBindOwner{
            {Address: "0.0.0.0", Port: 8443, Network: bindregistry.ListenTCP}: {
                Kind:        caddyassembly.CaddyOwnerNaive,
                Domain:      "p.example.com",
                InboundName: "naive-1",
            },
        },
        Domains: map[string]caddyassembly.CaddyDomainCertSpec{
            "p.example.com": {Domain: "p.example.com", Email: "a@example.com"},
        },
    }
    data, err := RenderCaddyJSON(plan, caddycapabilities.CaddyCapabilities{ForwardProxy: true})
    if err != nil {
        t.Fatal(err)
    }
    s := string(data)
    if !containsInOrder(s, `"forward_proxy"`, `"file_server"`) {
        t.Error("forward_proxy must appear before file_server")
    }
}

func containsInOrder(s, a, b string) bool {
    ia := strings.Index(s, a)
    ib := strings.Index(s, b)
    return ia >= 0 && ib > ia
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/renderer -run TestRenderCaddyJSONNaiveForwardProxyOrder -v
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

Expand `internal/renderer/caddyjson.go`:

```go
func renderServer(key bindregistry.BindKey, owner caddyassembly.CaddyBindOwner, caps caddycapabilities.CaddyCapabilities) map[string]any {
    server := map[string]any{
        "listen": []string{listenString(key)},
        "auto_https": map[string]any{"disable_redirects": true},
    }
    switch owner.Kind {
    case caddyassembly.CaddyOwnerPanel:
        server["routes"] = []map[string]any{
            {
                "match": []map[string]any{{"host": []string{owner.Domain}}},
                "handle": []map[string]any{{
                    "handler": "reverse_proxy",
                    "upstreams": []map[string]any{{"dial": "127.0.0.1:8080"}},
                }},
            },
            {
                "handle": []map[string]any{{"handler": "static_response", "status_code": 404}},
            },
        }
    case caddyassembly.CaddyOwnerNaive:
        handlers := []map[string]any{
            {
                "handler": "forward_proxy",
                "basic_auth": []map[string]any{}, // populated from inbound profiles in Task 15
                "hide_ip": true,
                "hide_via": true,
                "probe_resistance": map[string]any{},
            },
            {
                "handler": "file_server",
                "root": "/var/lib/veil/www",
            },
        }
        server["routes"] = []map[string]any{{"handle": handlers}}
    }
    return server
}
```

Implement TLS automation grouping by email + challenge mode. Add helper `challengeForDomain` that returns the settings' `AcmeChallengeMode`. For `dns-01`, validation already rejects the mode when DNS provider credentials are not configured, so the renderer only needs to support `http-01` and `tls-alpn-01` issuers for the MVP. Emit an `acme` issuer with only the selected challenge type enabled and the others explicitly disabled.

Add `disable_redirects` to all servers and no `:80` listener unless ACME challenge planned.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/renderer -run 'TestRenderCaddyJSONNaiveForwardProxyOrder|TestRenderCaddyJSONPanelOnly' -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/renderer/caddyjson.go internal/renderer/caddyjson_test.go
git commit -m "feat(renderer): Panel/naive servers, TLS automation grouping, no implicit redirects"
```

---

## Task 12: Caddy Admin API client

**Files:**
- Create: `internal/caddyadmin/client.go`
- Test: `internal/caddyadmin/client_test.go`

**Interfaces:**
- Produces:
  ```go
  type Client struct{ AdminEndpoint string }
  func NewClient(endpoint string) Client
  func (c Client) LoadConfig(json []byte) error
  ```

- [ ] **Step 1: Write the failing test**

```go
package caddyadmin

import (
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestLoadConfigPostsJSON(t *testing.T) {
    var received string
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if r.Method != http.MethodPost {
            t.Errorf("method = %s", r.Method)
        }
        if r.URL.Path != "/load" {
            t.Errorf("path = %s", r.URL.Path)
        }
        buf := make([]byte, r.ContentLength)
        r.Body.Read(buf)
        received = string(buf)
        w.WriteHeader(http.StatusOK)
    }))
    defer srv.Close()

    c := NewClient(srv.URL)
    if err := c.LoadConfig([]byte(`{"apps":{}}`)); err != nil {
        t.Fatal(err)
    }
    if received == "" {
        t.Error("server received empty body")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/caddyadmin -run TestLoadConfigPostsJSON -v
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

```go
package caddyadmin

import (
    "bytes"
    "fmt"
    "net/http"
)

type Client struct {
    AdminEndpoint string
    HTTPClient    *http.Client
}

func NewClient(endpoint string) Client {
    if endpoint == "" {
        endpoint = "http://127.0.0.1:2019"
    }
    return Client{AdminEndpoint: endpoint, HTTPClient: http.DefaultClient}
}

func (c Client) LoadConfig(json []byte) error {
    url := c.AdminEndpoint + "/load"
    resp, err := c.HTTPClient.Post(url, "application/json", bytes.NewReader(json))
    if err != nil {
        return fmt.Errorf("caddy admin POST failed: %w", err)
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        return fmt.Errorf("caddy admin returned %s", resp.Status)
    }
    return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/caddyadmin -run TestLoadConfigPostsJSON -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/caddyadmin/client.go internal/caddyadmin/client_test.go
git commit -m "feat(caddyadmin): Admin API client for POST /load"
```

---

## Task 13: Apply plan builder integration

**Files:**
- Modify: `internal/api/apply_plan_builder.go`
- Modify: `internal/generatedconfig/artifact_catalog.go`
- Test: `internal/api/apply_plan_builder_test.go` (extend)

**Interfaces:**
- Consumes: `bindregistry`, `caddyassembly`, `renderer/caddyjson`, `caddycapabilities`.
- Produces: Apply plan includes Caddy JSON artifact and unit reload for single `veil-caddy.service`.

- [ ] **Step 1: Write the failing test**

```go
package api

import (
    "testing"

    "github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildApplyPlanIncludesCaddyJSONArtifact(t *testing.T) {
    settings := model.Settings{
        PanelListen: "0.0.0.0:8080",
        Mode:        "server",
        PanelAccess: "caddy",
        PanelDomain: "panel.example.com",
        PanelEmail:  "admin@example.com",
    }
    inbounds := []model.Inbound{}
    plan := BuildApplyPlan(settings, inbounds, nil)
    if !plan.Valid {
        t.Fatalf("plan invalid: %v", plan.Errors)
    }
    found := false
    for _, c := range plan.Configs {
        if c == "/etc/veil/generated/caddy/config.json" {
            found = true
        }
    }
    if !found {
        t.Error("expected Caddy JSON config artifact in plan")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/api -run TestBuildApplyPlanIncludesCaddyJSONArtifact -v
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

In `internal/generatedconfig/artifact_catalog.go`, add constant and artifact spec:

```go
const CaddyJSONConfigSubpath = "caddy/config.json"
```

And add to `NewArtifactCatalog`:

```go
{Subpath: CaddyJSONConfigSubpath, ValidationName: "caddy", ValidationCommand: func(path string) []string { return []string{"caddy", "validate", "--config", path, "--adapter", "json"} }},
```

In `internal/api/apply_plan_builder.go`, update `BuildApplyPlan` to:

1. Resolve domain cert specs.
2. Build Caddy render plan and global bind owners.
3. Plan ACME challenge binds.
4. Validate no conflicts with `bindregistry.ValidateNoConflicts`.
5. Render Caddy JSON.
6. Add artifact path to plan configs.
7. Add `veil-caddy.service` reload action instead of per-inbound `veil-caddy@` units.

Keep hysteria2 runtime units unchanged.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/api -run TestBuildApplyPlanIncludesCaddyJSONArtifact -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/api/apply_plan_builder.go internal/generatedconfig/artifact_catalog.go internal/api/apply_plan_builder_test.go
git commit -m "feat(api): apply plan builder emits single Caddy JSON artifact"
```

---

## Task 13b: Consolidate Caddy runtime to single `veil-caddy.service`

**Files:**
- Modify: `internal/protocols/naiveproxy/runtime.go`
- Modify: `internal/renderer/systemd.go`
- Modify: `internal/runtimeinstall` package if a Caddy runtime-install descriptor exists
- Test: `internal/protocols/naiveproxy/naiveproxy_test.go`, `internal/renderer/systemd_test.go`

**Interfaces:**
- `RuntimeProvider.RuntimeDescriptors` returns one `service.ManagedRuntime{Name: "veil-caddy.service"}` for all naive inbounds instead of per-inbound `veil-caddy@{name}.service` units.

- [ ] **Step 1: Write the failing test**

```go
package naiveproxy

import (
    "testing"

    "github.com/mikkelchokolate/Veil/internal/model"
)

func TestRuntimeDescriptorsSingleCaddyService(t *testing.T) {
    p := Plugin{}
    inbounds := []model.Inbound{
        {Name: "naive-1", Protocol: "naiveproxy"},
        {Name: "naive-2", Protocol: "naiveproxy"},
    }
    runtimes := p.RuntimeDescriptors(inbounds)
    if len(runtimes) != 1 {
        t.Fatalf("expected 1 Caddy runtime, got %d", len(runtimes))
    }
    if runtimes[0].Name != "veil-caddy.service" {
        t.Errorf("runtime name = %q", runtimes[0].Name)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/protocols/naiveproxy -run TestRuntimeDescriptorsSingleCaddyService -v
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

In `internal/protocols/naiveproxy/runtime.go`, replace the per-inbound loop with:

```go
func (p Plugin) RuntimeDescriptors(enabledInbounds []model.Inbound) []service.ManagedRuntime {
    for _, inb := range enabledInbounds {
        if inb.Protocol == "naiveproxy" {
            return []service.ManagedRuntime{{Name: "veil-caddy.service"}}
        }
    }
    return nil
}
```

In `internal/renderer/systemd.go`, remove any code that renders `veil-caddy@.service` template units and ensure `veil-caddy.service` is rendered once. Update `systemd_test.go` expectations.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/protocols/naiveproxy -run TestRuntimeDescriptorsSingleCaddyService -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/protocols/naiveproxy/runtime.go internal/renderer/systemd.go internal/protocols/naiveproxy/naiveproxy_test.go internal/renderer/systemd_test.go
git commit -m "feat(runtime): single veil-caddy.service for all naive inbounds"
```

## Task 14: Apply execution — Admin API load, cert sync, rollback

**Files:**
- Modify: `internal/api/management_apply_context.go`
- Modify: `internal/privileged/types.go` and `internal/privileged/client.go` (add `LoadCaddyConfig` op if needed)
- Test: `internal/api/management_apply_context_test.go` (extend)

**Interfaces:**
- Consumes: Caddy JSON artifact, `caddyadmin.Client`.
- Produces: After service reload, call Admin API `POST /load`; on failure restore previous config and reload.

- [ ] **Step 1: Write the failing test**

```go
package api

import (
    "testing"

    "github.com/mikkelchokolate/Veil/internal/model"
)

func TestManagementApplyContextCaddyAdminLoadCalled(t *testing.T) {
    // Use a test double State that records whether CaddyAdminLoad was invoked.
    // This is an integration-style test; start with the minimal assertion that
    // the context routes Caddy JSON live files to the admin loader.
    ctx := NewTestApplyContext()
    resp, err := ctx.ApplyLocked(model.ApplyRequest{Confirm: true, ApplyLive: true, ApplyServices: true})
    if err != nil {
        t.Fatal(err)
    }
    if !resp.Applied {
        t.Fatal("apply did not run")
    }
    if !ctx.CaddyAdminLoadCalled {
        t.Error("Caddy Admin API load was not invoked")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/api -run TestManagementApplyContextCaddyAdminLoadCalled -v
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

In `internal/api/management_apply_context.go`, after `ReloadPromotedServicesLocked` for `veil-caddy.service`, detect the Caddy JSON live path and call the Admin API loader (via `internal/caddyadmin`). Save the previous config before loading; on Admin API failure, write previous config back and reload.

For hysteria2 cert sync: after successful Caddy load, iterate hysteria2 domains from `CaddyRenderPlan.Domains`, call `internal/caddycert.Sync` for each, then reload affected hysteria2 units.

Use the existing rollback path for promotion failures.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/api -run TestManagementApplyContextCaddyAdminLoadCalled -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/api/management_apply_context.go internal/privileged/types.go internal/privileged/client.go internal/api/management_apply_context_test.go
git commit -m "feat(api): load Caddy JSON via Admin API, sync hysteria2 certs, rollback on failure"
```

---

## Task 15: Hysteria2 cert sync polling

**Files:**
- Modify: `internal/caddycert/caddycert.go`
- Test: `internal/caddycert/caddycert_test.go` (create if missing)

**Interfaces:**
- Produces: `Sync(domain, sourceDir, targetDir string) error` that polls Caddy storage until cert exists, then atomically copies cert/key.

- [ ] **Step 1: Write the failing test**

```go
package caddycert

import (
    "os"
    "path/filepath"
    "testing"
)

func TestSyncCopiesCertificate(t *testing.T) {
    src := t.TempDir()
    dst := t.TempDir()
    if err := os.WriteFile(filepath.Join(src, "example.com.crt"), []byte("CERT"), 0644); err != nil {
        t.Fatal(err)
    }
    if err := os.WriteFile(filepath.Join(src, "example.com.key"), []byte("KEY"), 0600); err != nil {
        t.Fatal(err)
    }
    if err := Sync("example.com", src, dst); err != nil {
        t.Fatal(err)
    }
    if _, err := os.Stat(filepath.Join(dst, "example.com.crt")); err != nil {
        t.Fatal(err)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/caddycert -run TestSyncCopiesCertificate -v
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

```go
package caddycert

import (
    "fmt"
    "os"
    "path/filepath"
    "time"
)

func Sync(domain, sourceDir, targetDir string) error {
    crtSrc := filepath.Join(sourceDir, domain+".crt")
    keySrc := filepath.Join(sourceDir, domain+".key")
    crtDst := filepath.Join(targetDir, domain+".crt")
    keyDst := filepath.Join(targetDir, domain+".key")

    deadline := time.Now().Add(120 * time.Second)
    for time.Now().Before(deadline) {
        if exists(crtSrc) && exists(keySrc) {
            break
        }
        time.Sleep(2 * time.Second)
    }
    if !exists(crtSrc) || !exists(keySrc) {
        return fmt.Errorf("certificate for %s not issued within timeout", domain)
    }

    tmpCrt := crtDst + ".tmp"
    tmpKey := keyDst + ".tmp"
    if err := copyFile(crtSrc, tmpCrt); err != nil {
        return err
    }
    if err := copyFile(keySrc, tmpKey); err != nil {
        return err
    }
    if err := os.Rename(tmpCrt, crtDst); err != nil {
        return err
    }
    return os.Rename(tmpKey, keyDst)
}

func exists(path string) bool {
    _, err := os.Stat(path)
    return err == nil
}

func copyFile(src, dst string) error {
    data, err := os.ReadFile(src)
    if err != nil {
        return err
    }
    return os.WriteFile(dst, data, 0600)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/caddycert -run TestSyncCopiesCertificate -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/caddycert/caddycert.go internal/caddycert/caddycert_test.go
git commit -m "feat(caddycert): poll and atomically sync Caddy-managed certs"
```

---

## Task 16: Legacy migration wizard

**Files:**
- Create: `internal/inbounds/legacy_migration.go`
- Test: `internal/inbounds/legacy_migration_test.go`

**Interfaces:**
- Produces:
  ```go
  type LegacyState string
  func DetectLegacyInbounds(inbounds []model.Inbound) []model.Inbound
  func CanCreateManagedNaive(inbounds []model.Inbound) bool
  func SuggestMigration(settings model.Settings, inbounds []model.Inbound) ([]model.Inbound, error)
  ```

- [ ] **Step 1: Write the failing test**

```go
package inbounds

import (
    "testing"

    "github.com/mikkelchokolate/Veil/internal/model"
)

func TestDetectLegacyInbound(t *testing.T) {
    inbounds := []model.Inbound{
        {Name: "old", Protocol: "naiveproxy", Port: 443, ProtocolFields: map[string]any{}},
    }
    legacy := DetectLegacyInbounds(inbounds)
    if len(legacy) != 1 {
        t.Fatalf("expected 1 legacy inbound, got %d", len(legacy))
    }
}

func TestBlockManagedUntilLegacyResolved(t *testing.T) {
    inbounds := []model.Inbound{
        {Name: "old", Protocol: "naiveproxy", Port: 443, ProtocolFields: map[string]any{}},
    }
    if CanCreateManagedNaive(inbounds) {
        t.Error("creating managed naive inbounds must be blocked while legacy exists")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/inbounds -run 'TestDetectLegacyInbound|TestBlockManagedUntilLegacyResolved' -v
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

```go
package inbounds

import "github.com/mikkelchokolate/Veil/internal/model"

func DetectLegacyInbounds(inbounds []model.Inbound) []model.Inbound {
    var out []model.Inbound
    for _, inb := range inbounds {
        if inb.Protocol != "naiveproxy" {
            continue
        }
        if inb.ProtocolFields == nil || inb.ProtocolFields["domain"] == nil || inb.ProtocolFields["domain"] == "" {
            out = append(out, inb)
        }
    }
    return out
}

func CanCreateManagedNaive(inbounds []model.Inbound) bool {
    return len(DetectLegacyInbounds(inbounds)) == 0
}

func SuggestMigration(settings model.Settings, inbounds []model.Inbound) ([]model.Inbound, error) {
    // Populate domain/publicPort/transport/email; require admin review before applying.
    // Implementation in Task 19 UI/API task.
    return inbounds, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/inbounds -run 'TestDetectLegacyInbound|TestBlockManagedUntilLegacyResolved' -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/inbounds/legacy_migration.go internal/inbounds/legacy_migration_test.go
git commit -m "feat(inbounds): legacy naive inbound detection and migration gate"
```

---

## Task 17: Panel mode switch and settings field schema

**Files:**
- Modify: `internal/settings/settings_validation.go`
- Modify: `internal/panelaccess/profile.go`
- Modify: `internal/protocols/naiveproxy/ui.go`
- Test: existing tests updated

**Interfaces:**
- `PanelAccess` switch in settings UI exposes `caddy` fields domain, email, public port.
- `panelaccess.Profile` uses `PanelPublicPort` for Panel Caddy bind.

- [ ] **Step 1: Write the failing test**

```go
package panelaccess

import (
    "testing"

    "github.com/mikkelchokolate/Veil/internal/model"
)

func TestProfileUsesPanelPublicPort(t *testing.T) {
    settings := model.Settings{
        PanelAccess: "caddy",
        PanelDomain: "panel.example.com",
        PanelEmail:  "a@example.com",
        PanelPublicPort: 8443,
    }
    profile, err := BuildProfile(settings)
    if err != nil {
        t.Fatal(err)
    }
    if profile.PublicPort != 8443 {
        t.Errorf("public port = %d", profile.PublicPort)
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/panelaccess -run TestProfileUsesPanelPublicPort -v
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

In `internal/panelaccess/profile.go`, update `Profile` struct and builder to read `PanelPublicPort`. Default to 443 if zero.

In `internal/settings/settings_validation.go`, ensure `caddy` mode requires `PanelDomain` and `PanelEmail` (already partially does; update field names if changed from legacy `Domain`/`Email`). Keep legacy `Domain`/`Email` as fallback.

In `internal/protocols/naiveproxy/ui.go`, update settings field schema to expose new settings fields.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/panelaccess -run TestProfileUsesPanelPublicPort -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/panelaccess/profile.go internal/settings/settings_validation.go internal/protocols/naiveproxy/ui.go internal/panelaccess/profile_test.go
git commit -m "feat(panelaccess,settings,ui): PanelPublicPort and caddy mode fields"
```

---

## Task 18: Client export for naive transport

**Files:**
- Modify: `internal/protocols/naiveproxy/client_access.go`
- Test: `internal/protocols/naiveproxy/client_access_test.go` (extend)

**Interfaces:**
- Produces `https://` and/or `quic://` URIs based on `transport`, omitting default port.

- [ ] **Step 1: Write the failing test**

```go
package naiveproxy

import (
    "testing"

    "github.com/mikkelchokolate/Veil/internal/model"
)

func TestBuildLinksDual(t *testing.T) {
    settings := model.Settings{DefaultInboundPublicPort: 443}
    inbound := model.Inbound{
        Protocol: "naiveproxy",
        Profiles: []model.ClientProfile{{Username: "u", Password: "p"}},
        ProtocolFields: map[string]any{"domain": "p.example.com", "transport": "dual"},
    }
    links, err := BuildLinks(settings, inbound)
    if err != nil {
        t.Fatal(err)
    }
    if len(links) != 2 {
        t.Fatalf("expected 2 links, got %d", len(links))
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /root/Veil
go test ./internal/protocols/naiveproxy -run TestBuildLinksDual -v
```

Expected: FAIL.

- [ ] **Step 3: Write minimal implementation**

```go
func BuildLinks(settings model.Settings, inbound model.Inbound) ([]model.ClientLink, error) {
    domain := NaiveDomain(inbound)
    port := NaivePublicPort(settings, inbound)
    transport := NaiveTransport(inbound)
    creds := inbound.Profiles
    if len(creds) == 0 {
        return nil, errors.New("no profiles")
    }
    var links []model.ClientLink
    for _, profile := range creds {
        if transport == "tcp" || transport == "dual" {
            links = append(links, model.ClientLink{
                Name:     inbound.Name + "-https",
                Protocol: "naiveproxy",
                Transport: "tcp",
                Port:     port,
                URI:      naiveURI("https", profile.Username, profile.Password, domain, port, 443),
            })
        }
        if transport == "quic" || transport == "dual" {
            links = append(links, model.ClientLink{
                Name:     inbound.Name + "-quic",
                Protocol: "naiveproxy",
                Transport: "quic",
                Port:     port,
                URI:      naiveURI("quic", profile.Username, profile.Password, domain, port, 443),
            })
        }
    }
    return links, nil
}

func naiveURI(scheme, user, pass, domain string, port, defaultPort int) string {
    host := domain
    if port != defaultPort {
        host = fmt.Sprintf("%s:%d", domain, port)
    }
    return fmt.Sprintf("%s://%s:%s@%s", scheme, user, pass, host)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./internal/protocols/naiveproxy -run TestBuildLinksDual -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add internal/protocols/naiveproxy/client_access.go internal/protocols/naiveproxy/client_access_test.go
git commit -m "feat(naiveproxy): client export per transport (https/quic)"
```

---

## Task 19: End-to-end naive create/delete scenario

**Files:**
- Modify: `test/e2e/management_flow_test.go`
- Test: run e2e suite

**Interfaces:**
- Creates a naive inbound via management API, asserts Caddy JSON contains forward_proxy, deletes inbound, asserts cleanup.

- [ ] **Step 1: Write the failing test**

```go
package e2e

import (
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestNaiveInboundCaddyJSON(t *testing.T) {
    srv := startServer(t, serverOptions{token: "e2e-secret-token"})

    resp := srv.do(http.MethodPut, "/api/settings", `{"panelListen":"127.0.0.1:2096","mode":"dev","panelAccess":"direct","defaultAcmeEmail":"admin@example.com","defaultInboundPublicPort":443}`)
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("settings expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
    }
    drain(resp)

    resp = srv.do(http.MethodPost, "/api/inbounds", `{"name":"naive-tcp","protocol":"naiveproxy","transport":"tcp","port":8443,"enabled":true,"protocolFields":{"domain":"proxy.example.com","transport":"tcp","publicPort":8443},"profiles":[{"name":"alice","username":"alice","password":"alice-pass","enabled":true}]}`)
    if resp.StatusCode != http.StatusCreated {
        t.Fatalf("inbound expected 201, got %d: %v", resp.StatusCode, readJSON(t, resp))
    }
    drain(resp)

    resp = srv.do(http.MethodPost, "/api/apply/plan", "")
    if resp.StatusCode != http.StatusOK {
        t.Fatalf("apply plan expected 200, got %d: %v", resp.StatusCode, readJSON(t, resp))
    }
    planBody := readJSON(t, resp)
    if valid, ok := planBody["valid"].(bool); !ok || !valid {
        t.Fatalf("expected valid plan, got %v", planBody)
    }
    drain(resp)

    resp = srv.do(http.MethodPost, "/api/apply", `{"confirm":true}`)
    if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
        t.Fatalf("apply expected 200/409, got %d: %v", resp.StatusCode, readJSON(t, resp))
    }
    drain(resp)

    caddyJSON := filepath.Join(srv.applyRoot, "generated", "caddy", "config.json")
    data, err := os.ReadFile(caddyJSON)
    if err != nil {
        t.Fatalf("expected Caddy JSON at %s: %v", caddyJSON, err)
    }
    s := string(data)
    if !strings.Contains(s, "forward_proxy") {
        t.Error("Caddy JSON missing forward_proxy handler")
    }
    if !strings.Contains(s, "proxy.example.com") {
        t.Error("Caddy JSON missing naive domain")
    }
}
```

- [ ] **Step 2: Run test to verify it fails/skips**

```bash
cd /root/Veil
go test ./test/e2e -run TestNaiveInboundCaddyJSON -v -short
```

Expected: SKIP.

- [ ] **Step 3: Write minimal implementation**

Add the test body from Step 1 to `test/e2e/management_flow_test.go` after the existing tests. The test reuses the `startServer`, `srv.do`, `readJSON`, `drain`, and `srv.applyRoot` helpers from `harness_test.go`.

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /root/Veil
go test ./test/e2e -run TestNaiveInboundCaddyJSON -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /root/Veil
git add test/e2e/management_flow_test.go
git commit -m "test(e2e): naive inbound create/delete Caddy JSON scenario"
```

---

## Self-Review Checklist

- [ ] **Spec coverage:** Every section of `2026-07-04-inbound-caddy-redesign-design.md` maps to at least one task above.
- [ ] **Placeholder scan:** No `TBD`, `TODO`, `implement later`, or `fill in details` remain.
- [ ] **Type consistency:** `BindKey`, `CaddyRenderPlan`, `CaddyDomainCertSpec`, `AcmeChallengeOwner`, and `CaddyCapabilities` names and field types match across tasks.
- [ ] **CI:** Run `go test ./...` before final commit of each task.

---

## Execution Handoff

**Plan complete and saved to `docs/superpowers/plans/2026-07-04-inbound-caddy-redesign.md`. Two execution options:**

**1. Subagent-Driven (recommended)** — dispatch a fresh subagent per task, review between tasks, fast iteration.

**2. Inline Execution** — execute tasks in this session using `executing-plans`, batch execution with checkpoints.

**Which approach?**
