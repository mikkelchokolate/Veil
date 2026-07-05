# Inbound/Caddy Redesign Design

## Goal

Make naiveproxy and hysteria2 inbounds intuitive to deploy with per-inbound
public domains while decoupling the Panel's own exposure mode from inbound
Caddy instances.

After this redesign:

- A naive inbound is created with its own `domain`, `email`, credentials,
  optional public port (default `Settings.DefaultInboundPublicPort`, itself 443),
  and transport (`tcp`, `quic`, or `dual`). Caddy is configured automatically
  for that domain, port, and transport.
- A naive inbound owns one or more bind keys derived from its transport:
  `tcp` owns TCP `publicPort`, `quic` owns UDP `publicPort`, `dual` owns both.
  Two naive inbounds cannot own the same `(address, publicPort, protocol)` bind
  key. The same numeric public port may be reused only when the network
  protocols do not overlap.
- The domain on a naive inbound is used for TLS/SNI, ACME certificate issuance,
  and client export. The same domain may be used on multiple naive inbounds as
  long as their bind keys do not conflict.
- The Panel can stay in `direct` mode while the same single Caddy process serves
  naive inbounds on their own ports.
- Panel mode (`local`/`direct`/`caddy`) can be switched from the web UI. When
  `caddy` is selected, the admin enters a domain, email, and public port; other
  modes need no extra input.
- When an inbound is deleted, its Caddy server is removed. Caddy domain/certificate
  ownership is removed only when no naive inbound, no Panel site, and no
  hysteria2 inbound reference it.
- The Panel's own Caddy site is never removed accidentally: a domain owned by
  the Panel is protected until Panel mode is changed away from `caddy`.
- hysteria2 inbounds may reference a domain and reuse the ACME certificate that
  Caddy obtains for that domain, without adding a Caddy site block.

## Current State & Pain Points

- `PanelAccess` (`local`/`direct`/`caddy`) is stored in `model.Settings` but the
  mode switch is not atomic and is only reconciled on the next Apply.
- Naive inbounds today require a global domain/email in settings. There is no
  per-inbound domain ownership.
- Each naive inbound gets its own `veil-caddy@{inbound.Name}.service`. This is
  resource-heavy and makes shared TLS/ACME state hard to reason about.
- Panel access through Caddy is implemented by injecting a Panel route into a
  naive inbound's Caddyfile. No naive inbound means a separate
  `panel.Caddyfile`.
- hysteria2 reuses Caddy's ACME cert only when `PanelAccess == "caddy"`. In
  `direct` mode there is no clean way for hysteria2 to obtain a domain
  certificate.
- Validation is split across `inbounds`, `settings`, `livevalidation`, and
  protocol plugins. There is no unified "inbound with domain" validation path.

## Design

### Ownership Model

There is **one physical Caddy process** (`veil-caddy.service`) that serves all
Caddy-managed endpoints. Ownership is split into two layers.

#### Bind-level endpoint ownership (routing)

A physical listener is identified by its bind key: `(address, port, network
protocol)`. Veil maintains one global bind registry that knows every owner of
every bind key across all services:

```go
type ListenNetwork string

const (
    ListenTCP ListenNetwork = "tcp"
    ListenUDP ListenNetwork = "udp"
)

type BindKey struct {
    Address string        // e.g. "0.0.0.0" or "::"
    Port    int
    Network ListenNetwork // tcp | udp
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
    ServiceName string // e.g. "veil.service", "veil-caddy.service"
    InboundName string // populated for naive/hysteria2 inbounds
}
```

The Caddy renderer consumes a subset of the global registry:

```go
type CaddyBindOwnerKind string

const (
    CaddyOwnerPanel CaddyBindOwnerKind = "panel"
    CaddyOwnerNaive CaddyBindOwnerKind = "naive"
)

type CaddyBindOwner struct {
    Kind        CaddyBindOwnerKind
    Domain      string // TLS/SNI identity; for Panel this is also the host route
    InboundName string // only populated when Kind == CaddyOwnerNaive
}
```

- A naive inbound owns one or two bind keys for a single `publicPort`,
  depending on `transport`:
  - `tcp`  → owns TCP `(0.0.0.0, publicPort, tcp)`.
  - `quic` → owns UDP `(0.0.0.0, publicPort, udp)`.
  - `dual` → owns TCP and UDP binds on `publicPort`.
- Panel in `caddy` mode owns TCP `(0.0.0.0, PanelPublicPort, tcp)`.
- A bind key can be owned by only one service. TCP and UDP with the same
  numeric port do not conflict unless one inbound owns both.
- Panel and naive endpoints cannot share a TCP bind key.
- Validation errors name the conflicting owner, e.g. "Cannot switch naive
  inbound to QUIC: UDP :443 is already used by hysteria2 inbound 'hy-main'."

##### Bind address normalization

Veil must normalize public bind addresses before conflict checks. For MVP, all
public Caddy, naive, hysteria2, and Panel binds are treated as wildcard public
binds unless a future feature explicitly supports per-interface binding.

Conflict rules for normalized addresses:

- Wildcard IPv4 `0.0.0.0` conflicts with any IPv4 address on the same port and
  protocol.
- Wildcard IPv6 `::` conflicts with any IPv6 address on the same port and
  protocol.
- If the runtime uses dual-stack IPv6 sockets (`IPV6_V6ONLY=0`), the IPv6
  wildcard also conflicts with IPv4 wildcard and specific IPv4 binds.
- A specific address conflicts only when its normalized address, port, and
  protocol overlap with another specific address.
