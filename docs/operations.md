# Panel Operations

This guide covers configuration validation and safe apply previews in the Veil
Panel. CLI commands are listed by `veil help`. Exposure hardening is documented
separately in [HARDENING.md](HARDENING.md).

## Live configuration validation

The Inbound editor sends a debounced candidate snapshot to
`POST /api/validation`. Validation is read-only and checks:

- required protocol, transport, port, domain, email, and credential fields;
- duplicate enabled TCP or UDP bindings;
- conflicts with the Panel listener;
- ACME challenge binding per domain: a Hysteria2 Inbound reusing the Panel's or a NaiveProxy Inbound's domain keeps `tls-alpn-01`; a Hysteria2-only domain needs HTTP-01 and a challenge listener on TCP `:80` (a `:80` conflict there is reported as a warning, not a hard error);
- live TCP and UDP port availability for new or changed Inbounds;
- public DNS resolution for protocols that require a domain;
- required runtime binaries and managed systemd units.

An unchanged enabled Inbound is allowed to retain the port it already owns.
Changing its protocol, transport, port, or identity makes the binding subject
to a fresh live-port probe.

Issues have a stable code, severity, field, optional Inbound ID, remediation,
and source. Common codes include:

| Code | Meaning |
|---|---|
| `duplicate_binding` | Two enabled candidate Inbounds use the same transport and port. |
| `reserved_panel_port` | An Inbound conflicts with the Panel TCP listener. |
| `port_in_use` | A new or changed host binding is already occupied. |
| `port_probe_failed` | Veil could not establish whether a binding is available. |
| `dns_unresolved` | The configured public domain did not resolve. |
| `runtime_binary_missing` | A required protocol executable is unavailable. |
| `runtime_unit_missing` | A required managed systemd unit is unavailable. |

The browser check is advisory feedback, not authorization. Settings, Inbound,
WARP, and apply mutations run the same validator authoritatively. A failed
authoritative check returns HTTP `422` and does not change Management state or
write staged files.

## Race boundary

Port and DNS state can change after a preview. Veil therefore validates again
immediately before mutation, but it does not reserve a port during preview.
The managed runtime may still fail to bind if another process wins the race
between validation and service start. Apply health checks and rollback remain
the final protection for that case.

## Structured apply preview

`POST /api/apply/plan` returns the compatibility fields `configs`, `actions`,
and `runtimes` plus:

- `issues`: structured validation results;
- `operations`: secret-free file and service operations.

Each operation identifies its type, source or destination path, affected unit,
interruption risk, rollback availability, and validation provenance. A
`connection-drop` risk means a service restart may terminate active proxy
sessions. `rollbackAvailable` means Veil's apply workflow creates or can use a
safety copy; it does not replace encrypted disaster-recovery backups.

The preview never contains rendered configuration bodies, passwords, private
keys, session material, API tokens, or backup passphrases.

## API example

```bash
curl -fsS \
  -H "Authorization: Bearer $VEIL_API_TOKEN" \
  -H "Content-Type: application/json" \
  -d @candidate.json \
  https://panel.example.com/api/validation

curl -fsS \
  -H "Authorization: Bearer $VEIL_API_TOKEN" \
  -X POST \
  https://panel.example.com/api/apply/plan
```

Cookie-authenticated requests also require `X-CSRF-Token`. Viewer sessions are
read-only and cannot submit validation or apply-plan POST requests.

## Panel locale and accessibility

The Panel ships English and Russian catalogs. Locale selection uses the
following precedence:

1. The authenticated user's persisted preference.
2. The `veil_locale` preference cookie.
3. The browser `Accept-Language` header.
4. English.

The setup page and Panel header expose the same locale selector.
`POST /api/auth/locale` persists the current browser user's own preference and
sets the locale cookie. It requires a cookie session plus `X-CSRF-Token`;
viewer sessions may use this self-service endpoint, but static API tokens are
rejected.

Keyboard users can skip directly to the main content. Dialogs contain Tab focus,
close on Escape, and return focus to their trigger. Async result regions announce
updates through polite live regions, and reduced-motion preferences disable
nonessential animation. The responsive layout keeps primary workflows usable
at a 360-pixel viewport without horizontal page scrolling.

## Privileged operations

The Panel runs as the locked `veil` account. Operations that modify live host
state are sent to the socket-activated `veil-helper.socket` over
`/run/veil/helper.sock`. The helper authenticates the local process credentials,
accepts only predefined operations and paths, and has no network listener.

Privileged actions include live config promotion and rollback, allowlisted
service controls and logs, encrypted backup lifecycle, state-key rotation,
verified binary installation, firewall material, and Panel restart. A rootless
container cannot perform those host operations.

All API failures use the OpenAPI `ErrorEnvelope` JSON shape. Internal failures
are sanitized; detailed filesystem and helper errors remain in the service/audit
logs rather than crossing the HTTP boundary:

```json
{
  "error": {
    "code": "operation_failed",
    "message": "privileged helper is unavailable",
    "requestId": "server-generated-request-id"
  }
}
```

Clients should branch on the structured `code` and include `requestId` in
operator reports.

## Routing source policy

Remote `geoip.dat` and `geosite.dat` material must use HTTPS and a mandatory
SHA-256 sidecar. Veil only accepts GitHub release/raw asset hosts by default,
rechecks every redirect, resolves and pins a public IP for the connection, and
rejects private, loopback, link-local, or userinfo targets. Operators may add
explicit public hosts with a comma-separated `VEIL_ROUTING_ALLOWED_HOSTS` value
in `/etc/veil/veil.env`; this does not permit private-address targets. Downloads
are size-bounded and cancellation interrupts both HTTP I/O and retry backoff.
