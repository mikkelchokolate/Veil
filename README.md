# Veil

Veil is an open-source management panel and CLI for NaiveProxy and Hysteria2.

It is designed as a control plane: Veil manages installation, configuration, panel/API state, routing, WARP settings, and safe apply workflows while keeping NaiveProxy/Caddy, Hysteria2, and sing-box as separate services.

Core idea:

- NaiveProxy runs on TCP, for example TCP/443.
- Hysteria2 runs on UDP, for example UDP/443.
- Both can share the same numeric port because TCP and UDP port spaces are separate.
- You can install both together, or only NaiveProxy, or only Hysteria2.

Status: active development. Suitable for testing and evaluation. Production hardening in progress — see [Roadmap](#roadmap).

## Quick start

Install the latest Linux release:

```bash
curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | bash
```

Recommended RU profile with explicit shared proxy port:

```bash
curl -fsSL https://raw.githubusercontent.com/mikkelchokolate/Veil/main/scripts/install.sh | bash -s -- \
  --profile ru-recommended \
  --domain example.com \
  --email admin@example.com \
  --port 443 \
  --stack both
```

Build from source:

```bash
make test
make build
./bin/veil version
```

Dry run example:

```bash
./bin/veil install \
  --profile ru-recommended \
  --domain example.com \
  --email admin@example.com \
  --port 443 \
  --stack both \
  --dry-run
```

Notes:

- `--port` is the shared proxy port. Veil does not silently choose 443, 8443, or a random port for you.
- With `--stack both`, NaiveProxy uses TCP on the selected port and Hysteria2 uses UDP on the same numeric port.
- The panel runs on a separate TCP port; use `--panel-port` to choose it explicitly.
- The curl installer downloads a GitHub Release archive, verifies it with `checksums.txt`, installs `veil`, then runs `veil install`.

## Operator workflow: backup, rollback, and audit

Veil supports safe repair and rollback with optional backup and audit logging. All destructive commands require `--yes` for confirmation.

### Backup on repair

Use `--backup-dir` to opt into backups before repairing. The directory must be writable by the operator (typically root when running as root).

```bash
./bin/veil repair --backup-dir /var/lib/veil/backups --audit-log /var/log/veil/audit.jsonl --yes
```

The backup captures every file before repair overwrites it. The repair output prints a `Backup ID:` that you can use with rollback commands.

A dry-run (`--dry-run`) does **not** create any backup or audit side effects — it only shows what would change.

### Rollback

List all backups:

```bash
./bin/veil rollback list --backup-dir /var/lib/veil/backups
```

Restore a specific backup:

```bash
./bin/veil rollback restore <backup-id> --backup-dir /var/lib/veil/backups --audit-log /var/log/veil/audit.jsonl --yes
```

Remove a backup after you confirm it is no longer needed:

```bash
./bin/veil rollback cleanup <backup-id> --backup-dir /var/lib/veil/backups --audit-log /var/log/veil/audit.jsonl --yes
```

### Audit logs

`--audit-log` is optional. When provided, Veil appends one JSONL line per operation, recording the action, timestamp, backup ID, list of affected files, success/failure status, and error details on failure. Audit logs are never written during `--dry-run`.

## Special thanks

Veil is being shaped with the help of [Hermes Agent](https://github.com/NousResearch/hermes-agent).

## CLI reference

### serve — Run the API and web panel

```bash
# Local-only (default, no auth required)
./bin/veil serve

# HTTPS with TLS
./bin/veil serve --tls-cert /etc/veil/cert.pem --tls-key /etc/veil/key.pem

# Public bind requires auth
./bin/veil serve --listen 0.0.0.0:443 --auth-token "$VEIL_API_TOKEN"

# SIGHUP reloads state and encryption key without restart
kill -HUP $(cat /run/veil.pid)
```

### status — Query running server for service health

```bash
./bin/veil status                          # human-readable (default: 127.0.0.1:2096)
./bin/veil status --json                   # machine-readable JSON
./bin/veil status --listen :443 --auth-token secret
```

### update — Self-update from GitHub releases

```bash
./bin/veil version --check                 # compare current vs latest
./bin/veil update --dry-run                # preview
./bin/veil update --yes                    # download, verify, install
./bin/veil update --yes --restart          # + restart veil.service + health check
./bin/veil update --yes --staged           # restart + health check + auto-rollback on failure
./bin/veil update --yes --force            # reinstall even if already latest
```

With `--staged`, Veil installs the new binary, restarts the service, runs a health
check, and automatically rolls back to the previous binary if either restart or
health check fails. Use `--staged` for zero-downtime-risk updates.

### config validate — Offline state validation

```bash
./bin/veil config validate --state /var/lib/veil/state.json
```

### TLS secure defaults

When TLS is enabled, `veil serve` enforces:
- TLS 1.2 minimum (no TLS 1.0/1.1)
- AEAD cipher suites only (ECDHE + AES-256-GCM / AES-128-GCM)
- ECDSA and RSA certificates supported
- X25519 and P-256 curve preferences
- Server-preferred cipher order
- Strict-Transport-Security (HSTS) with 2-year max-age and preload flag
- Server header stripped to avoid version disclosure

### Security headers

Every response includes:
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `Referrer-Policy: no-referrer`
- `X-Permitted-Cross-Domain-Policies: none`
- `Cross-Origin-Resource-Policy: same-origin`
- `X-DNS-Prefetch-Control: off`

Panel responses additionally include `Content-Security-Policy`, `Cross-Origin-Opener-Policy`, `Permissions-Policy`, and `Origin-Agent-Cluster`.

### Service logs

View recent journald logs for managed services from the panel or API:

```bash
# API: get last 100 lines of caddy logs
curl http://127.0.0.1:2096/api/logs?unit=caddy&lines=100

# CLI: use journalctl directly
journalctl -u veil.service -n 50
```

### Docker

Multi-stage Docker image available (Alpine, non-root user):

```bash
# Build
make docker VERSION=latest

# Run with host networking (for systemd access)
docker run -d --name veil --network host \
  -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
  veil-panel/veil:latest serve

# Run with port mapping
docker run -d --name veil -p 2096:2096 \
  -v veil-state:/var/lib/veil -v veil-etc:/etc/veil \
  -e VEIL_API_TOKEN=your-secret \
  veil-panel/veil:latest serve --listen 0.0.0.0:2096 --auth-token your-secret
```

## Roadmap

- ✅ First production-ready installer flow (idempotency, checksum verification, backup before install)
- ✅ Safe update and repair workflows (backup, rollback, audit logs)
- ✅ Production hardening (signal handling, graceful shutdown, error propagation)
- ✅ Secrets at rest encryption (AES-256-GCM, field-level)
- ✅ Server hardening (TLS 1.2+, rate limiting, input validation, security headers)
- ✅ Self-update with checksum verification, backup, restart, and health check
- ✅ Service status querying and offline config validation
- ✅ SIGHUP state reload for zero-downtime config changes
- ✅ Expand the web panel for settings, inbounds, routing rules, WARP, apply history, and service status
- ✅ Safer update workflow with `--staged` flag (automatic rollback on health check failure)
- ✅ Security hardening: HSTS, security headers on all responses, Server header stripping, command injection prevention, DNS prefetch control
- ✅ Service logs viewer via panel and API
- ✅ Docker deployment support