- Legacy runtime binds must be normalized into the same model before migration
  checks.

The `BindKey` struct stores the normalized canonical form. Address parsing must
reject invalid values and collapse equivalent wildcard representations.

A naive inbound's Caddy HTTP/3 server (for `quic`/`dual`) uses the same
`forward_proxy` handler and `file_server` fallback as the TCP server, but
listens on the UDP protocol. Both servers share the same domain, certificate,
and profiles.

#### Certificate ownership (domain lifecycle)

TLS certificates are managed per domain, independent of ports:

```go
type CaddyDomainOwners struct {
    Panel                bool     // PanelAccess == "caddy" and PanelDomain == domain
    NaiveInboundNames    []string // naive inbounds using this domain on any public port
    HysteriaInboundNames []string // hysteria2 inbounds using this domain
}

type CaddyDomainCertSpec struct {
    Domain string
    Email  string // resolved once per domain
    Owners CaddyDomainOwners
}
```

A domain is eligible for certificate acquisition when `Panel` is true or any
naive/hysteria2 inbound references it. When no owner remains, Caddy stops
managing the certificate for that domain.

Because certificates are per-domain, the ACME email is also resolved **once per
domain**. If multiple owners of the same domain (Panel site, naive inbounds, or
hysteria2 inbounds) provide conflicting explicit emails, the configuration is
rejected. The resolved email is stored in `CaddyDomainCertSpec.Email` and used
for all certificate operations for that domain.

The Panel's own Caddy site is protected by the `Panel` ownership bit: it cannot
be removed by deleting inbounds.

### Data Model Changes

#### Settings

```go
type Settings struct {
    // ... existing fields ...

    PanelAccess            string `json:"panelAccess"`            // local | direct | caddy
    PanelDomain            string `json:"panelDomain"`            // required when PanelAccess == "caddy"
    PanelEmail             string `json:"panelEmail"`             // required when PanelAccess == "caddy"
    PanelPublicPort        int    `json:"panelPublicPort"`        // default 443; external port for Panel in caddy mode
    DefaultAcmeEmail       string `json:"defaultAcmeEmail"`       // fallback ACME email for inbounds
    DefaultInboundPublicPort int  `json:"defaultInboundPublicPort"` // default 443; default for new naive inbound publicPort
    AcmeChallengeMode      string `json:"acmeChallengeMode"`      // http-01 | tls-alpn-01 | dns-01 (dns-01 reserved until DNS provider credentials are configured)
}
```

- `PanelDomain`/`PanelEmail` are validated only when `PanelAccess == "caddy"`.
- When `PanelAccess` is `local` or `direct`, these fields may be empty.
- `Mode` field is deprecated; `PanelAccess` becomes the canonical exposure
  switch.

#### Inbound ProtocolFields

For `naiveproxy`:

```json
{
  "domain":       "proxy.example.com",
  "email":        "admin@example.com",
  "publicPort":   443,
  "transport":    "tcp",
  "fallbackRoot": "/var/www/fallback"
}
```

- `domain` is required for naiveproxy. It is used for TLS/SNI, ACME certificate
  issuance, and client export.
- `email` is optional. It is the explicit ACME contact for this inbound's
  domain. Domain-level email resolution is:
  1. collect all explicit emails from every owner of the same domain (Panel
     site, naive inbounds, hysteria2 inbounds);
  2. if more than one distinct explicit email exists → reject the change;
  3. otherwise use the explicit email;
  4. else fall back to `Settings.DefaultAcmeEmail`;
  5. else fall back to `Settings.PanelEmail` if present;
  6. else reject creation.
  The UI for a second naive inbound on an already-owned domain shows the
  inherited resolved email and warns before allowing a conflicting override.
- `publicPort` is the port Caddy listens on for this inbound. It defaults to
  `Settings.DefaultInboundPublicPort` (itself defaulting to 443).
- `transport` controls the network protocol(s) the inbound owns. Allowed values:
  - `tcp`  — HTTPS/H2 over TCP. Owns a TCP bind on `publicPort`.
  - `quic` — HTTP/3/QUIC over UDP. Owns a UDP bind on `publicPort`.
  - `dual` — both HTTPS/H2 and HTTP/3/QUIC on `publicPort`. Owns TCP and UDP
    binds.
  The default is `tcp`.
- A naive inbound owns a set of bind keys `(address, publicPort, protocol)`
  derived from its transport. Two inbounds cannot own the same bind key. The
  Panel endpoint owns TCP `(address, PanelPublicPort, tcp)` and cannot share
  that bind key with a naive inbound.
- Naive credentials are sourced from `inbound.Profiles` (username/password
  pairs), matching the existing profile-based model. Multiple user groups on the
  same naive public port must be represented as profiles inside one naive
  inbound, not as separate physical inbounds.
- `fallbackRoot` is optional and defaults to a panel-managed static directory.
- The inbound's `Port` field continues to hold the public listen port; keeping
  `publicPort` in `ProtocolFields` makes the value explicit for the new model.

For `hysteria2`:

```json
{
  "domain": "hy.example.com",
  "insecure": false
}
```

- `domain` is optional. When present, hysteria2 uses the Caddy-obtained ACME
  certificate for that domain.
- `insecure` keeps its current meaning for testing.

#### Client Export

Naive client export depends on `transport`:

- `tcp` exports an HTTPS/H2 endpoint:
  `https://user:pass@domain[:publicPort]`
- `quic` exports a QUIC/HTTP3 endpoint:
  `quic://user:pass@domain[:publicPort]`
