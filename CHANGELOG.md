# Changelog

All notable changes to Veil will be documented in this file.

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
