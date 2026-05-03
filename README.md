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

## Roadmap

- ✅ First production-ready installer flow (idempotency, checksum verification, backup before install)
- ✅ Safe update and repair workflows (backup, rollback, audit logs)
- ✅ Production hardening (signal handling, graceful shutdown, error propagation)
- ✅ Secrets at rest encryption (AES-256-GCM, field-level)
- Expand the web panel for settings, inbounds, routing rules, WARP, apply history, and service status
- Add safer update workflow (version comparison, staged rollback)
- Continue hardening: rate limiting, TLS, input validation edge cases