- `dual` exports both endpoints for the same profile:
  `https://user:pass@domain[:publicPort]` and
  `quic://user:pass@domain[:publicPort]`

The port is omitted only when it is the default port for the exported scheme
(443 for `https`, 443/UDP for `quic` in NaiveProxy clients); otherwise it is
included explicitly.

### Caddy Lifecycle

#### Unit

Replace the template unit `veil-caddy@.service` with a single
`veil-caddy.service`. The service runs one Caddy process.

Veil generates Caddy's native **JSON config** and stores it at
`/etc/veil/generated/caddy/config.json`. The JSON config is loaded into Caddy
via its Admin API (`POST /load`). A Caddyfile rendering may be kept as a
debug/export artifact, but it is not the authoritative runtime config.

The Caddy Admin API must listen only on `127.0.0.1` or a Unix socket and must
not be publicly exposed. Only Veil's privileged apply component may call
`POST /load`.

#### Caddy Binary / Capability Requirement

The `veil-caddy` binary must include the naive `forward_proxy` module
(`github.com/klzgrad/forwardproxy` for the naive branch). Veil's installer is
responsible for building or installing a Caddy binary that contains this
module.

At startup and before every Apply that includes naive inbounds, Veil probes the
Caddy binary and reports the following capabilities:

```go
type CaddyCapabilities struct {
    ForwardProxy bool // http.handlers.forward_proxy is available
    HTTP3        bool // Caddy can serve HTTP/3
    H3Only       bool // Caddy can serve HTTP/3 without also opening a TCP listener
}
```

- `ForwardProxy` must be true for any naive inbound. If missing, Apply fails
  with a clear error telling the operator that the custom Caddy build is
  required.
- `HTTP3` must be true for `transport = dual`.
- `H3Only` must be true for `transport = quic`. If `H3Only` is false, the API
  rejects `quic` transport and the UI hides it; only `tcp` and `dual` are
  exposed.

The capability values are discovered once at startup and refreshed before each
Apply. `H3Only` must be verified by loading a temporary safe Caddy config and
confirming that the UDP listener is opened without opening the corresponding
TCP listener; module presence alone is not sufficient. The capabilities are
surfaced in the UI so the operator knows which transport modes are available.

#### Caddy Config Assembly

A new package `internal/renderer/caddyassembly` (or an expanded
`internal/renderer/caddy.go`) produces the merged Caddy JSON config:

1. Build the global `map[BindKey]BindOwner` registry from settings, inbounds,
   legacy runtimes, and implicit ACME challenge listeners.
2. Derive the Caddy renderer plan from the global bind registry. The plan
   includes:
   - `Servers` — Caddy-served Panel and naive listeners.
   - `ACMEChallenges` — implicit `BindOwnerAcmeChallenge` listeners that Caddy
     must open for `http-01` (TCP :80) or `tls-alpn-01` (TCP :443).
   - `Domains` — `map[string]CaddyDomainCertSpec` for certificate automation.

   ```go
   type CaddyRenderPlan struct {
       Servers        map[BindKey]CaddyBindOwner
       ACMEChallenges map[BindKey]AcmeChallengeOwner
       Domains        map[string]CaddyDomainCertSpec
   }

   type AcmeChallengeOwner struct {
       ChallengeMode string   // http-01 | tls-alpn-01
       Domains       []string // domains that require this challenge bind
   }
   ```
3. Build `map[domain]CaddyDomainOwners` independently from settings and all
   inbounds, not from the Caddy bind subset. This map must include:
   - Panel domain when `PanelAccess == "caddy"`;
   - all naive domains;
   - all hysteria2 domains.
4. The Caddy renderer may group bind keys into the correct Caddy JSON server
   shape. For each owned bind key (or grouped set), emit the corresponding
   Caddy listener. ACME challenge listeners are rendered as dedicated challenge
   handlers, not as user-facing sites:
   - If `Owner.Kind == CaddyOwnerPanel`: emit a TCP HTTP server that host-matches
     `Owner.Domain` and reverse-proxies to the Panel loopback listener; fallback
     responds with 404.
   - If `Owner.Kind == CaddyOwnerNaive`: emit a TCP HTTP server, a UDP HTTP/3
     server, or both (depending on `transport`) on the public port. Each naive
     server has no host matcher and its handlers are ordered `forward_proxy`
     first, then `file_server` fallback using `fallbackRoot`. The
     `forward_proxy` handler uses the inbound's users (`basic_auth`, `hide_ip`,
     `hide_via`, `probe_resistance`). Certificate selection is SNI-based from
     Caddy's managed certificate cache. Veil explicitly sets Caddy server
     protocols per transport:
     - `tcp`  → enable HTTP/1.1 and HTTP/2 over TCP; do not enable HTTP/3.
     - `quic` → enable HTTP/3 over UDP only, if Caddy JSON supports an H3-only
       listener.
     - `dual` → enable HTTP/1.1 and HTTP/2 over TCP and HTTP/3 over UDP.
     Before release, an implementation spike must verify that Caddy JSON can
     represent a QUIC-only naive server without also opening a TCP listener. If
     H3-only cannot be represented safely, `quic` transport is disabled and only
     `tcp`/`dual` are exposed in the UI.
