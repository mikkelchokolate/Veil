# Changelog

All notable changes to Veil will be documented in this file.

## Unreleased

## [v0.6.0] - 2026-06-05

### Security

- Public Panel exposure is fail-closed across direct and Caddy modes: user
  authentication and protected metrics are required, while direct exposure
  additionally requires TLS and an API token.
- The HTTP Panel now runs as the locked `veil` user with no capabilities.
  Allowlisted host mutations are delegated to a peer-credential-checked,
  socket-activated root helper.
- Browser sessions persist only hashed cookie and CSRF material, enforce idle
  and absolute expiry, and are revoked when user authority changes.
- Authentication and mutation events are written to a structured, redacted,
  rotated audit log.

### Added

- Added a loopback-only first-run setup flow for creating the initial
  administrator and acknowledging backup responsibilities.
- Added encrypted backup verification, compatibility metadata, scheduled
  systemd backups, daily/weekly/monthly retention, Panel backup controls, and
  cross-version restore fixtures.
- Added real-time port, DNS, runtime, and managed-unit validation plus richer
  secret-free apply previews with file operations, affected units,
  interruption risk, and rollback availability.
- Added complete English and Russian Panel catalogs with per-user locale
  persistence and a session-only locale API.
- Added skip navigation, keyboard-contained dialogs, screen-reader status
  regions, reduced-motion support, and responsive mobile layouts.
- Added a generated Go SDK and contract tests for the OpenAPI specification.

### Changed

- OpenAPI now documents the complete session, CSRF, RBAC, locale, error, and
  privileged-helper contracts with request/response schemas and examples.
- Native packages ship hardened Panel/helper units, a protected helper socket,
  scoped ownership migration, and permission-boundary tests by default.
- Viewer users may update only their own display locale; all management
  mutations remain admin-only.

### Fixed

- Backup restore jobs publish a terminal success state only after the matching
  audit record has been finalized.
- Panel HTML injects active CSRF material before placeholder cleanup, restoring
  cookie-session mutations such as persisted locale changes.
- Russian localization now covers dynamic operator actions and stable
  validation issue messages in addition to the static Panel shell.
- Installs targeting an alternative systemd directory now stage files without
  creating accounts, changing ownership, or invoking systemctl on the build
  host.

### Migration Notes

- Native-package upgrades migrate Panel state to the `veil` account and enable
  `veil-helper.socket`; review custom filesystem ownership and systemd
  overrides before upgrading.
- Existing users default to English until they select another Panel language.
- Direct public listeners must provide TLS, API-token auth, user/session auth,
  and authenticated metrics or the server refuses to start.

## [v0.5.0] — 2026-06-04

### Security

- Public Panel listeners are now fail-closed unless both API token auth and user/session auth are configured.
- Added browser session authentication, CSRF protection, admin/viewer RBAC, session revocation, and first-admin setup support.
- Added independent `/metrics` access policy controls and blocked public metrics on public Panel listeners.
- Shipped hardened systemd units by default with constrained capabilities and read/write paths.
- Hardened Docker Compose defaults, Docker runtime paths, and release supply-chain verification.
- Bumped `golang.org/x/net`, `golang.org/x/crypto`, and `golang.org/x/text` and raised the Go toolchain to 1.25.11 to clear known vulnerabilities in called code.

### Fixed

- Mieru generated config now rejects duplicate usernames across aggregated enabled Inbounds (including generated client profiles) instead of emitting a config the `mieru` server would reject at load time.
- Fixed key rotation rollback atomicity and propagated secret decryption failures during state load.
- Fixed cross-platform end-to-end graceful shutdown behavior and dev-mode auth disablement.
- Fixed olcRTC auth credential encryption/redaction coverage.

### Added

