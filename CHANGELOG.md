# Changelog

All notable changes to Veil will be documented in this file.

## [v0.3.0] — 2026-05-06

### Added

- Auto-generated random web base path for the panel (`/a1b2c3d4e5f6/`) — hides the panel from casual scanners
- `VEIL_WEB_BASE_PATH` written to `/etc/veil/veil.env` during install, picked up automatically by `veil serve`
- Interactive install now shows the full panel URL (`https://domain.com/a1b2c3d4e5f6/`)
- Interactive install confirmation prompt (`Apply install plan? [y/N]`) instead of requiring `--yes`

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