5. Build Caddy TLS automation policies from the `CaddyDomainCertSpec` values.
   Domains are grouped by ACME issuer configuration: resolved email, challenge
   mode, and future DNS provider configuration when DNS-01 is enabled. For each
   group, emit one `apps.tls.automation.policies[]` entry whose `subjects` lists
   all domains in the group and whose issuer is configured with the group's
   email and the selected challenge mode only. Caddy must not be allowed to fall
   back to an unplanned challenge type (e.g. HTTP-01 when DNS-01 was selected),
   because unplanned challenge types could open listeners that are not in the
   `BindKey` registry. A domain appears in exactly one automation policy. This
   explicitly tells Caddy which certificates to obtain even when the HTTP server
   has no host matcher (e.g. a naive `forward_proxy` port) or when the domain is
   used only for hysteria2 certificate sync. This is a certificate-only entry
   for hysteria2-only domains; for Panel and naive domains it augments the
   normal automatic HTTPS behavior.
6. Hysteria2 contributes to a post-apply certificate sync list regardless of
   how the certificate was obtained.

Because naive inbounds do not use host matching, the generated JSON for a naive
port must not contain host matchers for that server.

#### Caddy implicit listener policy

Veil must not allow Caddy to create public listeners that are not represented in
the global `BindKey` registry. All public TCP/UDP binds must be explicit:

- Panel direct / Panel Caddy binds;
- naive TCP/UDP binds;
- hysteria2 UDP binds;
- legacy Caddy binds;
- implicit ACME challenge binds.

Caddy's automatic HTTP-to-HTTPS redirects must be disabled unless Veil
explicitly models them as bind owners in the global registry. The generated JSON
must set `auto_https` options (e.g. `disable_redirects`) so that Caddy does not
open an unmanaged TCP `:80` listener for redirects. Certificate automation may
still be configured through TLS automation policies, but no implicit public
`:80` or `:443` listener may appear outside the render plan.

If an operator wants a managed HTTP-to-HTTPS redirect on `:80`, that must be
introduced later as an explicit `BindOwnerRedirect` entry with its own bind-key
validation, not as a side effect of Caddy Automatic HTTPS.

#### Non-Standard Port Warning

When a naive inbound uses `publicPort != 443`, the UI shows a warning that the
admin must ensure firewall/NAT rules allow client traffic to that port and that
NaiveProxy clients support the non-standard port. Certificate issuance may still
require HTTP-01 (TCP :80), TLS-ALPN-01 (TCP :443), or DNS-01.
`AcmeChallengeMode` controls the challenge type.

For `quic`-only naive inbounds, the UI also warns that certificate issuance may
still require a TCP challenge path (HTTP-01 on :80 or TLS-ALPN-01 on :443) or
DNS-01, even though client traffic uses UDP. If no TCP challenge path is
available, DNS-01 must be configured.

#### Certificate Sync for hysteria2

After Caddy reloads, the apply flow triggers certificate synchronization for
every unique hysteria2 domain:

1. Poll Caddy storage until the certificate for the domain exists or a timeout
   elapses (default 120s).
2. Atomically copy the certificate/key pair to `/etc/veil/certs/{domain}.{crt,key}`.
3. Reload/restart the affected hysteria2 service.

If the certificate is not issued before the timeout, the Apply marks the
inbound as "certificate pending" and surfaces a clear error instead of leaving
hysteria2 unable to start.

The sync is independent of `PanelAccess`: it runs for any hysteria2 inbound
with a non-empty domain.

### Panel Mode Switching

The web UI settings page exposes a "Panel Access" section:

- Dropdown: `local`, `direct`, `caddy`.
- When `caddy` is selected, extra inputs appear: Domain, Email, and Public Port.
  Domain and Email are required; Public Port defaults to 443.
- When `local` or `direct` is selected, the inputs are hidden and values are
  cleared or ignored.

The API endpoint `PUT /api/settings` validates the new mode immediately:

- `caddy` requires non-empty `PanelDomain` and `PanelEmail`, and a valid
  `PanelPublicPort`.
- No other mode imposes domain/email/port requirements.

Changing the mode only mutates state. The actual network reconfiguration
happens when the user confirms Apply.

### Apply & Rollback on Mode Switch

Mode switch Apply uses the existing apply flow with an extended health check
phase:

1. Stage the new settings, Caddy JSON config, and service units.
2. Promote staged files to live paths.
3. For Caddy config changes, load the new JSON config via Caddy's Admin API
   (`POST /load`). Restart `veil-caddy.service` only when the unit, binary,
   environment, or storage path changes.
4. If Panel listener or TLS source changed, restart `veil.service`.
5. Health check (scoped to the change):
   - Create/delete naive inbound: probe the affected bind key(s) for TLS/QUIC
     accept, valid certificate handshake, and a response from `fallbackRoot` on
     `GET /`:
     - `tcp`  — TLS handshake over TCP, HTTP/2 `GET /` fallbackRoot.
     - `quic` — QUIC/TLS handshake over UDP, HTTP/3 `GET /` fallbackRoot.
     - `dual` — run both TCP and QUIC checks.
   - Panel mode switch: probe the Panel on its new expected URL and all Caddy
     endpoints that changed.
   - Global Caddy change (e.g. `DefaultInboundPublicPort` or
     `AcmeChallengeMode`): probe all owned Caddy endpoints.
   - Wait up to a configurable timeout (default 60s).
6. If health checks fail:
   - Roll back settings to the previous state.
   - Roll back promoted files (including the previous Caddy JSON config).
   - Load the previous Caddy JSON config into the running Caddy process via the
     Admin API (`POST /load`). Restart `veil-caddy.service` only if the Admin
     API load fails or a unit/binary/environment change happened.
   - Restart `veil.service` if its listener or TLS source changed.
   - Surface a clear error to the user.

The previous state is preserved in the apply stage (backup) before promotion.

