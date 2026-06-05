# Veil Project Roadmap

This roadmap tracks work that is not yet shipped. Completed capabilities move
to the release history so this file remains a reliable source of truth.

## Completed In v0.5.0

- Fail-closed public listeners require both API token and user/session auth.
- `/metrics` has an independent access policy and cannot be public on a public
  Panel listener.
- The installer supports `local`, `direct`, and `caddy` modes plus random or
  operator-selected Panel ports.
- NaiveProxy/Caddy, Hysteria2, and olcRTC support isolated systemd instances.
- The Panel provides local QR rendering and client JSON/subscription exports.
- Admin and viewer users, active session listing, and session revocation are
  available in the Panel.
- Viewer sessions are read-only in both the UI and API.
- The Panel includes replacement API token generation guidance.
- Apply flows include secret-free file, runtime, backup, rollback, and
  DNS/TLS/firewall/service-impact previews.
- Encrypted backup create/restore and state-key rotation are available from the
  CLI.
- Release artifacts include checksums, SBOM, cosign bundles, and provenance
  attestations; CI includes pinned actions, Dependabot, CodeQL, and OpenAPI
  linting.

## v0.6.0: Operational Hardening

### Exposure And First Run

- Reject public Panel HTTP unless an explicit unsafe override is supplied.
- Treat Caddy reverse-proxy exposure as public when evaluating auth and metrics
  policy, even though the Panel listener remains on loopback.
- Provide a loopback-only first-run setup flow for creating the initial admin
  and confirming exposure and backup choices.
- Surface trusted/self-signed TLS state and certificate expiry remediation.

### Authentication And Audit

- Persist browser sessions across Panel restarts without storing raw session or
  CSRF tokens.
- Enforce idle and absolute session expiry.
- Revoke user sessions when credentials or roles change.
- Record structured, redacted authentication and mutation audit events with
  rotation and retention.

### Backup And Disaster Recovery

- Add archive verification and compatibility metadata.
- Add scheduled encrypted backups through shipped systemd timer units.
- Add daily, weekly, and monthly retention policies.
- Add Panel controls for create, list, download, verify, prune, and queued
  restore operations.
- Restore committed encrypted v1 and v0.5.0-v2 compatibility fixtures in CI.

### Validation And Preview

- Add real-time TCP/UDP port collision and availability checks before save.
- Add protocol-specific DNS, TLS, firewall, and runtime remediation.
- Expand secret-free apply preview with file operation types, affected units,
  interruption risk, and rollback availability.

### Privilege Separation

- Run the HTTP Panel as the dedicated `veil` user.
- Move allowlisted filesystem, systemd, journald, firewall, update, key, and
  backup operations to a root helper over a protected Unix socket.
- Remove root capabilities and broad write paths from the Panel unit.
- Add package and E2E tests for ownership, socket permissions, managed paths,
  and helper command/path allowlists.

### API And UX

- Complete request, response, error, auth, and example schemas in OpenAPI.
- Generate and contract-test a Go client SDK.
- Add English and Russian UI catalogs with persisted user locale.
- Complete keyboard, focus, screen-reader, responsive, and mobile verification.
- Add richer visual configuration and apply-impact previews without exposing
  generated secrets.

## After v0.6.0

- Additional protocols and transport integrations.
- Standardized multi-user profiles across every supported runtime.
- Optional external session and audit stores for clustered deployments.
- Additional backup destinations and hardware-backed key providers.