- Added native `.deb`, `.rpm`, and `.apk` packages (built with nfpm) attached to releases, installing the Panel binary and managed systemd units.
- Added an SPDX SBOM, keyless cosign signatures, and GitHub provenance attestations for release artifacts.
- Added CodeQL and Dependabot configuration, pinned GitHub Actions, and a `make verify-release` release gate.
- Added an OpenAPI 3.1 specification for the Panel HTTP API at `docs/openapi.yaml`, including validation in CI/release checks.
- Added a hardening guide, disaster recovery guide, troubleshooting guide, install guide, roadmap, documentation index, and release verification instructions.
- Added encrypted and plaintext backup create/restore commands, backup archive compatibility tests, and restore coverage.
- Added atomic state key rotation with rollback safety and decryption-error propagation.
- Added state schema migrations for long-lived management state compatibility.
- Added Panel UX controls for client exports, local QR rendering, user/session management, token rotation guidance, viewer role behavior, safe apply preview, and DNS/TLS/firewall/service warnings.
- Added NaiveProxy/Caddy multi-instance support with per-Inbound Caddyfiles and managed runtime catalog coverage.
- Added port collision and WARP port warning validation.
- Added a black-box end-to-end test suite (`test/e2e`, behind the `e2e` build tag) that compiles the real `veil` binary, runs `serve` over a live socket, and verifies health/readiness, bearer-auth gating, the full inbound→client-link→apply flow with on-disk generated config, duplicate-username rejection, state persistence across restarts, graceful shutdown, and the `config validate`/`version`/`doctor` CLIs.
- Expanded CI and release quality gates to enforce gofmt, `go mod tidy`, staticcheck, govulncheck, shellcheck, the race detector, and coverage reporting.
- Added end-to-end Mieru Inbounds, generated config, client artifacts, managed runtime metadata, firewall planning, apply validation, live promotion, repair, and uninstall coverage.
- Added optional HTTPS Panel access through Caddy with a random Web base path.
- Added self-signed Panel TLS for direct/local Panel access without Caddy.
- Added protocol runtime provisioning reporting in Apply plans and Panel Apply preview.

### Changed

- Interactive `veil install` now asks for Panel exposure mode (`local`, `direct`, or `caddy`) and random/custom Panel port selection.
- The curl installer no longer forces `--panel-access local` during interactive installs, allowing the CLI wizard to collect exposure mode and port choices.
- Direct/local Panel docs now treat random Panel ports as the safe interactive default instead of assuming `2096`.
- NaiveProxy/Caddy runtime management now uses `veil-caddy@<inbound>.service` style instances instead of a single aggregate Caddy runtime.
- Apply, repair, uninstall, status, service actions, and Panel previews now understand multi-instance managed runtimes.
- Orphaned managed systemd instances are stopped and disabled during apply/promotion cleanup.
- Docker builds and release verification now run through stronger CI gates and container publishing checks.
- The Go module path is now `github.com/mikkelchokolate/Veil`.
- Windows/default path handling and interactive passphrase prompting were made more portable.
- Veil install is Panel-only; protocols are configured later as Panel Inbounds.
- Veil install writes systemd units by default, defaults direct/local Panel access to port 2096, requires root in the curl installer only for real applies, avoids binary install side effects during curl `--dry-run`, validates missing installer option values before side effects, requires exactly one exact checksum asset match for release assets, preserves interactive prompts through `/dev/tty`, and runs daemon-reload/enable/restart for installed Panel units.
- Veil install and repair render systemd units and install plans with the resolved Caddy binary path when Panel Caddy access is used, and packaged systemd unit templates now match the renderer including `veil.env`, WARP, and Mieru units.
- Veil status and staged update health checks prefer local generated Panel TLS and trust loopback self-signed Panel certificates.
- Panel Caddy access now renders, plans, validates, repairs, and exposes firewall rules end-to-end even when no NaiveProxy Inbound exists, while rejecting non-Caddy TCP/443 runtime conflicts.
- Docker runtime directories are writable by the non-root `veil` user, Docker health checks use explicit HTTP for the default non-TLS server, and local/CI release gates now include a build plus installer script syntax/help checks.
- Veil repair writes systemd units by default, reloads systemd after repairing unit files, preserves existing Panel secrets/TLS material, and repairs Panel Caddy access files from either existing env or encrypted Panel state.
- Veil uninstall removes managed systemd unit files, honors custom install/config/systemd paths, and runs daemon-reload after uninstall; the curl uninstaller forwards those paths, validates missing option values before side effects, and allows non-root dry-run previews.
- Removed legacy protocol stack and shared proxy port install inputs instead of hiding or translating them; dead install-time protocol artifact, install client link, stack policy, shared port planning, port availability, shared-port firewall planning, protocol binary acquisition, binary download/repair, protocol checksum input, and Naive Caddy build Modules were removed from the installer.
- RURecommendedProfile, Veil install/repair workflow inputs, the Settings Interface, RU-recommended profile preview Interface, Panel settings actions, Client link responses, and the curl installer parser no longer carry legacy protocol stack/install fields; strict JSON Interfaces now reject removed `stack` input instead of accepting it for compatibility.
- NaiveProxy client links use the `naive+https://` scheme.
- Protocol capabilities now drive Inbound options, Generated config set rendering, Client link delivery, Apply actions, managed runtimes, and repair planning.