### Validation

A unified inbound validator is introduced in `internal/inbounds`:

- naiveproxy:
  - For new inbounds, `domain` must be non-empty and a valid domain name.
  - Legacy inbounds in `unresolved` state are exempt from domain requirements
    until migrated.
  - Domain-level ACME email resolution must succeed for the inbound's domain.
    Conflicting explicit emails from multiple owners of the same domain are
    rejected; otherwise the resolved email follows the per-domain fallback chain
    (explicit → DefaultAcmeEmail → PanelEmail → error).
  - `publicPort` must be in 1-65535, default `Settings.DefaultInboundPublicPort`.
  - `transport` must be one of `tcp`, `quic`, `dual`; default `tcp`.
  - `transport` capability checks against `CaddyCapabilities`:
    - `tcp` requires `ForwardProxy`.
    - `quic` requires `ForwardProxy` and `H3Only`.
    - `dual` requires `ForwardProxy` and `HTTP3`.
  - `transport` determines the bind keys the inbound owns:
    - `tcp`  → TCP `(0.0.0.0, publicPort, tcp)`.
    - `quic` → UDP `(0.0.0.0, publicPort, udp)`.
    - `dual` → TCP and UDP binds on `publicPort`.
  - Each bind key `(address, publicPort, protocol)` must be owned by at most one
    service. Two naive inbounds cannot own the same bind key.
  - When `PanelAccess == "caddy"`, the Panel endpoint TCP bind
    `(0.0.0.0, PanelPublicPort, tcp)` must not collide with any naive inbound
    bind key.
  - When editing an existing naive inbound's `transport` or `publicPort`, Veil
    computes the old and new bind-key sets. It rejects the change if any newly
    added bind key is already owned by another inbound or service, with a clear
    error such as "Cannot switch naive inbound to QUIC: UDP :443 is already used
    by hysteria2 inbound 'hy-main'."
  - Name must be unique, transport must be supported.
- hysteria2:
  - If `domain` is present, it must be a valid domain name.
  - `port` and credentials validated as today.
- Settings:
  - `PanelAccess` must be one of `local`, `direct`, `caddy`.
  - `PanelDomain`/`PanelEmail` required only for `caddy`.
  - `PanelPublicPort` must be in 1-65535, default 443.
  - `DefaultInboundPublicPort` must be in 1-65535, default 443.
  - `DefaultAcmeEmail` must be a valid email when set.
  - `AcmeChallengeMode` must be one of `http-01`, `tls-alpn-01`, `dns-01` when
    set. The `dns-01` mode is reserved until DNS provider credentials are
    configured.
  - A global port-conflict check must ensure no two public services bind the
    same normalized `(address, port, protocol)` tuple. The check includes every
    `BindOwnerKind` in the global registry:
    - Panel direct listener (`PanelListen`) when `PanelAccess == "direct"` (TCP).
    - Panel Caddy TCP bind when `PanelAccess == "caddy"`.
    - All naive inbound bind keys derived from their `transport`.
    - hysteria2 UDP ports.
    - legacy Caddy binds during migration.
    - implicit ACME challenge binds.
  - ACME challenge bind planning is checked per `CaddyDomainCertSpec`:
    - `dns-01` adds no bind keys.
    - `http-01` requires Caddy-controlled TCP `:80` for the domain.
    - `tls-alpn-01` requires Caddy-controlled TCP `:443` for the domain.
    - A required ACME challenge bind may be reused only if the existing
      `veil-caddy.service` listener is compatible with the challenge type:
      - `http-01`: the listener must be able to serve plain HTTP ACME challenge
        responses on TCP :80 for the domain.
      - `tls-alpn-01`: the listener must be able to complete TLS-ALPN-01
        validation on TCP :443 for the domain.
      - Same-process ownership alone is not enough; listener protocol
        compatibility must be checked.
    - If the challenge bind is owned by another service, validation rejects the
      Apply.
    - If the challenge bind is free, Veil adds an implicit
      `BindOwnerAcmeChallenge` entry to the global bind registry and renders the
      required Caddy challenge listener/config.
  - `quic`-only naive inbounds and hysteria2-only domains cannot use `http-01`
    or `tls-alpn-01` unless the required TCP challenge bind is available and
    compatible for `veil-caddy.service`.
  - `tcp` and `dual` naive inbounds on a non-443 `publicPort` do not make
    `tls-alpn-01` viable by themselves; the domain still needs a reachable TCP
    :443 listener for TLS-ALPN-01.
  - UX warning when TCP and UDP share the same numeric port: firewall/NAT must
    allow the correct protocol.

#### Domain-level ACME email resolution

For every Caddy-managed domain, Veil resolves exactly one ACME email:

1. Collect all explicit emails from every owner of that domain (Panel site,
   naive inbounds, hysteria2 inbounds).
2. If more than one distinct explicit email exists → reject the configuration.
3. Otherwise use the explicit email.
4. If no explicit email exists, fall back to `Settings.DefaultAcmeEmail`.
5. Else fall back to `Settings.PanelEmail` if present.
6. Else reject if the domain needs ACME.

The resolved email is stored in `CaddyDomainCertSpec.Email` and is used for all
certificate operations for that domain.

### hysteria2 Configuration

The hysteria2 renderer reads the inbound domain and, when present, points TLS
certificate paths to `/etc/veil/certs/{domain}.crt` and
`/etc/veil/certs/{domain}.key`. It no longer checks `PanelAccess == "caddy"`;
it checks only whether the domain has a synced certificate.

## User Flows

