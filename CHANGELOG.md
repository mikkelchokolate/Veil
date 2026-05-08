# Changelog

All notable changes to Veil will be documented in this file.

## Unreleased

### Added

- Added end-to-end Mieru Inbounds, generated config, client artifacts, managed runtime metadata, firewall planning, apply validation, live promotion, repair, and uninstall coverage.
- Added optional HTTPS Panel access through Caddy with a random Web base path.
- Added self-signed Panel TLS for direct/local Panel access without Caddy.
- Added protocol runtime provisioning reporting in Apply plans.

### Changed

- Veil install is Panel-only; protocols are configured later as Panel Inbounds.
- Veil install writes systemd units by default, defaults direct/local Panel access to port 2096, requires root in the curl installer only for real applies, avoids binary install side effects during curl `--dry-run`, rejects legacy stack requests before side effects, preserves interactive prompts through `/dev/tty`, and runs daemon-reload/enable/restart for installed Panel units.
- Veil install and repair render systemd units and install plans with the resolved Caddy binary path when Panel Caddy access is used.
- Veil status and staged update health checks prefer local generated Panel TLS and trust loopback self-signed Panel certificates.
- Panel Caddy access now renders, plans, validates, repairs, and exposes firewall rules end-to-end even when no NaiveProxy Inbound exists, while rejecting non-Caddy TCP/443 runtime conflicts.
- Docker runtime directories are writable by the non-root `veil` user, Docker health checks use explicit HTTP for the default non-TLS server, and local `make release-check` now includes a build.
- Veil repair writes systemd units by default, reloads systemd after repairing unit files, preserves existing Panel secrets/TLS material, and repairs Panel Caddy access files from either existing env or encrypted Panel state.
- Veil uninstall removes managed systemd unit files, honors custom install/config/systemd paths, and runs daemon-reload after uninstall.
- Legacy protocol stack and shared proxy port install inputs are hidden or ignored for compatibility.
- NaiveProxy client links use the `naive+https://` scheme.
- Protocol capabilities now drive Inbound options, Generated config set rendering, Client link delivery, Apply actions, managed runtimes, and repair planning.

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