### Migration Notes

- Public `veil serve` listeners now require both an API token and at least one configured Panel user/session auth account.
- Interactive installs may choose a random high Panel port; use the printed Panel URL or SSH tunnel command instead of assuming `2096`.
- NaiveProxy/Caddy managed runtime instances now use `veil-caddy@<inbound>.service`; stale instances are cleaned up during apply/promotion.
- Go consumers importing the module must use `github.com/mikkelchokolate/Veil`.
- Operators should verify backup restore and key rotation procedures before upgrading production state.

## [v0.3.16] — 2026-05-06

### Changed

- Extracted Management snapshot construction and secret transforms into its own Module
- Extracted Atomic file writing into its own Module

## [v0.3.15] — 2026-05-06

### Changed

- Extracted Config validation command mapping and execution into its own Module
- Extracted Management config rendering and WARP routing projection into its own Module
- Extracted Firewall rule projection from Settings and Inbounds into its own Module

## [v0.3.14] — 2026-05-06

### Changed

- Extracted Promoted service reload mapping and execution into its own Module

## [v0.3.13] — 2026-05-06

### Changed

- Extracted Live config promotion, backup, live-path mapping, and rollback into its own Module

## [v0.3.12] — 2026-05-06

### Changed

- Extracted Apply stage writer for staged plan, snapshot, generated configs, route-dat files, and validation

## [v0.3.11] — 2026-05-06

### Changed

- Extracted Settings management validation/redaction/save ordering from HTTP handlers
- Extracted WARP management redaction/defaults/save ordering from HTTP handlers
- Extracted Apply history storage, filtering, capping, and stage selection into its own Module

## [v0.3.10] — 2026-05-06

### Changed

- Extracted Routing rule management mutation and persistence ordering from HTTP handlers

## [v0.3.9] — 2026-05-06

### Changed

- Extracted route-dat download/checksum logic into its own Module
- Extracted Routing preset definitions from the management HTTP implementation

## [v0.3.8] — 2026-05-06

### Changed

- Extracted Client subscription formatting and query validation from HTTP handlers
- Extracted Apply plan building into a pure Module with render-validation callbacks

## [v0.3.7] — 2026-05-06

### Changed

- Deepened the Panel Module by extracting Inbound and Client profile form slices from the giant HTML string
- Centralized State store secret policy so encrypted fields are transformed through one Module
- Centralized Client profile credential projection for Client links and Generated config set rendering
- Added an Apply workflow state seam with a fake Adapter test
- Extracted Inbound management mutation/persistence ordering from HTTP handlers

## [v0.3.6] — 2026-05-06

### Fixed

- Release CI flake in corrupted ciphertext regression test by tampering decoded ciphertext bytes instead of randomly replacing a base64 character

## [v0.3.5] — 2026-05-06

### Added

- `ClientProfileCatalog` Module for Client profile password generation, preservation, and enabled filtering
- Simple Panel controls for adding Client profiles without hand-writing JSON
- Renderer support for multiple NaiveProxy and Hysteria2 users from Client profiles

### Changed

- Client links and Generated config set now use Client profiles end-to-end
- Client profile passwords are encrypted at rest by the State store

## [v0.3.4] — 2026-05-06