### Create a naive inbound

1. User goes to Inbounds → Create.
2. Selects `naiveproxy`.
3. Enters name, domain, email (prefilled from `Settings.DefaultAcmeEmail` or
   `Settings.PanelEmail`), public port (prefilled from
   `Settings.DefaultInboundPublicPort`), transport (`tcp` by default), and
   credentials (profiles).
4. On Save, validation rejects if domain, resolved email, invalid transport,
   unsupported Caddy capability, bind-key collision (e.g. "TCP :443 is already
   used by Panel"), or ACME challenge incompatibility is invalid.
5. User applies changes. The apply order is:
   1. Render and validate the merged Caddy JSON config.
   2. Gracefully load the Caddy config via the Admin API.
   3. Wait for the certificate if needed.
   4. Health-check the affected bind key(s).
   5. Commit the active state.

### Delete a naive inbound

1. User deletes the inbound.
2. On Apply, the inbound's HTTP server is removed from the Caddy JSON config.
3. If no Panel site, no other naive inbound, and no hysteria2 inbound use the
   domain, Caddy stops managing/requesting certificates for it.

### Switch Panel to caddy mode

1. User opens Settings → Panel Access.
2. Selects `caddy`, enters Panel domain, email, and public port.
3. Saves settings.
4. User applies. New Caddy JSON config includes the Panel site on the Panel
   port. Panel service restarts to bind loopback. Caddy serves Panel on the
   domain.
5. Health check verifies Panel is reachable through Caddy.
6. On failure, rollback restores previous mode.

### Switch Panel to direct mode while naive inbounds exist

1. User selects `direct` in Settings.
2. On Apply, Panel site is removed from the Caddy JSON config, Panel service
   restarts to listen publicly with native TLS, `veil-caddy.service` continues
   to serve naive inbounds.

### Migrate legacy naive inbounds

1. After upgrade, unresolved naive inbounds are listed with a migration badge.
2. User opens the migration wizard for each inbound.
3. Wizard prefills `publicPort` from `Settings.DefaultInboundPublicPort` and asks for
   `domain`. `email` is resolved from `Settings.DefaultAcmeEmail` or
   `Settings.PanelEmail`.
4. On save, the inbound becomes managed by the new model and is rendered into
   the merged Caddy JSON config on next Apply.
5. Until migrated, the inbound cannot be edited through the new API, but it
   can be deleted via a legacy-delete flow. It continues to run on its legacy
   unit.

## Testing Strategy (TDD)

The implementation follows test-driven development. Tests are written or
updated before production code for each module.

### Unit tests

- `internal/inbounds`:
  - naiveproxy validation: missing domain, missing email, invalid publicPort,
    invalid transport, bind-key conflicts for tcp/quic/dual, Panel TCP bind
    collision, valid with default publicPort and transport, email inheritance
    from DefaultAcmeEmail or PanelEmail.
  - Editing transport recalculates old/new bind keys and rejects collisions.
  - Transport switch matrix:
    - `tcp`  → `quic`: checks newly required UDP bind.
    - `quic` → `tcp`:  checks newly required TCP bind.
    - `tcp`  → `dual`: checks newly required UDP bind.
    - `quic` → `dual`: checks newly required TCP bind.
    - `dual` → `tcp`:  releases UDP bind.
    - `dual` → `quic`: releases TCP bind.
    - changing `publicPort` recalculates all bind keys for the selected transport.
  - hysteria2 validation: optional domain, invalid domain.
- `internal/settings`:
  - PanelAccess validation per mode, required domain/email only for `caddy`.
  - DefaultAcmeEmail fallback resolution.
  - Panel/naive bind-key collision detection.
  - Global public port conflict detection covers all `BindOwnerKind` values:
    Panel direct, Panel Caddy, naive TCP/UDP, hysteria2 UDP, legacy Caddy, and
    implicit ACME challenge binds.
  - Bind address normalization:
    - `0.0.0.0:443/tcp` conflicts with `192.168.1.10:443/tcp`.
    - `::/tcp` wildcard behavior is deterministic and respects dual-stack socket
      settings.
    - Equivalent wildcard representations (`0.0.0.0`, empty address, omitted
      address) normalize to the same canonical `BindKey`.
    - Legacy per-inbound Caddy unit binds are normalized before migration
      conflict checks.
- `internal/renderer/caddyassembly` (new or extended):
  - Empty state produces empty Caddy JSON config.
  - Panel-only caddy mode renders Panel site on `PanelPublicPort`.
  - Single naive TCP inbound renders a TCP HTTP server on its `publicPort` with a
    `forward_proxy` handler and `file_server` fallback.
  - Single naive QUIC inbound renders a UDP HTTP/3 server on its `publicPort`
    with `forward_proxy` and `file_server` fallback.
  - Single naive dual inbound renders both a TCP and a UDP HTTP/3 server on its
    `publicPort`, sharing domain, certificate, and profiles.
  - Naive JSON server has `forward_proxy` before `file_server` fallback and has
    no host matcher.
  - Two naive inbounds owning the same bind key are rejected by validation.
  - Naive inbound and Panel owning the same TCP bind key are rejected by
    validation.
  - Same domain on two different naive ports renders two servers, each with TLS
    automation for that domain, and certificate ownership is shared.
  - Multiple profiles in one naive inbound render multiple `basic_auth` entries
    inside a single `forward_proxy` handler.
  - Removing a naive inbound removes its server(s); the domain stays while Panel
    or hysteria2 own it.
  - hysteria2-only domain produces a certificate-only TLS automation entry.
  - Domain-level ACME email resolution: conflicting explicit emails for the
    same domain are rejected; otherwise explicit → DefaultAcmeEmail → PanelEmail
    → error.
  - Non-443 publicPort renders correct server(s) and triggers warning.
- `internal/caddy` (or installer):
  - Capability detection returns `ForwardProxy=true` when module present.
  - Capability detection returns `ForwardProxy=false` and blocks naive Apply
    when module absent.
  - `quic` transport rejected when `H3Only=false`.
  - `dual` transport rejected when `HTTP3=false`.
- `internal/inbounds` / `internal/settings`:
  - ACME challenge bind planning:
    - `http-01` adds implicit TCP :80 `BindOwnerAcmeChallenge` when free.
    - `tls-alpn-01` adds implicit TCP :443 `BindOwnerAcmeChallenge` when free.
    - `http-01`/`tls-alpn-01` reuse an existing Caddy listener on :80/:443 only
      when protocol compatibility is verified (e.g. a Panel or naive TCP :443
      listener can answer TLS-ALPN-01; a plain HTTP :80 listener can answer
      HTTP-01).
    - `http-01`/`tls-alpn-01` rejected when :80/:443 is owned by another
      service.
    - `quic`-only naive inbound with `AcmeChallengeMode=http-01` rejected unless
      TCP :80 is available and compatible for Caddy.
    - `quic`-only naive inbound with `AcmeChallengeMode=tls-alpn-01` rejected
      unless TCP :443 is available and compatible for Caddy.
    - `quic`-only naive inbound with `AcmeChallengeMode=dns-01` allowed when DNS
      credentials configured.
    - hysteria2-only domain with `AcmeChallengeMode=http-01` rejected unless
      TCP :80 is available and compatible for Caddy.
    - hysteria2-only domain with `AcmeChallengeMode=tls-alpn-01` rejected unless
      TCP :443 is available and compatible for Caddy.
    - `tcp` naive on `publicPort=8443` with `AcmeChallengeMode=tls-alpn-01`
      rejected unless TCP :443 is available and compatible for Caddy.
  - TLS automation policy grouping:
    - Domains with the same resolved email and challenge mode share one
      automation policy.
    - Domains with different resolved emails render separate automation policies.
    - Each domain appears in exactly one automation policy.
    - ACME issuer config enables only the selected `AcmeChallengeMode` and does
      not include fallback challenge types that were not planned in the
      `BindKey` registry.
  - Implicit listener suppression:
    - Generated Caddy config does not open TCP `:80` for automatic redirects
      unless a planned `BindOwnerAcmeChallenge` or explicit redirect owner
      exists.
    - With `AcmeChallengeMode=dns-01`, no TCP `:80`/`:443` challenge listener is
      emitted unless another explicit owner requires it.
    - A naive inbound with `publicPort=443` and `AcmeChallengeMode=dns-01` does
      not implicitly create an HTTP redirect listener on `:80`.
- `internal/protocols/hysteria2`:
  - Renderer uses `/etc/veil/certs/{domain}.*` when domain present.
  - Renderer ignores domain/cert path when domain empty.
- `internal/privileged`:
  - caddycert sync triggered for hysteria2 domains regardless of PanelAccess.
  - Cert sync polls Caddy storage until certificate exists or timeout.

### Integration tests

- Apply flow:
  - Create naive inbound → staged Caddy JSON config contains expected
    `forward_proxy` server.
  - Delete naive inbound → domain removed from Caddy JSON config when no owner
    remains.
  - Switch Panel mode `direct` → `caddy` → Panel site appears.
  - Switch Panel mode `caddy` → `direct` → Panel site removed, naive servers
    remain.
- Socket verification after Apply:
  - `transport=tcp`:  TCP `publicPort` is listening for naive traffic; UDP
    `publicPort` is not listening for naive traffic.
  - `transport=quic`: UDP `publicPort` is listening for naive traffic; TCP
    `publicPort` is not listening for naive traffic. TCP `publicPort` may be
    listening only if it is owned by `BindOwnerAcmeChallenge` or another valid
    Caddy owner.
  - `transport=dual`: both TCP and UDP `publicPort` are listening for naive
    traffic.
- Socket registry verification after Apply:
  - Actual listening public TCP/UDP sockets opened by `veil-caddy.service` and
    hysteria2 services must be a subset of the planned `BindKey` registry.
  - Verification excludes internal-only sockets such as the Caddy Admin API
    loopback/Unix socket, the Panel loopback listener, and any other control
    plane sockets bound to `127.0.0.1` or `::1`.
  - No unmanaged public TCP `:80` listener exists unless it was explicitly
    planned as `BindOwnerAcmeChallenge` or a future redirect owner.
- Rollback:
  - Simulate failed health check after mode switch; verify previous settings and
    configs are restored.

### End-to-end tests (existing suite)

- Extend e2e coverage to create a naive inbound and assert Caddy JSON config
  content includes a `forward_proxy` handler on the configured public port.
- Extend e2e coverage to verify actual listening sockets match the configured
  `transport` (`tcp`/`quic`/`dual`).

## Migration

Existing deployments use `veil-caddy@.service` and per-inbound Caddyfiles.

Migration path:

1. Existing naive inbounds without a per-inbound domain are placed into a
   `legacy`/`unresolved` state. They keep their current runtime configuration
   (Caddyfile/service unit) so they continue to work after upgrade.
2. Create and Edit operations on unresolved inbounds are blocked until the
   migration wizard supplies a `domain` and `publicPort`. Delete is allowed
   through a separate legacy-delete flow with a warning that the running unit
   will be stopped and removed.
3. **All-at-once cutover.** While any unresolved legacy naive inbound exists,
   the global `veil-caddy.service` must not bind ports used by legacy
   `veil-caddy@{name}.service` units. The merged config may be generated and
   validated, but the global unit is not started or reloaded for naive inbound
   changes until all legacy naive inbounds are migrated. Creating new managed
   naive inbounds is blocked while unresolved legacy inbounds exist (MVP
   behavior), preventing port collisions and mixed runtime state.
4. A migration wizard in the web UI (and a corresponding API endpoint) lets the
   admin assign `domain`/`publicPort` to each legacy naive inbound. `transport`
   defaults to `tcp`.
5. A "Migrate all possible" button auto-fills each legacy inbound with:
   - `domain` = `Settings.PanelDomain` or a configured default domain.
   - `publicPort` = `Settings.DefaultInboundPublicPort`, then the next
     suggested free port if any of its bind keys collide.
   - `transport` = `tcp`.
   - `email` = `Settings.DefaultAcmeEmail` or `Settings.PanelEmail`.
   The admin must review and confirm before the migration is applied.
6. Once migrated, the inbound follows the new model: it is rendered into the
   merged Caddy JSON config and served by `veil-caddy.service`.
7. On first Apply after all inbounds are migrated:
   - The merged `/etc/veil/generated/caddy/config.json` is generated.
   - The runtime catalog stops emitting `veil-caddy@{name}.service` units and
     emits `veil-caddy.service`.
   - The apply engine marks old per-inbound units as orphaned and stops them.
8. A migration test verifies that a state file with the old layout can be
   migrated and produces the expected merged Caddy JSON config.

## Decisions & Open Questions

1. **Single Caddy process**: Accepted. Simplifies certificate reuse and
   lifecycle; Caddy is designed to serve many sites in one process.
2. **Endpoint vs certificate ownership split**: Accepted. Routing owns bind
   keys `(address, port, protocol)`; certificate owns the domain. This prevents
   confusion when one domain is used on multiple ports or protocols.
3. **NaiveProxy uses Caddy forward_proxy handler**: Accepted. NaiveProxy is not
   reverse-proxied to a separate backend; Caddy itself terminates TLS and runs
   the `forward_proxy` handler on the inbound's public port.
4. **Panel/naive bind collision**: Rejected. Validation forbids sharing the
   same `(address, port, protocol)` bind key between Panel and a naive inbound.
5. **No host matching for naive inbounds**: Accepted. A naive inbound owns its
   whole public port because forward proxy handles `CONNECT` requests to
   arbitrary origins.
6. **NaiveProxy transport modes**: Accepted. Naive inbounds support `tcp`,
   `quic`, and `dual` transports. TCP maps to HTTPS/H2 and owns a TCP bind key;
   QUIC maps to HTTP/3/QUIC and owns a UDP bind key; dual owns both. `quic` is
   exposed only if the Caddy binary's `H3Only` capability is verified; `dual`
   requires `HTTP3`. Validation and edit flows check conflicts by
   `(address, port, protocol)`, not by numeric port alone.
7. **ACME challenge compatibility**: Accepted. DNS-01 works for any domain.
   HTTP-01 requires public TCP :80; TLS-ALPN-01 requires public TCP :443.
   `quic`-only naive inbounds and hysteria2-only domains cannot use HTTP-01 or
   TLS-ALPN-01 unless Veil has a Caddy-controlled TCP :80/:443 challenge path
   that is available and compatible with the selected challenge type.
8. **Separate Panel public port**: Accepted. `PanelPublicPort` is separate from
   `DefaultInboundPublicPort` so the Panel endpoint is explicit and not tied to
   the naive default.
9. **Domain-level ACME email resolution**: Accepted. ACME email is resolved
   once per Caddy-managed domain. Conflicting explicit emails from Panel/naive/
   hysteria2 owners of the same domain are rejected. Otherwise resolution
   follows explicit → `DefaultAcmeEmail` → `PanelEmail` → error.
10. **hysteria2 cert-only domains**: Accepted. hysteria2-only domains are added
    to Caddy's TLS automation policy subjects, not to HTTP site blocks, so Caddy
    obtains the certificate without exposing an endpoint.
11. **Caddy config format**: Accepted. Veil generates Caddy JSON config and
    loads it via the Admin API. A Caddyfile may be kept as a debug/export
    artifact but is not authoritative.
12. **Graceful Caddy reload**: Accepted. Use Caddy Admin API for config changes;
    restart the service only for unit/binary/environment changes.
13. **Global port conflict check**: Accepted. All public listeners are checked
    for collisions by normalized `(address, port, protocol)`, covering every
    `BindOwnerKind`: Panel direct, Panel Caddy, naive, hysteria2, legacy Caddy,
    and implicit ACME challenge binds.
14. **Scoped health checks**: Accepted. Only affected endpoints are health-checked
    for inbound create/delete; global changes check all endpoints.
15. **Legacy all-at-once cutover**: Accepted. New managed naive inbounds are
    blocked until all legacy naive inbounds are migrated, preventing two Caddy
    processes from binding the same port.
16. **Rollback timeout**: Default 60s, configurable via settings if operational
    experience demands it.
17. **No unmanaged Caddy public listeners**: Accepted. Every public TCP/UDP
    listener opened by Caddy must be represented in the global `BindKey`
    registry. Automatic HTTP-to-HTTPS redirects are disabled unless modeled
    explicitly as bind owners.
