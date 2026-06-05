# Panel Operations

This guide covers configuration validation and safe apply previews in the Veil
Panel. Exposure hardening is documented separately in
[HARDENING.md](HARDENING.md).

## Live configuration validation

The Inbound editor sends a debounced candidate snapshot to
`POST /api/validation`. Validation is read-only and checks:

- required protocol, transport, port, domain, email, and credential fields;
- duplicate enabled TCP or UDP bindings;
- conflicts with the Panel listener;
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