### Added

- Client profiles attached to Inbounds for 3x-ui-style multiple users on one listener
- Client links now generate one URI per enabled Client profile
- Generated Caddy and Hysteria2 configs now render multiple users from Client profiles
- Client profile passwords are generated by the backend when omitted and encrypted at rest
- Panel Inbound form now includes a Client profiles JSON editor

### Changed

- Clarified domain language: **Client profile** is distinct from **Inbound**

## [v0.3.3] — 2026-05-06

### Fixed

- Apply plan now rejects multiple enabled Inbounds of the same protocol instead of allowing Generated config set overwrite
- Documented the current Generated config set limitation for multiple same-protocol Inbounds in `CONTEXT.md`

## [v0.3.2] — 2026-05-06

### Added

- `CONTEXT.md` with Veil domain language for architecture reviews and future changes
- `BuildRURecommendedInstall` workflow Module to preserve panel-port-before-profile ordering
- Docker examples for Web base path and auto-TLS options

### Changed

- CLI install now delegates ru-recommended profile ordering to the installer Module
- Local `make release-check` now passes on a clean tree

## [v0.3.1] — 2026-05-06

### Added

- Per-inbound passwords for NaiveProxy and Hysteria2 client links
- Backend password generation for new inbounds without a provided password
- Password preservation on inbound update when no replacement password is provided
- Inbound passwords encrypted at rest in `state.json`
- `make release-check` for vet, tests, diff check, and dirty-tree guard

### Changed

- Extracted `ClientLinksBuilder`, `InboundCatalog`, `StateStore`, `ApplyWorkflow`, and `GeneratedConfigSet` Modules for better locality and testability
- Generated configs now use per-inbound passwords when present, falling back to global protocol passwords
- Install credential disclosure is centralized in a small summary Module

## [v0.3.0] — 2026-05-06

### Added

- Auto-generated random web base path for the panel (`/a1b2c3d4e5f6/`) — hides the panel from casual scanners
- Panel served via Caddy reverse proxy with automatic Let's Encrypt HTTPS — no separate port or manual TLS config
- `VEIL_WEB_BASE_PATH` written to `/etc/veil/veil.env` during install, picked up automatically by `veil serve`
- Interactive install now shows the full panel URL (`https://domain.com/a1b2c3d4e5f6/`) and credentials (username, passwords)
- Interactive install confirmation prompt (`Apply install plan? [y/N]`) instead of requiring `--yes`
- Simplified user-facing documentation

## [v0.2.1] — 2026-05-05

### Added

- `veil uninstall` CLI command — safely removes Veil panel, services, and configuration
  - `--dry-run` previews the removal plan
  - `--yes` confirms the operation
  - Stops and disables veil, veil-naive, veil-hysteria2, veil-warp services
  - Removes /etc/veil, /var/lib/veil, and /usr/local/bin/veil
- `scripts/uninstall.sh` — curl-installable uninstaller script

## [v0.2.0] — 2026-05-05

### Added

- `GET /api/version` — server version and runtime info (Go version, OS/arch, name)
- `POST /api/tools/dns-lookup` — DNS resolution from the server (`{"hostname":"..."}` → addresses, CNAME)
- `GET /api/firewall` — UFW firewall status and planned rules based on inbounds and settings
- `POST /api/tools/ping` — ICMP ping from the server (`{"host":"...","count":3}` → min/avg/max/stddev)
- Web panel cards for Version, Firewall, DNS lookup, and Ping
- Rate limiting for `/api/tools/dns-lookup` (10 req/min, burst 3) and `/api/tools/ping` (5 req/min, burst 2)

## [v0.1.0] — initial release

- CLI: install, repair, rollback, serve, update, status, config, doctor, version
- API: system, tls, network, connections, processes, disk, services, logs, settings, inbounds, routing, WARP, client-links, apply, speedtest, metrics
- Web panel with full management UI
- Secrets at rest encryption (AES-256-GCM)
- TLS 1.2+, rate limiting, security headers, auth tokens
- Backup/rollback with audit logging
- Self-update with checksum verification and staged rollback
- Docker deployment support
